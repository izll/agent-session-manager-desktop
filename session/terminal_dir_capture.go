package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Remembering where a terminal tab was left, so it starts there next time.
//
// A terminal tab is a place you navigate: you cd into a subdirectory, work
// there, stop the session, and coming back to the project root means finding
// your way there again. tmux already tracks each pane's directory, so this is a
// matter of reading it before the panes are killed and storing it on the tab.
//
// Only plain terminal tabs. For an agent the working directory is part of what
// identifies the session — the conversation it resumes and the git diff it
// shows are both anchored to it — so an agent tab that had been navigated
// elsewhere would come back pointing at the wrong project.

// Bounded because it runs on the way to stopping a session: a wedged
// multiplexer must not be able to hold up the stop, and a directory that fails
// to be captured only costs the tab its remembered path.
const terminalDirCaptureTimeout = 2 * time.Second

// CaptureTerminalWorkingDirs records where each running terminal tab currently
// is, so a later start can resume there.
//
// Called while the panes are still alive — once the tmux session is killed the
// information is gone. Reports whether anything changed, so the caller knows
// whether the instance needs saving.
func (i *Instance) CaptureTerminalWorkingDirs() bool {
	return i.captureTerminalWorkingDirs(sessionPaneDirs())
}

// CaptureTerminalWorkingDir records where one terminal tab currently is,
// for stopping a single tab rather than the whole session.
func (i *Instance) CaptureTerminalWorkingDir(windowIdx int) bool {
	return i.captureTerminalWorkingDir(windowIdx, queryPaneCurrentPath)
}

func (i *Instance) captureTerminalWorkingDir(windowIdx int, query paneDirQuery) bool {
	if i.Status != StatusRunning {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), terminalDirCaptureTimeout)
	defer cancel()
	for idx := range i.FollowedWindows {
		window := &i.FollowedWindows[idx]
		if window.Index != windowIdx {
			continue
		}
		dir, ok := i.readTerminalTabDir(ctx, *window, query)
		if !ok || dir == window.WorkDir {
			return false
		}
		window.WorkDir = dir
		i.UpdatedAt = time.Now()
		return true
	}
	return false
}

// paneDirQuery reads a pane's current directory. Injected so the tests do not
// need a running multiplexer.
type paneDirQuery func(ctx context.Context, target string) string

func (i *Instance) captureTerminalWorkingDirs(query paneDirQuery) bool {
	if i.Status != StatusRunning {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), terminalDirCaptureTimeout)
	defer cancel()

	changed := false
	for idx := range i.FollowedWindows {
		window := &i.FollowedWindows[idx]
		dir, ok := i.readTerminalTabDir(ctx, *window, query)
		if !ok || dir == window.WorkDir {
			continue
		}
		window.WorkDir = dir
		changed = true
	}
	if changed {
		i.UpdatedAt = time.Now()
	}
	return changed
}

// TerminalDirsNow reads where each running terminal tab on this computer is,
// keyed by window index, for saving while the session runs.
//
// Capturing only on stop missed the case that matters most: a machine that
// is shut down or restarts never stops its sessions, the multiplexer simply
// dies, and every terminal came back where it had last been stopped rather
// than where it was left.
//
// A tab back at the session's own directory maps to "" — no directory of its
// own — so that returning to the root is remembered too. A tab whose directory
// could not be read is absent, which leaves what is stored alone.
//
// Bounded by its own deadline whatever the caller passes: the sidebar poll
// calls this while holding the project lock, and a tab sitting in a network
// mount that stopped answering would otherwise hold it for good.
func (i *Instance) TerminalDirsNow(ctx context.Context) map[int]string {
	return i.terminalDirsNow(ctx, sessionPaneDirs())
}

