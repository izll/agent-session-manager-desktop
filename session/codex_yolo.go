package session

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Whether YOLO is really in effect for a Codex conversation.
//
// Claude shows its permission mode on screen, and the badge reads it from
// there. Codex does not: its footer is model, effort, directory and task. And
// the flag the app passed is no proof — Codex's background server accepts
// --dangerously-bypass-approvals-and-sandbox and then ignores it.
//
// What Codex actually applied is written into the conversation's rollout file:
// a "thread_settings_applied" event whenever the conversation is opened, and a
// "turn_context" line at every turn. The last of those says what is in force.
// YOLO is approval policy "never" with no sandbox — a permission profile of
// type "disabled", or, in older versions, sandbox policy "danger-full-access".

// CodexYolo is what a rollout says about YOLO.
type CodexYolo int

const (
	// CodexYoloUnknown: nothing says either way — no file yet, no settings
	// line in it, or it could not be read.
	CodexYoloUnknown CodexYolo = iota
	CodexYoloOn
	CodexYoloOff
)

// codexYoloTail is how much of a rollout's end is read first; the settings
// are usually within it, since opening a conversation writes them.
const codexYoloTail = 256 * 1024

// codexYoloMaxWindow bounds how far back a rollout is searched. A long turn
// writes megabytes after its turn_context; past this the answer is unknown.
const codexYoloMaxWindow = 8 * 1024 * 1024

// codexYoloRemoteTail is the same bound for a server, where the search runs
// there and only the one line comes back.
const codexYoloRemoteTail = 1024 * 1024

// codexYoloRemoteLifetime is how long a server's answer is kept: asking costs
// a command on the server, and the sidebar polls every few seconds.
const codexYoloRemoteLifetime = 15 * time.Second

// codexRolloutMissLifetime is how long "no rollout for this conversation" is
// believed before the sessions directory is searched again.
const codexRolloutMissLifetime = 30 * time.Second

type codexPermissionSettings struct {
	ApprovalPolicy    json.RawMessage `json:"approval_policy"`
	SandboxPolicy     *codexTyped     `json:"sandbox_policy"`
	PermissionProfile *codexTyped     `json:"permission_profile"`
}

type codexTyped struct {
	Type string `json:"type"`
}

type codexSettingsLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type string `json:"type"`
		codexPermissionSettings
		ThreadSettings *codexPermissionSettings `json:"thread_settings"`
	} `json:"payload"`
}

var (
	codexTurnContextMarker    = []byte(`"turn_context"`)
	codexThreadSettingsMarker = []byte(`"thread_settings_applied"`)
)

// codexYoloFromLine reads one rollout line. ok is false for a line that says
// nothing about permissions.
func codexYoloFromLine(line []byte) (CodexYolo, bool) {
	if !bytes.Contains(line, codexTurnContextMarker) && !bytes.Contains(line, codexThreadSettingsMarker) {
		return CodexYoloUnknown, false
	}
	var parsed codexSettingsLine
	if err := json.Unmarshal(line, &parsed); err != nil {
		return CodexYoloUnknown, false
	}
	var settings *codexPermissionSettings
	switch {
	case parsed.Type == "turn_context":
		settings = &parsed.Payload.codexPermissionSettings
	case parsed.Type == "event_msg" && parsed.Payload.Type == "thread_settings_applied":
		settings = parsed.Payload.ThreadSettings
	}
	if settings == nil || len(settings.ApprovalPolicy) == 0 {
		return CodexYoloUnknown, false
	}
	var approval string
	// Not a string — a structured policy — is some kind of asking, not "never".
	_ = json.Unmarshal(settings.ApprovalPolicy, &approval)
	unsandboxed := (settings.PermissionProfile != nil && settings.PermissionProfile.Type == "disabled") ||
		(settings.SandboxPolicy != nil && settings.SandboxPolicy.Type == "danger-full-access")
	if approval == "never" && unsandboxed {
		return CodexYoloOn, true
	}
	return CodexYoloOff, true
}

