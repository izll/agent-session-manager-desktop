package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	return i.captureTerminalWorkingDirs(queryPaneCurrentPath)
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
	for idx := range i.FollowedWindows {
		window := &i.FollowedWindows[idx]
		if window.Index != windowIdx {
			continue
		}
		if !isTerminalTab(window.Agent) || window.Stopped {
			return false
		}

		ctx, cancel := context.WithTimeout(context.Background(), terminalDirCaptureTimeout)
		defer cancel()

		target := fmt.Sprintf("%s:%d", i.TmuxSessionName(), window.Index)
		// Back at the session's own directory is "" and is saved like any
		// other move, the same as the running capture does; treating it as
		// nothing to save kept a subdirectory the tab had already left.
		dir, ok := classifyCapturedDir(query(ctx, target), i.Path)
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

	sessionName := i.TmuxSessionName()
	changed := false

	for idx := range i.FollowedWindows {
		window := &i.FollowedWindows[idx]
		if !isTerminalTab(window.Agent) || window.Stopped {
			continue
		}

		target := fmt.Sprintf("%s:%d", sessionName, window.Index)
		dir, ok := classifyCapturedDir(query(ctx, target), i.Path)
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
func (i *Instance) TerminalDirsNow(ctx context.Context) map[int]string {
	return i.terminalDirsNow(ctx, queryPaneCurrentPath)
}

func (i *Instance) terminalDirsNow(ctx context.Context, query paneDirQuery) map[int]string {
	dirs := map[int]string{}
	// The query asks the local multiplexer, which knows nothing of a server's
	// panes.
	if i.Status != StatusRunning || i.ServerID != "" {
		return dirs
	}
	sessionName := i.TmuxSessionName()
	for _, window := range i.FollowedWindows {
		if !isTerminalTab(window.Agent) || window.Stopped || window.ServerID != "" {
			continue
		}
		target := fmt.Sprintf("%s:%d", sessionName, window.Index)
		if dir, ok := classifyCapturedDir(query(ctx, target), i.Path); ok {
			dirs[window.Index] = dir
		}
	}
	return dirs
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

// queryPaneCurrentPath asks the multiplexer where a pane is. tmux tracks this
// itself, so there is no process tree to walk here.
func queryPaneCurrentPath(ctx context.Context, target string) string {
	output, err := TmuxCommandContext(ctx, "display-message", "-p", "-t", target, "#{pane_current_path}").Output()
	if err != nil {
		return ""
	}
	return string(output)
}