func (i *Instance) terminalDirsNow(ctx context.Context, query paneDirQuery) map[int]string {
	dirs := map[int]string{}
	if i.Status != StatusRunning {
		return dirs
	}
	ctx, cancel := context.WithTimeout(ctx, terminalDirCaptureTimeout)
	defer cancel()
	for _, window := range i.FollowedWindows {
		if dir, ok := i.readTerminalTabDir(ctx, window, query); ok {
			dirs[window.Index] = dir
		}
	}
	return dirs
}

// readTerminalTabDir reads where one terminal tab is. ok false means "leave
// the stored directory alone": not a running terminal, not on this computer,
// or not answered in time.
//
// One reader for the stop-time captures and the running one, so they cannot
// disagree again. They did: the stop captures still asked the local
// multiplexer about tabs on a server, which answered for a local pane, and
// that local path — or, after the root rule, an empty one — overwrote the
// server tab's own directory.
func (i *Instance) readTerminalTabDir(ctx context.Context, window FollowedWindow, query paneDirQuery) (string, bool) {
	if !isTerminalTab(window.Agent) || window.Stopped {
		return "", false
	}
	// The query asks the local multiplexer, which knows nothing of a server's
	// panes — neither a server session's nor a server tab's.
	if i.ServerID != "" || window.ServerID != "" {
		return "", false
	}
	target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), window.Index)
	reported := query(ctx, target)
	if ctx.Err() != nil {
		return "", false
	}
	return classifyCapturedDirWithin(ctx, reported, i.Path)
}

// classifyCapturedDirWithin is classifyCapturedDir under a deadline.
//
// The checks stat the path and resolve symlinks, and neither can be
// interrupted: on a network mount whose server stopped answering they block
// indefinitely. They run aside, and an answer that does not come in time is
// treated as unreadable. The goroutine left behind ends whenever the
// filesystem does answer.
func classifyCapturedDirWithin(ctx context.Context, reported, sessionPath string) (string, bool) {
	type answer struct {
		dir string
		ok  bool
	}
	result := make(chan answer, 1)
	go func() {
		dir, ok := classifyCapturedDir(reported, sessionPath)
		result <- answer{dir, ok}
	}()
	select {
	case got := <-result:
		return got.dir, got.ok
	case <-ctx.Done():
		return "", false
	}
}

// classifyCapturedDir decides what a reported directory means for the tab.
//
// Not usable (ok false) unless it is an absolute path to a directory that
// exists: a pane whose directory was deleted keeps reporting the old path, and
// storing that would make the tab fail to start where it used to simply work.
//
// The session's own path comes back as "" with ok true — the tab's "no
// directory of its own". Writing the path itself would turn an inherited
// directory into a pinned one, so moving the session would leave its
// terminals behind; skipping it would keep a subdirectory the tab had left.
func classifyCapturedDir(reported, sessionPath string) (string, bool) {
	trimmed := strings.TrimSpace(reported)
	if trimmed == "" || !filepath.IsAbs(trimmed) {
		return "", false
	}
	if info, err := os.Stat(trimmed); err != nil || !info.IsDir() {
		return "", false
	}
	if samePath(trimmed, sessionPath) {
		return "", true
	}
	return trimmed, true
}

// isTerminalTab reports whether a tab is a plain shell rather than an agent.
func isTerminalTab(agent AgentType) bool {
	return agent == AgentTerminal
}

// samePath compares two paths allowing for symlinks, so /home/x and its
// resolved form are not treated as different directories.
func samePath(left, right string) bool {
	if left == right {
		return true
	}
	leftAbs, err := filepath.Abs(left)
	if err != nil {
		return false
	}
	rightAbs, err := filepath.Abs(right)
	if err != nil {
		return false
	}
	if evaluated, err := filepath.EvalSymlinks(leftAbs); err == nil {
		leftAbs = evaluated
	}
	if evaluated, err := filepath.EvalSymlinks(rightAbs); err == nil {
		rightAbs = evaluated
	}
	return leftAbs == rightAbs
}