// lastCodexYolo finds the last settings line in a stretch of a rollout.
//
// fromLineStart says data begins at the start of a line; otherwise its first
// line is a fragment and is skipped. A last line with no newline yet is still
// being written and fails to parse, which skips it too. consumed is how far
// the complete lines reach, for the next read to start from.
func lastCodexYolo(data []byte, fromLineStart bool) (state CodexYolo, found bool, consumed int) {
	consumed = bytes.LastIndexByte(data, '\n') + 1
	body := data[:consumed]
	start := 0
	if !fromLineStart {
		start = bytes.IndexByte(body, '\n') + 1
		if start == 0 {
			return CodexYoloUnknown, false, consumed
		}
	}
	end := len(body)
	for end > start {
		lineStart := bytes.LastIndexByte(body[start:end-1], '\n') + 1 + start
		if state, ok := codexYoloFromLine(body[lineStart:end]); ok {
			return state, true, consumed
		}
		end = lineStart
	}
	return CodexYoloUnknown, false, consumed
}

type codexYoloFileEntry struct {
	size      int64
	modTime   time.Time
	state     CodexYolo
	scannedTo int64
}

var (
	codexYoloFileMu    sync.Mutex
	codexYoloFileCache = map[string]codexYoloFileEntry{}
)

// codexYoloFromFile reads a rollout's current YOLO state.
//
// Cached by size and modification time, so a poll of an unchanged file costs a
// stat. A file that only grew is read from where the last read stopped: Codex
// appends, and the newest settings line wins, so what came before is already
// accounted for.
func codexYoloFromFile(path string) CodexYolo {
	info, err := os.Stat(path)
	if err != nil {
		return CodexYoloUnknown
	}
	size, modTime := info.Size(), info.ModTime()

	codexYoloFileMu.Lock()
	cached, ok := codexYoloFileCache[path]
	codexYoloFileMu.Unlock()
	if ok && cached.size == size && cached.modTime.Equal(modTime) {
		return cached.state
	}

	f, err := os.Open(path)
	if err != nil {
		return CodexYoloUnknown
	}
	defer f.Close()

	entry := codexYoloFileEntry{size: size, modTime: modTime}
	if ok && size >= cached.scannedTo && size-cached.scannedTo <= codexYoloMaxWindow {
		data, err := readRange(f, cached.scannedTo, size)
		if err != nil {
			return CodexYoloUnknown
		}
		state, found, consumed := lastCodexYolo(data, true)
		entry.state, entry.scannedTo = cached.state, cached.scannedTo+int64(consumed)
		if found {
			entry.state = state
		}
	} else {
		entry.state, entry.scannedTo = CodexYoloUnknown, size
		for _, window := range []int64{codexYoloTail, codexYoloMaxWindow} {
			start := max(size-window, 0)
			data, err := readRange(f, start, size)
			if err != nil {
				return CodexYoloUnknown
			}
			state, found, consumed := lastCodexYolo(data, start == 0)
			entry.scannedTo = start + int64(consumed)
			if found {
				entry.state = state
				break
			}
			if start == 0 {
				break
			}
		}
	}

	codexYoloFileMu.Lock()
	codexYoloFileCache[path] = entry
	codexYoloFileMu.Unlock()
	return entry.state
}

func readRange(f *os.File, start, end int64) ([]byte, error) {
	data := make([]byte, end-start)
	n, err := f.ReadAt(data, start)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return data[:n], nil
}

type codexRolloutLookup struct {
	path string
	at   time.Time
}

var (
	codexRolloutMu    sync.Mutex
	codexRolloutPaths = map[string]codexRolloutLookup{}
)

// codexRolloutPath finds a conversation's rollout file on this computer.
// Codex files them as sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl.
func codexRolloutPath(conversationID string) string {
	codexRolloutMu.Lock()
	known, ok := codexRolloutPaths[conversationID]
	codexRolloutMu.Unlock()
	if ok {
		if known.path == "" && time.Since(known.at) < codexRolloutMissLifetime {
			return ""
		}
		if known.path != "" {
			if _, err := os.Stat(known.path); err == nil {
				return known.path
			}
		}
	}

	found := ""
	if root, err := codexSessionsDir(); err == nil {
		matches, _ := filepath.Glob(filepath.Join(root, "*", "*", "*", "rollout-*-"+conversationID+".jsonl"))
		if len(matches) > 0 {
			found = matches[0]
		}
	}
	codexRolloutMu.Lock()
	codexRolloutPaths[conversationID] = codexRolloutLookup{path: found, at: time.Now()}
	codexRolloutMu.Unlock()
	return found
}

