package session

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Codex's shared background server, and why it is off unless asked for.
//
// From 0.157 the terminal `codex` is a client of a background "app-server
// daemon" that it starts on first use and every later codex connects to. Two
// things go wrong with that under this app:
//
//   - The session's settings do not reach the daemon. The thread is created
//     with no approval or sandbox policy, so the daemon applies config.toml's
//     defaults — --dangerously-bypass-approvals-and-sandbox is accepted on the
//     command line and then ignored, and a YOLO session asks for approval in a
//     sandbox. (openai/codex #9144, #14068, #46252.)
//   - The daemon sometimes does not come up in time, and codex exits with
//     "app server did not become ready ... rerun with --no-daemon".
//
// It also holds the conversation's rollout file itself, outside the pane's
// process tree, which is where CaptureCodexResumeIDs looks for it.
//
// --no-daemon runs the old way, in the pane. It is the default here; the
// setting exists for someone who wants the daemon regardless.

// codexUseDaemon is the Settings value, pushed in by the app. The zero value is
// the default: no daemon.
var codexUseDaemon atomic.Bool

// SetCodexUseDaemon records whether Codex may use its background server.
func SetCodexUseDaemon(use bool) {
	codexUseDaemon.Store(use)
}

// CodexUseDaemon reports the value SetCodexUseDaemon last recorded.
func CodexUseDaemon() bool {
	return codexUseDaemon.Load()
}

// agentFlagProbeLifetime is how long an answer about a flag is trusted.
//
// Long enough that starting a handful of tabs asks once, short enough that an
// upgrade — `npm i -g @openai/codex` while the app runs — is noticed within
// minutes rather than at the next launch.
const agentFlagProbeLifetime = 10 * time.Minute

// agentFlagProbeTimeout bounds one `<agent> --help`. It runs on the way to
// starting the agent, so a hung probe must not become a hung start.
const agentFlagProbeTimeout = 5 * time.Second

type agentFlagProbeAnswer struct {
	supported bool
	at        time.Time
}

var (
	agentFlagProbeMu    sync.Mutex
	agentFlagProbeCache = map[string]agentFlagProbeAnswer{}
)

// agentHelpText returns what `<command> --help` prints on the machine a tab
// runs on. A variable so tests can answer without an agent installed.
var agentHelpText = func(i *Instance, serverID, command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agentFlagProbeTimeout)
	defer cancel()

	if serverID == "" {
		cmd := exec.CommandContext(ctx, command, "--help")
		HideConsoleWindow(cmd)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	shell, isShell := i.execOn(serverID).(ShellExecutor)
	if !isShell {
		return "", errNoShellOnServer
	}
	stdout, stderr, exitCode, err := shell.RunShell(ctx, "", command, "--help")
	if err != nil {
		return "", err
	}
	if exitCode != 0 {
		return "", errProbeExitCode
	}
	return string(stdout) + string(stderr), nil
}

type probeError string

func (e probeError) Error() string { return string(e) }

const (
	errNoShellOnServer probeError = "the server cannot run commands other than the multiplexer"
	errProbeExitCode   probeError = "--help exited with a non-zero status"
)

// agentSupportsFlag reports whether the agent on a machine lists a flag in its
// help.
//
// Read from the help rather than worked out from a version number: the version
// that introduced a flag is a fact about someone else's release history, and
// the help is what the installed binary actually accepts. Passing a flag an
// older codex does not know makes it exit with "unexpected argument" — so any
// doubt, including a probe that failed, answers no.
//
// Only a successful answer is kept. A failure is asked again next time, rather
// than keeping the flag off for the whole lifetime because a server was slow
// once.
func (i *Instance) agentSupportsFlag(serverID, command, flag string) bool {
	key := serverID + "\x00" + command + "\x00" + flag
	if serverID == "" {
		// Where the command resolves to is part of the key: switching node
		// versions swaps which codex is on PATH without anything else changing.
		path, err := exec.LookPath(command)
		if err != nil {
			return false
		}
		key += "\x00" + path
	}

	agentFlagProbeMu.Lock()
	cached, ok := agentFlagProbeCache[key]
	agentFlagProbeMu.Unlock()
	if ok && time.Since(cached.at) < agentFlagProbeLifetime {
		return cached.supported
	}

	help, err := agentHelpText(i, serverID, command)
	if err != nil {
		return false
	}
	supported := helpListsFlag(help, flag)

	agentFlagProbeMu.Lock()
	agentFlagProbeCache[key] = agentFlagProbeAnswer{supported: supported, at: time.Now()}
	agentFlagProbeMu.Unlock()
	return supported
}

// helpListsFlag reports whether help text lists a flag as a whole word, so
// --no-daemon is not found inside, say, --no-daemon-restart.
func helpListsFlag(help, flag string) bool {
	for _, field := range strings.FieldsFunc(help, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',' || r == '=' || r == '[' || r == ']'
	}) {
		if field == flag {
			return true
		}
	}
	return false
}

// resetAgentFlagProbes forgets every cached answer. For tests.
func resetAgentFlagProbes() {
	agentFlagProbeMu.Lock()
	agentFlagProbeCache = map[string]agentFlagProbeAnswer{}
	agentFlagProbeMu.Unlock()
}

// noDaemonArgs returns the flag that keeps an agent off its background server,
// or nothing.
//
// Nothing when the agent has no such server, when the user chose to use it,
// when their own extra arguments already say so (a repeated flag is an error),
// or when the agent on that machine does not know the flag.
//
// Nothing, too, for a conversation the background server still holds: without
// it that conversation would not open at all (see codex_daemon_held.go). The
// notice returned then is for the user, since YOLO does not take effect for
// it; the caller sends it once it knows which window the agent landed in.
func (i *Instance) noDaemonArgs(config AgentConfig, serverID string, args []string, extraArgs string) ([]string, *CodexDaemonHeldNotice) {
	if config.NoDaemonFlag == "" || CodexUseDaemon() {
		return nil, nil
	}
	for _, arg := range SplitArgs(extraArgs) {
		if arg == config.NoDaemonFlag {
			return nil, nil
		}
	}
	if !i.agentSupportsFlag(serverID, config.Command, config.NoDaemonFlag) {
		return nil, nil
	}
	if conversationID := conversationArg(config, args); conversationID != "" &&
		codexDaemonHoldsConversation(i, serverID, conversationID) {
		log.Printf("[CodexDaemon] conversation %s is held by the background server on %s; continuing it there",
			conversationID, describeMachine(serverID))
		return nil, &CodexDaemonHeldNotice{
			SessionID:      i.ID,
			SessionName:    i.Name,
			ServerID:       serverID,
			ConversationID: conversationID,
		}
	}
	return []string{config.NoDaemonFlag}, nil
}

// agentArgv is buildAgentArgv for an agent started on a given machine: the
// app's own flags, then whatever that machine's agent needs added, then the
// user's extra arguments.
//
// The added flag goes after the app's arguments rather than first: for a
// subcommand (codex resume <id>, codex fork <id>) that puts it on the
// subcommand, which is where it is sure to be read.
//
// held is non-nil when a conversation the background server holds is continued
// through it. The caller hands it to reportCodexDaemonHeld along with the
// window the agent was started in — known only once the window exists — so the
// user's notice can restart exactly that tab after the server is stopped.
func (i *Instance) agentArgv(config AgentConfig, serverID string, args []string, extraArgs string) (argv []string, held *CodexDaemonHeldNotice) {
	flags, held := i.noDaemonArgs(config, serverID, args, extraArgs)
	return buildAgentArgv(config.Command, append(args, flags...), extraArgs), held
}
