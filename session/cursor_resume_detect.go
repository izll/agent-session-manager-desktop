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

const cursorProcessDiscoveryTimeout = 10 * time.Second

type cursorSessionDetector func(tmuxSession string, windowIdx int, expectedCWD string) string

// DetectCursorChatIDFromTmux finds the Cursor chat owned by a tmux window.
//
// Cursor has no flag to assign a chat id at launch, so the running process is
// bound to the store it keeps open instead — the same approach Codex needed.
// While a chat is live, cursor-agent holds ~/.cursor/chats/<project>/<chatId>/
// store.db open (with its -wal and -shm beside it); the id is the directory's
// own name. Measured on a running pane: the agent's process listed exactly
// those three descriptors and nothing else from the chat store.
//
// This stays unambiguous with several tabs in one project, which is what the
// alternative — taking the most recently modified directory — could not do.
func DetectCursorChatIDFromTmux(tmuxSession string, windowIdx int, expectedCWD string) string {
	return DetectCursorChatIDFromTmuxContext(context.Background(), tmuxSession, windowIdx, expectedCWD)
}

// DetectCursorChatIDFromTmuxContext is cancellable so periodic detection stops
// with the poll that started it.
func DetectCursorChatIDFromTmuxContext(ctx context.Context, tmuxSession string, windowIdx int, expectedCWD string) string {
	discoveryCtx, cancelDiscovery := context.WithTimeout(ctx, cursorProcessDiscoveryTimeout)
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

	chatsRoot, err := cursorChatsDir()
	if err != nil {
		return ""
	}
	return detectCursorChatIDFromLiveProcessTree(discoveryCtx, chatsRoot, panePID, expectedCWD)
}

// cursorChatIDFromOpenPaths picks the chat id out of the files a process has
// open.
//
// expectedCWD narrows it to one project: the directory holding a project's
// chats is named after that path, so the check is a string comparison rather
// than another file to read. It is skipped when the caller does not know the
// directory, which leaves the "exactly one candidate" rule as the only guard —
// the same rule Codex relies on.
func cursorChatIDFromOpenPaths(chatsRoot, expectedCWD string, paths []string) string {
	chatsRoot, err := filepath.Abs(trimExtendedLengthPrefix(chatsRoot))
	if err != nil {
		return ""
	}
	if evaluated, evalErr := filepath.EvalSymlinks(chatsRoot); evalErr == nil {
		chatsRoot = evaluated
	}
	wantProject := ""
	if strings.TrimSpace(expectedCWD) != "" {
		wantProject = cursorProjectHash(expectedCWD)
	}

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
		// store.db, store.db-wal and store.db-shm all name the same chat.
		if !strings.HasPrefix(filepath.Base(path), "store.db") {
			continue
		}
		if resolved, evalErr := filepath.EvalSymlinks(path); evalErr == nil {
			path = resolved
		}
		if !pathInsideDirectory(chatsRoot, path) {
			continue
		}
		chatDir := filepath.Dir(path)
		chatID := filepath.Base(chatDir)
		project := filepath.Base(filepath.Dir(chatDir))
		if chatID == "" || project == "" || !IsSafeResumeID(chatID) {
			continue
		}
		if wantProject != "" && project != wantProject {
			continue
		}
		candidates[chatID] = struct{}{}
	}
	if len(candidates) != 1 {
		return ""
	}
	for chatID := range candidates {
		return chatID
	}
	return ""
}

// detectCursorChatIDFromProcessTreeContext walks a process tree under procRoot
// and reads each process's open descriptors.
//
// procRoot is a parameter so tests can hand it a directory of their own making
// rather than the real /proc.
func detectCursorChatIDFromProcessTreeContext(ctx context.Context, procRoot, chatsRoot string, rootPID int, expectedCWD string) string {
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
	return cursorChatIDFromOpenPaths(chatsRoot, expectedCWD, paths)
}

// NeedsCursorResumeCapture reports whether any Cursor pane here should have its
// chat id read from the live process.
func (i *Instance) NeedsCursorResumeCapture() bool {
	if i.Status != StatusRunning {
		return false
	}
	if i.Agent == AgentCursor && !i.MainWindowStopped {
		return true
	}
	for idx := range i.FollowedWindows {
		window := &i.FollowedWindows[idx]
		if window.Agent == AgentCursor && !window.Stopped {
			return true
		}
	}
	return false
}

func (i *Instance) captureCursorResumeIDs(detect cursorSessionDetector) bool {
	mainWindowIdx, mainWindowOK := 0, false
	if i.Agent == AgentCursor && !i.MainWindowStopped {
		mainWindowIdx, mainWindowOK = i.getMainWindowIndex()
	}
	return i.captureCursorResumeIDsAtMainWindow(detect, mainWindowIdx, mainWindowOK)
}

// captureCursorResumeIDsAtMainWindow records the id whenever it differs, not
// only when nothing is recorded yet: starting a new chat inside Cursor moves
// the pane to another conversation, and an id left behind would resume the one
// the user walked away from. Detection reads the process's own open files, so
// it either finds the chat in use or returns nothing.
func (i *Instance) captureCursorResumeIDsAtMainWindow(detect cursorSessionDetector, mainWindowIdx int, mainWindowOK bool) bool {
	changed := false
	sessionName := i.TmuxSessionName()

	if i.Agent == AgentCursor && !i.MainWindowStopped && mainWindowOK {
		if chatID := detect(sessionName, mainWindowIdx, i.Path); chatID != "" &&
			chatID != i.ResumeSessionID {
			i.ResumeSessionID = chatID
			changed = true
			log.Printf("[CursorResume] refreshed chat ID for session=%s", i.ID)
		}
	}

	for idx := range i.FollowedWindows {
		window := &i.FollowedWindows[idx]
		if window.Agent != AgentCursor || window.Stopped {
			continue
		}
		workDir := window.WorkDir
		if workDir == "" {
			workDir = i.Path
		}
		if chatID := detect(sessionName, window.Index, workDir); chatID != "" &&
			chatID != window.ResumeSessionID {
			window.ResumeSessionID = chatID
			changed = true
			log.Printf("[CursorResume] refreshed chat ID for tab=%s/%d", i.ID, window.Index)
		}
	}

	return changed
}