type codexYoloRemoteAnswer struct {
	state CodexYolo
	at    time.Time
}

var (
	codexYoloRemoteMu    sync.Mutex
	codexYoloRemoteCache = map[string]codexYoloRemoteAnswer{}
)

// codexYoloRemoteScript prints the last settings line of a conversation's
// rollout on a server, searched within its last codexYoloRemoteTail bytes.
func codexYoloRemoteScript(conversationID string) string {
	name := shellQuoteForRemote("-" + conversationID + ".jsonl")
	return `for f in "$HOME"/.codex/sessions/*/*/*/rollout-*` + name + `; do
  [ -f "$f" ] || continue
  tail -c ` + strconv.Itoa(codexYoloRemoteTail) + ` "$f" | grep -a -E '"type":"(turn_context|thread_settings_applied)"' | tail -n 1
  exit 0
done
exit 0`
}

// codexYoloOnServer asks a server for a conversation's YOLO state, keeping the
// answer for codexYoloRemoteLifetime.
func (i *Instance) codexYoloOnServer(ctx context.Context, serverID, conversationID string) CodexYolo {
	key := serverID + "\x00" + conversationID
	codexYoloRemoteMu.Lock()
	cached, ok := codexYoloRemoteCache[key]
	codexYoloRemoteMu.Unlock()
	if ok && time.Since(cached.at) < codexYoloRemoteLifetime {
		return cached.state
	}

	shell, isShell := i.execOn(serverID).(ShellExecutor)
	if !isShell {
		return CodexYoloUnknown
	}
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	stdout, _, exitCode, err := shell.RunShell(commandCtx, "", "sh", "-c", codexYoloRemoteScript(conversationID))
	if err != nil || exitCode != 0 {
		// Not kept: a server that did not answer is asked again next time.
		return CodexYoloUnknown
	}
	state, _ := codexYoloFromLine(bytes.TrimSpace(stdout))

	codexYoloRemoteMu.Lock()
	codexYoloRemoteCache[key] = codexYoloRemoteAnswer{state: state, at: time.Now()}
	codexYoloRemoteMu.Unlock()
	return state
}

// codexYoloForWindow reads the YOLO state of the Codex conversation in a
// window, wherever it runs.
func (i *Instance) codexYoloForWindow(ctx context.Context, windowIdx int) CodexYolo {
	_, conversationID := i.conversationInWindow(windowIdx)
	if conversationID == "" || !IsSafeResumeID(conversationID) {
		return CodexYoloUnknown
	}
	if serverID := i.serverForWindow(windowIdx); serverID != "" {
		return i.codexYoloOnServer(ctx, serverID, conversationID)
	}
	path := codexRolloutPath(conversationID)
	if path == "" {
		return CodexYoloUnknown
	}
	return codexYoloFromFile(path)
}

// autoYesRequestedForWindow reports whether YOLO was asked for a window: the
// session's own setting, or the tab's.
func (i *Instance) autoYesRequestedForWindow(windowIdx int) bool {
	if i.AutoYes {
		return true
	}
	if windowIdx == i.GetMainWindowIndex() {
		return false
	}
	for _, fw := range i.FollowedWindows {
		if fw.Index == windowIdx {
			return fw.AutoYes
		}
	}
	return false
}

// YoloReading is what a tab's YOLO badge shows.
type YoloReading struct {
	// On: YOLO is in effect, as the agent itself reports it.
	On bool
	// NotInEffect: YOLO was asked for, and the agent reports it is not in
	// effect. Only said where the agent's own record shows it — Codex.
	NotInEffect bool
}

// codexYoloReading is the badge for a Codex window.
func (i *Instance) codexYoloReading(ctx context.Context, windowIdx int) YoloReading {
	switch i.codexYoloForWindow(ctx, windowIdx) {
	case CodexYoloOn:
		return YoloReading{On: true}
	case CodexYoloOff:
		return YoloReading{NotInEffect: i.autoYesRequestedForWindow(windowIdx)}
	default:
		return YoloReading{}
	}
}