// restartDirArgs returns the "-c <dir>" arguments for respawn-pane, or nothing
// when there is no usable directory to ask for.
//
// Empty means the tab has no directory of its own, and a directory that has
// since been deleted would make respawn-pane fail outright — in both cases
// letting tmux use the pane's own directory is the behaviour that works.
func restartDirArgs(dir string) []string {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" || !filepath.IsAbs(trimmed) {
		return nil
	}
	if info, err := os.Stat(trimmed); err != nil || !info.IsDir() {
		return nil
	}
	return []string{"-c", trimmed}
}

// sessionPaneDirs answers for every window of a session from one listing.
//
// Asking window by window cost one multiplexer process per terminal tab, on a
// poll that runs for every session. The first question lists the session's
// panes once and the rest are answered from that. A window missing from the
// listing answers "", which leaves its stored directory alone — the listing
// only contains windows that exist, so the "answers for window 0 instead"
// trap of display-message cannot arise here.
//
// Where the listing fails — a multiplexer without list-panes -s, say — each
// window is asked on its own as before.
func sessionPaneDirs() paneDirQuery {
	var listedSession string
	var dirs map[int]string
	var listed bool
	return func(ctx context.Context, target string) string {
		colon := strings.LastIndex(target, ":")
		if colon < 0 {
			return queryPaneCurrentPath(ctx, target)
		}
		sessionName := target[:colon]
		if sessionName != listedSession {
			listedSession = sessionName
			dirs, listed = listPaneDirs(ctx, sessionName)
		}
		if !listed {
			return queryPaneCurrentPath(ctx, target)
		}
		index, err := strconv.Atoi(target[colon+1:])
		if err != nil {
			return ""
		}
		return dirs[index]
	}
}

// listPaneDirs lists where each window of a session is: its active pane's
// directory, which is what display-message answers for the window.
func listPaneDirs(ctx context.Context, sessionName string) (map[int]string, bool) {
	output, err := TmuxCommandContext(ctx, "list-panes", "-s", "-t", sessionName, "-F",
		"#{window_index}\t#{pane_active}\t#{pane_current_path}").Output()
	if err != nil {
		return nil, false
	}
	return parsePaneDirs(string(output)), true
}

// parsePaneDirs reads list-panes output: window index, whether the pane is the
// window's active one, and its directory. The active pane wins; a window whose
// active pane is not marked keeps its first.
func parsePaneDirs(output string) map[int]string {
	dirs := map[int]string{}
	active := map[int]bool{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 3)
		if len(fields) != 3 {
			continue
		}
		index, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		isActive := strings.TrimSpace(fields[1]) == "1"
		if _, seen := dirs[index]; !seen || (isActive && !active[index]) {
			dirs[index] = fields[2]
			active[index] = isActive
		}
	}
	return dirs
}

// queryPaneCurrentPath asks the multiplexer where a pane is. tmux tracks this
// itself, so there is no process tree to walk here.
//
// The window's index comes back with the path and has to match. For a window
// that no longer exists tmux does not fail: measured, `display-message -t
// s:7` with no window 7 answers for window 0 with status 0 — so a tab whose
// window had been killed was saved with another pane's directory.
func queryPaneCurrentPath(ctx context.Context, target string) string {
	output, err := TmuxCommandContext(ctx, "display-message", "-p", "-t", target,
		"#{window_index}\t#{pane_current_path}").Output()
	if err != nil {
		return ""
	}
	return paneAnswerFor(target, string(output))
}

// paneAnswerFor returns the path from a "<index>\t<path>" answer, or "" when
// the answer is about a different window than the target names.
func paneAnswerFor(target, answer string) string {
	index, path, found := strings.Cut(strings.TrimRight(answer, "\r\n"), "\t")
	if !found {
		return ""
	}
	colon := strings.LastIndex(target, ":")
	if colon < 0 || strings.TrimSpace(index) != target[colon+1:] {
		return ""
	}
	return path
}
