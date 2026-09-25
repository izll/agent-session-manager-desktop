package session

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// A conversation the background server still holds.
//
// Codex's app-server daemon keeps a conversation's rollout file open after the
// client that opened it has gone — for hours. Starting that conversation with
// --no-daemon then does not open it: Codex shows "This conversation is open in
// another app. Close it there and press R to continue here." Everyone who used
// the daemon before this app turned it off meets that screen on the first
// restart.
//
// So a resume or fork of a conversation the daemon holds goes through the
// daemon this once — the conversation opens, without YOLO, since the daemon
// ignores the flag — and the user is told, with a way to stop the daemon.

// codexDaemonCheckTimeout bounds the look for a daemon holding a conversation.
// It runs on the way to starting the tab.
const codexDaemonCheckTimeout = 3 * time.Second

// codexDaemonStopTimeout bounds `codex app-server daemon stop`.
const codexDaemonStopTimeout = 20 * time.Second

// CodexDaemonHeldNotice says a Codex conversation was continued through the
// background server because the server held it.
type CodexDaemonHeldNotice struct {
	SessionID      string `json:"sessionId"`
	SessionName    string `json:"sessionName"`
	ServerID       string `json:"serverId"`
	ConversationID string `json:"conversationId"`
	// WindowIndex is the window the conversation was started in: the tab the
	// notice restarts once the server is stopped.
	WindowIndex int `json:"windowIdx"`
}

var codexDaemonHeldHandler atomic.Pointer[func(CodexDaemonHeldNotice)]

// SetCodexDaemonHeldHandler registers what is told when a conversation is
// continued through the background server. nil removes it.
func SetCodexDaemonHeldHandler(handler func(CodexDaemonHeldNotice)) {
	if handler == nil {
		codexDaemonHeldHandler.Store(nil)
		return
	}
	codexDaemonHeldHandler.Store(&handler)
}

// reportCodexDaemonHeld tells the app about a conversation agentArgv continued
// through the background server, now running in window windowIdx. A nil
// notice, the usual case, tells nothing.
func reportCodexDaemonHeld(held *CodexDaemonHeldNotice, windowIdx int) {
	if held == nil {
		return
	}
	notice := *held
	notice.WindowIndex = windowIdx
	if handler := codexDaemonHeldHandler.Load(); handler != nil {
		// Not on the caller's goroutine: a launch runs under the storage lock,
		// and whatever the app does with the notice must not wait on it.
		go (*handler)(notice)
	}
}

// codexDaemonHoldsConversation reports whether the background server on a
// machine has a conversation's rollout file open. A variable so tests can
// answer without a daemon running. Any doubt answers no.
var codexDaemonHoldsConversation = func(i *Instance, serverID, conversationID string) bool {
	if !IsSafeResumeID(conversationID) || conversationID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), codexDaemonCheckTimeout)
	defer cancel()
	if serverID == "" {
		return codexDaemonHoldsLocally(ctx, conversationID)
	}
	shell, isShell := i.execOn(serverID).(ShellExecutor)
	if !isShell {
		return false
	}
	stdout, _, exitCode, err := shell.RunShell(ctx, "", "sh", "-c", codexDaemonHeldScript(conversationID))
	return err == nil && exitCode == 0 && strings.TrimSpace(string(stdout)) == "held"
}

// conversationArg returns the conversation an agent's arguments resume or fork,
// or "" when they start a new one.
//
// Read from the arguments the launch built rather than passed alongside them:
// every way of starting a tab builds them, so every one of them is covered.
func conversationArg(config AgentConfig, args []string) string {
	for idx, arg := range args {
		if arg == "" || (arg != config.ResumeFlag && arg != config.ForkFlag) {
			continue
		}
		for _, next := range args[idx+1:] {
			if strings.HasPrefix(next, "-") {
				continue
			}
			return next
		}
		return ""
	}
	return ""
}

// isCodexDaemonCommand reports whether a command line is Codex's managed
// background server.
func isCodexDaemonCommand(argv []string) bool {
	appServer, managed := false, false
	for _, arg := range argv {
		switch {
		case arg == "app-server":
			appServer = true
		case arg == "--managed-daemon":
			managed = true
		}
	}
	if appServer && managed {
		return true
	}
	return len(argv) > 0 && strings.Contains(filepath.ToSlash(argv[0]), "/app-server-daemon/")
}

