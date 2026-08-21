package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxCodexMetaLineSize = 1024 * 1024

type codexSessionDetector func(tmuxSession string, windowIdx int, expectedCWD string) string

// DetectCodexSessionIDFromTmux finds the Codex conversation owned by a tmux
// window. Codex does not support assigning an ID when a fresh interactive
// session starts, so we bind the running process to the rollout file it keeps
// open instead. This remains unambiguous even when several tabs use the same
// working directory.
func DetectCodexSessionIDFromTmux(tmuxSession string, windowIdx int, expectedCWD string) string {
	return DetectCodexSessionIDFromTmuxContext(context.Background(), tmuxSession, windowIdx, expectedCWD)
}

// DetectCodexSessionIDFromTmuxContext is cancellable so periodic detection
// cannot retain the storage lock across application shutdown.
func DetectCodexSessionIDFromTmuxContext(ctx context.Context, tmuxSession string, windowIdx int, expectedCWD string) string {
	if !tmuxWindowExistsContext(ctx, tmuxSession, windowIdx) {
		return ""
	}
	target := fmt.Sprintf("%s:%d", tmuxSession, windowIdx)
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	out, err := TmuxCommandContext(commandCtx, "display-message", "-p", "-t", target, "#{pane_pid}").Output()
	if err != nil {
		return ""
	}
	panePID, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || panePID <= 0 {
		return ""
	}

	sessionsRoot, err := codexSessionsDir()
	if err != nil {
		return ""
	}
	return detectCodexSessionIDFromLiveProcessTree(ctx, sessionsRoot, panePID, expectedCWD)
}

// detectCodexSessionIDFromOpenPaths applies the same containment, format and
// ambiguity checks to platform-specific process open-file discovery.
func detectCodexSessionIDFromOpenPaths(sessionsRoot, expectedCWD string, paths []string) string {
	sessionsRoot, err := filepath.Abs(sessionsRoot)
	if err != nil {
		return ""
	}
	if evaluated, evalErr := filepath.EvalSymlinks(sessionsRoot); evalErr == nil {
		sessionsRoot = evaluated
	}

	candidates := make(map[string]struct{})
	for _, path := range paths {
		if !filepath.IsAbs(path) || strings.HasSuffix(path, " (deleted)") {
			continue
		}
		path, err = filepath.Abs(path)
		if err != nil || filepath.Ext(path) != ".jsonl" {
			continue
		}
		if resolved, evalErr := filepath.EvalSymlinks(path); evalErr == nil {
			path = resolved
		}
		if !pathInsideDirectory(sessionsRoot, path) {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sessionID := parseCodexRootSessionMeta(f, expectedCWD)
		_ = f.Close()
		if sessionID != "" {
			candidates[sessionID] = struct{}{}
		}
	}
	if len(candidates) != 1 {
		return ""
	}
	for sessionID := range candidates {
		return sessionID
	}
	return ""
}

func detectCodexSessionIDFromProcessTree(procRoot, sessionsRoot string, rootPID int, expectedCWD string) string {
	sessionsRoot, err := filepath.Abs(sessionsRoot)
	if err != nil {
		return ""
	}
	if evaluated, evalErr := filepath.EvalSymlinks(sessionsRoot); evalErr == nil {
		sessionsRoot = evaluated
	}

	candidates := make(map[string]struct{})
	visited := make(map[int]struct{})
	queue := []int{rootPID}

	for len(queue) > 0 {
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
			fdPath := filepath.Join(fdDir, entry.Name())
			target, err := os.Readlink(fdPath)
			if err != nil || !filepath.IsAbs(target) || strings.HasSuffix(target, " (deleted)") {
				continue
			}
			target, err = filepath.Abs(target)
			if err != nil || filepath.Ext(target) != ".jsonl" {
				continue
			}
			// Resolve the target too, not just sessionsRoot. On macOS /tmp is a
			// symlink to /private/tmp, so a path reported as /tmp/... never
			// matches a root already resolved to /private/tmp — and the file is
			// skipped, which is why Codex session detection failed there.
			if resolved, evalErr := filepath.EvalSymlinks(target); evalErr == nil {
				target = resolved
			}
			if !pathInsideDirectory(sessionsRoot, target) {
				continue
			}

			f, err := os.Open(fdPath)
			if err != nil {
				continue
			}
			sessionID := parseCodexRootSessionMeta(f, expectedCWD)
			_ = f.Close()
			if sessionID != "" {
				candidates[sessionID] = struct{}{}
			}
		}
	}

	if len(candidates) != 1 {
		return ""
	}
	for sessionID := range candidates {
		return sessionID
	}
	return ""
}

