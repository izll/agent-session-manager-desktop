package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"asmgr-desktop/session"
)

// One Claude conversation open in two places at once.
//
// Both processes append to the same history file, each with its own idea of what came
// before, so the two lines of work end up interleaved in one conversation. It happened
// here: two tabs had the same conversation recorded, and every restart of the
// multiplexer resumed it twice. Nothing in Claude Code stops it, and it shows only when
// the history already looks wrong.
//
// Claude Code writes ~/.claude/sessions/<pid>.json for each process it runs, naming the
// conversation the process is on now, /resume included. That is what is read here, so
// any Claude counts, wherever it was started.

const duplicateConversationPoll = 30 * time.Second

// DuplicateConversation is one conversation and every place it is open.
type DuplicateConversation struct {
	ConversationID string   `json:"conversationId"`
	Places         []string `json:"places"`
	// Tabs are the places that are tabs of the open project, to be marked.
	Tabs []DuplicateTab `json:"tabs"`
}

// DuplicateTab is one tab a duplicated conversation is open in.
type DuplicateTab struct {
	SessionID string `json:"sessionId"`
	WindowIdx int    `json:"windowIdx"`
	// Place is this tab's own entry in Places, so a tab can name the others.
	Place int `json:"place"`
}

type claudeProcess struct {
	pid            int
	conversationID string
}

// liveClaudeProcesses reads the session records whose process is still the one that
// wrote them. A record outlives its process, and the number can be handed to another
// one since: where the system says when a process started, that has to match too.
func liveClaudeProcesses(dir string, alive func(pid int, procStart string) bool) []claudeProcess {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var live []claudeProcess
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var record struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
			ProcStart string `json:"procStart"`
		}
		if json.Unmarshal(data, &record) != nil || record.PID <= 0 || record.SessionID == "" {
			continue
		}
		if alive(record.PID, record.ProcStart) {
			live = append(live, claudeProcess{record.PID, record.SessionID})
		}
	}
	return live
}

// processStillOriginal checks a pid, and on Linux its start time against the record's.
func processStillOriginal(pid int, procStart string) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return runtime.GOOS != "linux" && session.ProcessAlive(pid)
	}
	if procStart == "" {
		return true
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) > 19 && fields[19] == procStart
}

// paneAt names a tab for a process: the pane it runs in, found by walking up from it.
type paneAt struct {
	session   string
	windowIdx int
	name      string
}

// placesOf finds each pid's tab. panes is by pane pid; a pane listed through one of the
// terminal's mirror sessions is the same pane, so the base session's entry is kept.
func placesOf(pids []int, panes map[int]paneAt, parent map[int]int) map[int]*paneAt {
	found := map[int]*paneAt{}
	for _, pid := range pids {
		for at, steps := pid, 0; at > 1 && steps < 64; at, steps = parent[at], steps+1 {
			if pane, ok := panes[at]; ok {
				found[pid] = &pane
				break
			}
		}
	}
	return found
}

func parsePaneListing(output string) map[int]paneAt {
	panes := map[int]paneAt{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) != 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		windowIdx, indexErr := strconv.Atoi(fields[2])
		if err != nil || indexErr != nil {
			continue
		}
		if current, seen := panes[pid]; seen && !strings.Contains(current.session, "_gui_") {
			continue
		}
		panes[pid] = paneAt{session: fields[1], windowIdx: windowIdx, name: fields[3]}
	}
	return panes
}

func parseParents(output string) map[int]int {
	parent := map[int]int{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 == nil && err2 == nil {
			parent[pid] = ppid
		}
	}
	return parent
}

// tabPlace is how a pane is shown, and which of the open project's tabs it is, if any.
type tabPlace struct {
	name      string
	sessionID string
}

// findDuplicateConversations groups the live processes by conversation and keeps those
// open more than once, each place named as the user knows it.
func findDuplicateConversations(processes []claudeProcess, places map[int]*paneAt, describe func(paneAt) tabPlace, outside string) []DuplicateConversation {
	byConversation := map[string][]int{}
	for _, process := range processes {
		byConversation[process.conversationID] = append(byConversation[process.conversationID], process.pid)
	}
	var duplicates []DuplicateConversation
	for id, pids := range byConversation {
		if len(pids) < 2 {
			continue
		}
		sort.Ints(pids)
		duplicate := DuplicateConversation{ConversationID: id}
		for _, pid := range pids {
			if pane := places[pid]; pane != nil {
				place := describe(*pane)
				duplicate.Places = append(duplicate.Places, place.name)
				if place.sessionID != "" {
					duplicate.Tabs = append(duplicate.Tabs, DuplicateTab{
						SessionID: place.sessionID, WindowIdx: pane.windowIdx, Place: len(duplicate.Places) - 1,
					})
				}
			} else {
				duplicate.Places = append(duplicate.Places, outside)
			}
		}
		duplicates = append(duplicates, duplicate)
	}
	sort.Slice(duplicates, func(i, j int) bool { return duplicates[i].ConversationID < duplicates[j].ConversationID })
	return duplicates
}