// isRolloutOf reports whether a path is the rollout file of a conversation:
// Codex names them rollout-<time>-<id>.jsonl.
func isRolloutOf(path, conversationID string) bool {
	base := filepath.Base(filepath.FromSlash(strings.TrimSuffix(path, " (deleted)")))
	return strings.HasPrefix(base, "rollout-") && strings.HasSuffix(base, "-"+conversationID+".jsonl")
}

// codexDaemonHoldsInProc answers from a /proc tree: some process that is the
// daemon has the rollout open.
func codexDaemonHoldsInProc(ctx context.Context, procRoot, conversationID string) bool {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return false
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		pidDir := filepath.Join(procRoot, entry.Name())
		cmdline, err := os.ReadFile(filepath.Join(pidDir, "cmdline"))
		if err != nil || !isCodexDaemonCommand(strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")) {
			continue
		}
		fdDir := filepath.Join(pidDir, "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err == nil && isRolloutOf(target, conversationID) {
				return true
			}
		}
	}
	return false
}

// codexDaemonHoldsInLsof answers from `lsof -Fn` output for the daemon's
// processes.
func codexDaemonHoldsInLsof(output, conversationID string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "n") && isRolloutOf(line[1:], conversationID) {
			return true
		}
	}
	return false
}

// codexDaemonPIDsFromPS picks the daemon's processes out of
// `ps -axo pid=,command=`.
func codexDaemonPIDsFromPS(output string) []string {
	var pids []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if _, err := strconv.Atoi(fields[0]); err != nil {
			continue
		}
		if isCodexDaemonCommand(fields[1:]) {
			pids = append(pids, fields[0])
		}
	}
	return pids
}

// codexDaemonHeldScript is the same check for a server, in sh: /proc where
// there is one, lsof otherwise. Prints "held" when the daemon holds it.
func codexDaemonHeldScript(conversationID string) string {
	suffix := shellQuoteForRemote("-" + conversationID + ".jsonl")
	return `suffix=` + suffix + `
held() { case "$1" in */rollout-*"$suffix"|*/rollout-*"$suffix (deleted)") return 0;; esac; return 1; }
if [ -r /proc/self/cmdline ]; then
  for c in $(grep -l -a -e --managed-daemon -e /app-server-daemon/ /proc/[0-9]*/cmdline 2>/dev/null); do
    d=${c%/cmdline}
    for f in "$d"/fd/*; do
      t=$(readlink "$f" 2>/dev/null) || continue
      if held "$t"; then echo held; exit 0; fi
    done
  done
else
  for p in $(ps -axo pid=,command= 2>/dev/null | awk '/app-server/ && (/--managed-daemon/ || /\/app-server-daemon\//) {print $1}'); do
    for t in $(lsof -Fn -p "$p" 2>/dev/null | sed -n 's/^n//p'); do
      if held "$t"; then echo held; exit 0; fi
    done
  done
fi
exit 1`
}

// codexDaemonStopCommand runs `codex app-server daemon stop` on a machine and
// returns what it printed. A variable so tests do not stop a real daemon.
var codexDaemonStopCommand = func(i *Instance, serverID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), codexDaemonStopTimeout)
	defer cancel()
	command := AgentConfigs[AgentCodex].Command
	args := []string{"app-server", "daemon", "stop"}
	if serverID == "" {
		out, err := CommandContext(ctx, command, args...).CombinedOutput()
		return string(out), err
	}
	shell, isShell := i.execOn(serverID).(ShellExecutor)
	if !isShell {
		return "", errNoShellOnServer
	}
	stdout, stderr, exitCode, err := shell.RunShell(ctx, "", append([]string{command}, args...)...)
	out := string(stdout) + string(stderr)
	if err != nil {
		return out, err
	}
	if exitCode != 0 {
		return out, fmt.Errorf("exit status %d", exitCode)
	}
	return out, nil
}

// StopCodexDaemon stops Codex's background server on the machine a tab runs
// on ("" for this computer). The server starts again by itself when a codex
// that uses it next needs it.
func (i *Instance) StopCodexDaemon(serverID string) error {
	out, err := codexDaemonStopCommand(i, serverID)
	out = strings.TrimSpace(out)
	if err != nil {
		if out != "" {
			return fmt.Errorf("codex app-server daemon stop: %v: %s", err, out)
		}
		return fmt.Errorf("codex app-server daemon stop: %w", err)
	}
	log.Printf("[CodexDaemon] stopped on %s: %s", describeMachine(serverID), out)
	return nil
}

func describeMachine(serverID string) string {
	if serverID == "" {
		return "this computer"
	}
	return "server " + serverID
}
