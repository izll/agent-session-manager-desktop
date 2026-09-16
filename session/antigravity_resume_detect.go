package session

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const antigravityProcessDiscoveryTimeout = 10 * time.Second

type antigravitySessionDetector func(tmuxSession string, windowIdx int, expectedCWD string) string

// antigravityPresenceDir returns ~/.gemini/antigravity-cli/presence, where a
// live conversation keeps its lock file.
func antigravityPresenceDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".gemini", "antigravity-cli", "presence"), nil
}

// DetectAntigravityConversationIDFromTmux finds the conversation owned by a
// tmux window.
//
// Antigravity assigns the id itself and never puts it on the command line, so
// the running process is bound to the file it holds open — as with Codex and
// Cursor. The file here is presence/<conversation-id>.lock, which the agent
// opens when a conversation starts and holds for its lifetime.
//
// Measured on a running 1.2.3: of everything under the store, the process
// listed that lock throughout, while conversations/<id>.db came and went with
// the writes. The lock is therefore the signal and the database is not.
//
// Nothing is claimed before the first exchange: a pane sitting at the trust
// prompt has opened no conversation yet, and there is genuinely no id to
// record. That is why this finds nothing for a few seconds after launch.
func DetectAntigravityConversationIDFromTmux(tmuxSession string, windowIdx int, expectedCWD string) string {
	return DetectAntigravityConversationIDFromTmuxContext(context.Background(), tmuxSession, windowIdx, expectedCWD)
}

// DetectAntigravityConversationIDFromTmuxContext is cancellable so periodic
// detection stops with the poll that started it.
func DetectAntigravityConversationIDFromTmuxContext(ctx context.Context, tmuxSession string, windowIdx int, expectedCWD string) string {
	discoveryCtx, cancelDiscovery := context.WithTimeout(ctx, antigravityProcessDiscoveryTimeout)
	defer cancelDiscovery()
	if !tmuxWindowExistsContext(discoveryCtx, tmuxSession, windowIdx) {
		return ""
	}
	target := fmt.Sprintf("%s:%d", tmuxSession, windowIdx)
	commandCtx, cancel := context.WithTimeout(discoveryCtx, TmuxCommandTimeout)
	defer cancel()
	out, err := TmuxCommandContext(commandCtx, "display-message", "-p", "-t", target, "#{pane_pid}").Output()
	if err != nil {
		return ""
	}
	panePID, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || panePID <= 0 {
		return ""
	}

	presenceRoot, err := antigravityPresenceDir()
	if err != nil {
		return ""
	}
	return detectAntigravityConversationIDFromLiveProcessTree(discoveryCtx, presenceRoot, panePID, expectedCWD)
}

// antigravityConversationIDFromOpenPaths picks the conversation id out of the
// files a process has open.
//
// Unlike Cursor there is no project in the path to check against, so
// expectedCWD is not consulted: the search already starts from one pane's own
// process, which is a stronger binding than any directory comparison. The
// working directory is deliberately not used instead — a pane can cd
// elsewhere mid-session, and that would then reject its own conversation.
//
// Orphaned locks from conversations that have ended are simply not open, so
// they never reach this at all. That was measured: of three locks on disk only
// the live one appeared in any process's descriptors.
func antigravityConversationIDFromOpenPaths(presenceRoot string, paths []string) string {
	presenceRoot, err := filepath.Abs(trimExtendedLengthPrefix(presenceRoot))
	if err != nil {
		return ""
	}
	if evaluated, evalErr := filepath.EvalSymlinks(presenceRoot); evalErr == nil {
		presenceRoot = evaluated
	}

	// See the note in cursorChatIDFromOpenPaths: silence here is ambiguous, so
	// the counts say which filter emptied the list. Only under --debug.
	named, contained := 0, 0

	candidates := make(map[string]struct{})
	for _, path := range paths {
		path = trimExtendedLengthPrefix(path)
		if !filepath.IsAbs(path) || strings.HasSuffix(path, " (deleted)") {
			continue
		}
		path, absErr := filepath.Abs(path)
		if absErr != nil {
			continue
		}
		if filepath.Ext(path) != ".lock" {
			continue
		}
		named++
		if resolved, evalErr := filepath.EvalSymlinks(path); evalErr == nil {
			path = resolved
		}
		if !pathInsideDirectory(presenceRoot, path) {
			debugf("[AntigravityResume] lock outside the presence root: %s (root %s)", path, presenceRoot)
			continue
		}
		contained++
		id := strings.TrimSuffix(filepath.Base(path), ".lock")
		if id == "" || !IsSafeResumeID(id) {
			continue
		}
		candidates[id] = struct{}{}
	}
	if len(candidates) != 1 {
		debugf("[AntigravityResume] no single conversation: paths=%d lock-named=%d "+
			"inside-root=%d candidates=%d root=%s",
			len(paths), named, contained, len(candidates), presenceRoot)
		return ""
	}
	for id := range candidates {
		return id
	}
	return ""
}