// duplicatesKey says whether what is found now is what was already reported.
func duplicatesKey(duplicates []DuplicateConversation) string {
	var key strings.Builder
	for _, duplicate := range duplicates {
		key.WriteString(duplicate.ConversationID + "=" + strings.Join(duplicate.Places, "|") + ";")
	}
	return key.String()
}

// watchDuplicateConversations reports a duplicate once, when it appears, and again only
// if it changes. Not on Windows, whose multiplexer and process table are another story.
func (a *App) watchDuplicateConversations(ctx context.Context) {
	if runtime.GOOS == "windows" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".claude", "sessions")
	ticker := time.NewTicker(duplicateConversationPoll)
	defer ticker.Stop()
	reported := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		duplicates := a.duplicateConversationsNow(ctx, dir)
		key := duplicatesKey(duplicates)
		if key == reported {
			continue
		}
		reported = key
		a.duplicatesMu.Lock()
		a.duplicates = duplicates
		a.duplicatesMu.Unlock()
		// Empty too, so marks on tabs go once it is sorted out.
		if a.ctx != nil {
			emitEvent(a.ctx, "conversations:duplicate", duplicates)
		}
		if len(duplicates) == 0 {
			continue
		}
		for _, duplicate := range duplicates {
			log.Printf("[duplicates] conversation %s is open in: %s", duplicate.ConversationID, strings.Join(duplicate.Places, ", "))
		}
		a.notifyDuplicateConversations(ctx, duplicates)
	}
}

// GetDuplicateConversations is what was found last, for an interface that has just
// loaded and missed the event.
func (a *App) GetDuplicateConversations() []DuplicateConversation {
	a.duplicatesMu.Lock()
	defer a.duplicatesMu.Unlock()
	return append([]DuplicateConversation{}, a.duplicates...)
}

func (a *App) duplicateConversationsNow(ctx context.Context, dir string) []DuplicateConversation {
	processes := liveClaudeProcesses(dir, processStillOriginal)
	counts := map[string]int{}
	var pids []int
	for _, process := range processes {
		counts[process.conversationID]++
	}
	for _, process := range processes {
		if counts[process.conversationID] > 1 {
			pids = append(pids, process.pid)
		}
	}
	if len(pids) == 0 {
		return nil
	}
	commandCtx, cancel := context.WithTimeout(ctx, session.TmuxCommandTimeout)
	defer cancel()
	listing, _ := session.TmuxCommandContext(commandCtx, "list-panes", "-a", "-F",
		"#{pane_pid}\t#{session_name}\t#{window_index}\t#{window_name}").Output()
	table, _ := session.CommandContext(commandCtx, "ps", "-A", "-o", "pid=,ppid=").Output()
	places := placesOf(pids, parsePaneListing(string(listing)), parseParents(string(table)))
	sessions := a.sessionsByTmuxName()
	// With the window number: one session can have several tabs of the same name.
	return findDuplicateConversations(processes, places, func(pane paneAt) tabPlace {
		base := pane.session
		if at := strings.Index(base, "_gui_"); at > 0 {
			base = base[:at]
		}
		if inst, ok := sessions[base]; ok {
			return tabPlace{name: inst.Name + " › " + pane.name + " #" + strconv.Itoa(pane.windowIdx), sessionID: inst.ID}
		}
		return tabPlace{name: base + " › " + pane.name + " #" + strconv.Itoa(pane.windowIdx)}
	}, "")
}

// sessionsByTmuxName gives the open project's sessions; another project's are named by
// their multiplexer session and not marked.
func (a *App) sessionsByTmuxName() map[string]*session.Instance {
	sessions := map[string]*session.Instance{}
	if a.storage == nil {
		return sessions
	}
	instances, _, err := a.storage.LoadAll()
	if err != nil {
		return sessions
	}
	for _, inst := range instances {
		sessions[inst.TmuxSessionName()] = inst
	}
	return sessions
}

func (a *App) notifyDuplicateConversations(ctx context.Context, duplicates []DuplicateConversation) {
	if a.storage == nil {
		return
	}
	_, _, settings, err := a.storage.LoadAllWithSettingsContext(ctx)
	if err != nil || settings == nil || !settings.NotifyDesktop {
		return
	}
	title := "⚠ Claude conversation open twice"
	if settings.Language == "hu" {
		title = "⚠ Egy Claude-beszélgetés kétszer fut"
	}
	var body []string
	for _, duplicate := range duplicates {
		body = append(body, strings.Join(duplicate.Places, " + "))
	}
	a.attentionWG.Add(1)
	go func() {
		defer a.attentionWG.Done()
		deliveryCtx, cancel := context.WithTimeout(ctx, desktopNotificationTimeout)
		defer cancel()
		if err := desktopNotificationDeliver(deliveryCtx, title, strings.Join(body, "\n")); err != nil && ctx.Err() == nil {
			log.Printf("[notify] desktop notification failed: %v", err)
		}
	}()
}