func readProcessChildren(procRoot string, pid int) []int {
	taskDir := filepath.Join(procRoot, strconv.Itoa(pid), "task")
	tasks, err := os.ReadDir(taskDir)
	if err != nil {
		return nil
	}

	seen := make(map[int]struct{})
	var children []int
	for _, task := range tasks {
		if _, err := strconv.Atoi(task.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(taskDir, task.Name(), "children"))
		if err != nil {
			continue
		}
		for _, field := range strings.Fields(string(data)) {
			childPID, err := strconv.Atoi(field)
			if err != nil || childPID <= 0 {
				continue
			}
			if _, exists := seen[childPID]; exists {
				continue
			}
			seen[childPID] = struct{}{}
			children = append(children, childPID)
		}
	}
	return children
}

func parseCodexRootSessionMeta(r io.Reader, expectedCWD string) string {
	reader := bufio.NewReader(io.LimitReader(r, maxCodexMetaLineSize+1))
	line, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return ""
	}
	if len(line) > maxCodexMetaLineSize {
		return ""
	}

	var meta codexSessionMeta
	if err := json.Unmarshal(line, &meta); err != nil || meta.Type != "session_meta" {
		return ""
	}
	if !meta.isRootCLI() {
		return ""
	}
	if expectedCWD != "" && !sameCleanPath(meta.Payload.CWD, expectedCWD) {
		return ""
	}

	sessionID := meta.resumeID()
	if sessionID == "" || !IsSafeResumeID(sessionID) {
		return ""
	}
	return sessionID
}

func pathInsideDirectory(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sameCleanPath(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" || !filepath.IsAbs(left) {
		return false
	}
	leftAbs, leftErr := filepath.Abs(filepath.Clean(left))
	rightAbs, rightErr := filepath.Abs(filepath.Clean(right))
	if leftErr != nil || rightErr != nil {
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

// CaptureCodexResumeIDs saves previously unknown Codex IDs for every running
// window. Existing IDs are deliberately never overwritten: they may have been
// selected manually and already identify the intended conversation.
func (i *Instance) CaptureCodexResumeIDs() bool {
	return i.captureCodexResumeIDs(DetectCodexSessionIDFromTmux)
}

// NeedsCodexResumeCapture reports whether any Codex pane here should have its
// session id read from the live process.
//
// Every running Codex pane qualifies, not only one with no id recorded yet.
// Codex starts a new session when the user resumes an earlier conversation
// inside it, and the id we hold then refers to the conversation they moved away
// from — so restarting the tab reopened that one, or an empty one, rather than
// what was on screen. The capture is a read of the process's own open files, so
// re-reading it costs a directory scan and cannot invent an id that is not
// there.
func (i *Instance) NeedsCodexResumeCapture() bool {
	if i.Status != StatusRunning {
		return false
	}
	if i.Agent == AgentCodex && !i.MainWindowStopped {
		return true
	}
	for idx := range i.FollowedWindows {
		window := &i.FollowedWindows[idx]
		if window.Agent == AgentCodex && !window.Stopped {
			return true
		}
	}
	return false
}

func (i *Instance) captureCodexResumeIDs(detect codexSessionDetector) bool {
	mainWindowIdx, mainWindowOK := 0, false
	if i.Agent == AgentCodex && i.ResumeSessionID == "" && !i.MainWindowStopped {
		mainWindowIdx, mainWindowOK = i.getMainWindowIndex()
	}
	return i.captureCodexResumeIDsAtMainWindow(detect, mainWindowIdx, mainWindowOK)
}

func (i *Instance) captureCodexResumeIDsAtMainWindow(detect codexSessionDetector, mainWindowIdx int, mainWindowOK bool) bool {
	changed := false
	sessionName := i.TmuxSessionName()

	// Recorded whenever it differs, not only when nothing is recorded yet.
	//
	// Resuming an earlier conversation inside Codex starts a new session, and
	// the id held here then points at the conversation the user moved away from
	// — so restarting the tab reopened that one, or an empty one, instead of
	// what was on screen. The detection reads the live process's own open
	// files, so it either finds the session in use or returns nothing; it
	// cannot substitute a wrong id for a right one.
	if i.Agent == AgentCodex && !i.MainWindowStopped {
		if mainWindowOK {
			if sessionID := detect(sessionName, mainWindowIdx, i.Path); sessionID != "" &&
				sessionID != i.ResumeSessionID {
				i.ResumeSessionID = sessionID
				changed = true
				log.Printf("[CodexResume] refreshed conversation ID for session=%s", i.ID)
			}
		}
	}

	for idx := range i.FollowedWindows {
		window := &i.FollowedWindows[idx]
		if window.Agent != AgentCodex || window.Stopped {
			continue
		}
		workDir := window.WorkDir
		if workDir == "" {
			workDir = i.Path
		}
		if sessionID := detect(sessionName, window.Index, workDir); sessionID != "" &&
			sessionID != window.ResumeSessionID {
			window.ResumeSessionID = sessionID
			changed = true
			log.Printf("[CodexResume] refreshed conversation ID for tab=%s/%d", i.ID, window.Index)
		}
	}

	return changed
}