// detectAntigravityConversationIDFromProcessTreeContext walks a process tree
// under procRoot and reads each process's open descriptors.
//
// procRoot is a parameter so tests can hand it a directory of their own making
// rather than the real /proc.
func detectAntigravityConversationIDFromProcessTreeContext(ctx context.Context, procRoot, presenceRoot string, rootPID int, _ string) string {
	visited := make(map[int]struct{})
	queue := []int{rootPID}
	var paths []string

	for len(queue) > 0 {
		if ctx.Err() != nil {
			return ""
		}
		pid := queue[0]
		queue = queue[1:]
		if pid <= 0 {
			continue
		}
		if _, seen := visited[pid]; seen {
			continue
		}
		visited[pid] = struct{}{}

		for _, childPID := range readProcessChildren(procRoot, pid) {
			if _, seen := visited[childPID]; !seen {
				queue = append(queue, childPID)
			}
		}

		fdDir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
		entries, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return ""
			}
			target, linkErr := os.Readlink(filepath.Join(fdDir, entry.Name()))
			if linkErr != nil {
				continue
			}
			paths = append(paths, target)
		}
	}
	return antigravityConversationIDFromOpenPaths(presenceRoot, paths)
}

// NeedsAntigravityResumeCapture reports whether any Antigravity pane here
// should have its conversation id read from the live process.
func (i *Instance) NeedsAntigravityResumeCapture() bool {
	if i.Status != StatusRunning {
		return false
	}
	if i.Agent == AgentAntigravity && !i.MainWindowStopped {
		return true
	}
	for idx := range i.FollowedWindows {
		window := &i.FollowedWindows[idx]
		if window.Agent == AgentAntigravity && !window.Stopped {
			return true
		}
	}
	return false
}

func (i *Instance) captureAntigravityResumeIDs(detect antigravitySessionDetector) bool {
	mainWindowIdx, mainWindowOK := 0, false
	if i.Agent == AgentAntigravity && !i.MainWindowStopped {
		mainWindowIdx, mainWindowOK = i.getMainWindowIndex()
	}
	return i.captureAntigravityResumeIDsAtMainWindow(detect, mainWindowIdx, mainWindowOK)
}

// captureAntigravityResumeIDsAtMainWindow records the id whenever it differs.
// /resume inside the CLI moves the pane to another conversation and the agent
// takes out that one's lock instead, so a value left behind would reopen the
// conversation the user walked away from.
func (i *Instance) captureAntigravityResumeIDsAtMainWindow(detect antigravitySessionDetector, mainWindowIdx int, mainWindowOK bool) bool {
	changed := false
	sessionName := i.TmuxSessionName()

	if i.Agent == AgentAntigravity && !i.MainWindowStopped && mainWindowOK {
		if id := detect(sessionName, mainWindowIdx, i.Path); id != "" && id != i.ResumeSessionID {
			i.ResumeSessionID = id
			changed = true
			log.Printf("[AntigravityResume] refreshed conversation ID for session=%s", i.ID)
		}
	}

	for idx := range i.FollowedWindows {
		window := &i.FollowedWindows[idx]
		if window.Agent != AgentAntigravity || window.Stopped {
			continue
		}
		workDir := window.WorkDir
		if workDir == "" {
			workDir = i.Path
		}
		if id := detect(sessionName, window.Index, workDir); id != "" && id != window.ResumeSessionID {
			window.ResumeSessionID = id
			changed = true
			log.Printf("[AntigravityResume] refreshed conversation ID for tab=%s/%d", i.ID, window.Index)
		}
	}

	return changed
}
