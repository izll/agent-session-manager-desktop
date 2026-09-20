package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// What the connection test reports.
//
// Step by step rather than a single yes or no: "could not connect" is the
// least useful thing to tell someone setting up a server. Reaching the machine,
// proving who you are, and finding a multiplexer there are three different
// problems with three different fixes, and the test says which one it got to.

// StepStatus is how one check turned out.
type StepStatus string

const (
	StepOK      StepStatus = "ok"
	StepFailed  StepStatus = "failed"
	StepSkipped StepStatus = "skipped"
	// StepAttention is for a result that is not a failure but needs a
	// decision — an unknown host key, most of all.
	StepAttention StepStatus = "attention"
)

// CheckStep is one line of the report.
type CheckStep struct {
	Name   string     `json:"name"`
	Status StepStatus `json:"status"`
	// Detail carries what was found: a version, a fingerprint, an error.
	Detail string `json:"detail,omitempty"`
}

// CheckResult is the whole report.
type CheckResult struct {
	Steps []CheckStep `json:"steps"`
	// HostKey is the fingerprint the server presented, for the caller to store
	// once the user has accepted it.
	HostKey string `json:"hostKey,omitempty"`
	// HostKeyIsNew says the fingerprint has not been accepted before, so the
	// caller knows to ask rather than to save it quietly.
	HostKeyIsNew bool `json:"hostKeyIsNew,omitempty"`
	// HostKeyChanged is the serious one: the server answers with a different
	// identity than the one recorded.
	HostKeyChanged bool `json:"hostKeyChanged,omitempty"`
	// NeedsPassphrase says the chosen key file is encrypted.
	NeedsPassphrase bool `json:"needsPassphrase,omitempty"`
	// OK is true when a session could actually be run on this server.
	OK bool `json:"ok"`
	// SuggestedExtraPath is a directory holding agents that the server's
	// non-interactive shell cannot see.
	//
	// Carried as a value rather than only mentioned in a step's text so the
	// UI can offer to apply it. Finding the agent and then making the user
	// retype the path is the sort of half-help that reads as no help.
	SuggestedExtraPath string `json:"suggestedExtraPath,omitempty"`
}

func (r *CheckResult) add(name string, status StepStatus, detail string) {
	r.Steps = append(r.Steps, CheckStep{Name: name, Status: status, Detail: detail})
}

// Step names. Constants because the UI translates on them, and a typo would
// show the raw name rather than a translation.
const (
	StepConnect     = "connect"
	StepAuth        = "auth"
	StepHostKey     = "hostkey"
	StepMultiplexer = "multiplexer"
	StepAgents      = "agents"
	StepArch        = "arch"
)

// AgentCommands are looked for on the server, so the session dialog can say
// which agents are usable there rather than reporting the local machine's.
var AgentCommands = []string{"claude", "codex", "agy", "cursor-agent", "gemini", "aider", "q", "opencode"}

// Check connects to a server and reports what it found.
//
// Every failure returns a result rather than only an error: the steps that did
// succeed are what tell the user how far the setup got.
func Check(ctx context.Context, target *Target, creds *Credentials,
	acceptNewHostKey func(fingerprint string) bool) *CheckResult {

	result := &CheckResult{}

	client, err := Dial(ctx, target, creds, acceptNewHostKey)
	if err != nil {
		classifyDialError(result, target, err)
		return result
	}
	defer client.Close()

	result.add(StepConnect, StepOK, target.address())
	result.add(StepAuth, StepOK, describeAuth(target))
	if target.KnownHostKey != "" {
		result.add(StepHostKey, StepOK, target.KnownHostKey)
		result.HostKey = target.KnownHostKey
	}

	// The multiplexer is not a nice-to-have: without it the app cannot do the
	// one thing it does, so its absence is a failure rather than a warning.
	if version, err := client.Run(ctx, "tmux -V"); err == nil {
		result.add(StepMultiplexer, StepOK, strings.TrimSpace(string(version)))
		result.OK = true
	} else {
		result.add(StepMultiplexer, StepFailed, "detail.tmuxMissing")
	}

	// uname is how a Unix server names its architecture, and its absence says
	// more than its failure: a machine without it is not one the helper can be
	// built for. Reported as such rather than as "exited with status 1", which
	// is what the user would otherwise be left to interpret.
	arch, archErr := client.Run(ctx, "uname -m")
	trimmed := strings.TrimSpace(string(arch))
	switch {
	case archErr != nil || trimmed == "":
		result.add(StepArch, StepFailed, "detail.notAUnixServer")
		result.OK = false
	case normaliseArch(trimmed) == "":
		// The helper is shipped for amd64 and arm64. Anything else cannot run
		// it, and saying so now beats failing at install time.
		result.add(StepArch, StepFailed, "detail.unsupportedArchitecture|"+trimmed)
		result.OK = false
	default:
		result.add(StepArch, StepOK, trimmed)
	}

	agentsStatus, agentsDetail, suggestedPath := agentStatus(ctx, client, target)
	result.add(StepAgents, agentsStatus, agentsDetail)
	result.SuggestedExtraPath = suggestedPath
	return result
}

// agentStatus lists the agents present on the server.
func agentStatus(ctx context.Context, client *Client, target *Target) (StepStatus, string, string) {
	// One command rather than one per agent: each is a round trip, and this
	// runs while a dialog waits.
	var builder strings.Builder
	for _, command := range AgentCommands {
		fmt.Fprintf(&builder, "command -v %s >/dev/null 2>&1 && echo %s; ", command, command)
	}

	// "true" at the end so the command succeeds even when nothing was found.
	//
	// Every probe is a `command -v ... && echo ...`, and the exit status of the
	// whole line is that of the last one — so a server with no agents on its
	// PATH answered with status 1, which was read as "could not ask" rather
	// than "asked, found none". The step then reported itself skipped and the
	// search for agents installed off the PATH never ran at all, which is
	// exactly the case it exists for.
	builder.WriteString("true")

	out, err := client.Run(ctx, withExtraPath(target, builder.String()))
	if err != nil {
		return StepSkipped, "", ""
	}
	found := strings.Fields(strings.TrimSpace(string(out)))
	if len(found) == 0 {
		// Nothing on the PATH is not the same as nothing installed. An SSH
		// command runs a non-interactive shell, which reads no profile, so an
		// agent in ~/.local/bin — where Claude's own installer puts it — is
		// invisible here while being perfectly runnable.
		//
		// Looking in the usual places turns "no agents found" into something
		// the user can act on, and names the exact value the extra PATH field
		// wants.
		if dir, names := agentsOffThePath(ctx, client, target); dir != "" {
			return StepAttention,
				"detail.agentsOffThePath|" + strings.Join(names, ", ") + "|" + dir, dir
		}
		// Attention, not a failure: a server with no agent still runs terminal
		// sessions, which is a large part of why someone adds one. The line
		// says what is missing and what it costs, so the server is not read as
		// broken when it is merely bare.
		return StepAttention, "detail.noAgentsFound", ""
	}
	return StepOK, strings.Join(found, ", "), ""
}

// DiscoverAgentPath returns a directory holding agents that the server's
// non-interactive shell cannot see, or empty when there is nothing to add.
//
// Exported so a connection can apply it without the user being asked. Finding
// the directory and then requiring someone to copy it into a settings field is
// work the program can do itself: the answer is the same either way, and the
// user learns about the problem only as a tab that will not start.
func DiscoverAgentPath(ctx context.Context, client *Client, target *Target) string {
	// Only when nothing is reachable as things stand. A server whose agents
	// are already on the PATH needs no addition, and probing for one would
	// risk preferring some other copy over the one it is set up to use.
	var builder strings.Builder
	for _, command := range AgentCommands {
		fmt.Fprintf(&builder, "command -v %s >/dev/null 2>&1 && echo %s; ", command, command)
	}
	builder.WriteString("true")

	out, err := client.Run(ctx, withExtraPath(target, builder.String()))
	if err != nil {
		return ""
	}
	if len(strings.Fields(strings.TrimSpace(string(out)))) > 0 {
		return ""
	}

	dir, _ := agentsOffThePath(ctx, client, target)
	return dir
}

// agentDirectories are where an agent commonly lands when it is not on a
// non-interactive shell's PATH.
//
// Kept short and specific: this runs while a dialog waits, and a wide search
// of the filesystem would be both slow and a good way to find something that
// is not what the user meant.
var agentDirectories = []string{
	"$HOME/.local/bin",
	"$HOME/bin",
	"$HOME/.npm-global/bin",
	"/usr/local/bin",
	"/opt/homebrew/bin",
}

// agentsOffThePath looks for agents in the usual install locations and reports
// the first directory holding any, with their names.
func agentsOffThePath(ctx context.Context, client *Client, target *Target) (string, []string) {
	var builder strings.Builder
	// Each directory is reported with what it holds, so one round trip answers
	// both "where" and "which".
	for _, dir := range agentDirectories {
		for _, command := range AgentCommands {
			fmt.Fprintf(&builder, `[ -x %s/%s ] && echo "%s %s"; `, dir, command, dir, command)
		}
	}

	// Same reasoning as above: none of these tests finding anything is an
	// answer, not a failure to ask.
	builder.WriteString("true")

	out, err := client.Run(ctx, builder.String())
	if err != nil {
		return "", nil
	}

	return firstDirectoryWithAgents(string(out))
}

// firstDirectoryWithAgents groups the probe's output by directory and returns
// the first one, with what it holds.
//
// Split out so it can be tested without a server.
func firstDirectoryWithAgents(out string) (string, []string) {
	// Grouped by directory, keeping the probe's order: the first match is the
	// most likely place and the one worth recommending.
	byDirectory := make(map[string][]string)
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		if _, seen := byDirectory[fields[0]]; !seen {
			order = append(order, fields[0])
		}
		byDirectory[fields[0]] = append(byDirectory[fields[0]], fields[1])
	}
	if len(order) == 0 {
		return "", nil
	}
	return order[0], byDirectory[order[0]]
}

// withExtraPath prefixes a command with the PATH addition configured for the
// server.
//
// An SSH command runs a non-interactive shell, which does not read .bashrc —
// so an agent installed in ~/.local/bin or through nvm is on the PATH when the
// user logs in and missing when we run it.
func withExtraPath(target *Target, command string) string {
	extra := strings.TrimSpace(target.ExtraPath)
	if extra == "" {
		return command
	}
	return fmt.Sprintf("export PATH=%s:$PATH; %s", shellQuote(extra), command)
}

// shellQuote wraps a value in single quotes so a path with a space in it stays
// one argument, and nothing in it is interpreted by the shell.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// classifyDialError turns a connection failure into the step it failed at.
func classifyDialError(result *CheckResult, target *Target, err error) {
	var unknownKey *UnknownHostKeyError
	if errors.As(err, &unknownKey) {
		result.add(StepConnect, StepOK, target.address())
		result.add(StepHostKey, StepAttention, unknownKey.Fingerprint)
		result.HostKey = unknownKey.Fingerprint
		result.HostKeyIsNew = true
		return
	}

	var changedKey *HostKeyMismatchError
	if errors.As(err, &changedKey) {
		result.add(StepConnect, StepOK, target.address())
		result.add(StepHostKey, StepFailed, changedKey.Error())
		result.HostKey = changedKey.Actual
		result.HostKeyChanged = true
		return
	}

	var needsPassphrase *PassphraseRequiredError
	if errors.As(err, &needsPassphrase) {
		result.add(StepAuth, StepAttention, needsPassphrase.Error())
		result.NeedsPassphrase = true
		return
	}

	// Authentication failures are reported against the auth step rather than
	// the connection: the machine answered, it just did not accept us.
	message := err.Error()
	if strings.Contains(message, "unable to authenticate") ||
		strings.Contains(message, "no supported methods") ||
		strings.Contains(message, "ssh: handshake failed") {
		result.add(StepConnect, StepOK, target.address())
		result.add(StepAuth, StepFailed, message)
		return
	}
	result.add(StepConnect, StepFailed, message)
}

func describeAuth(target *Target) string {
	switch target.AuthMethod {
	case "password":
		return "detail.authPassword"
	case "key":
		// The path itself, which is the useful part and needs no translating.
		return target.KeyPath
	default:
		return "detail.authAgent"
	}
}

// normaliseArch maps what uname reports to the architectures the helper is
// built for. An empty result means unsupported.
func normaliseArch(unameOutput string) string {
	switch strings.TrimSpace(unameOutput) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	default:
		return ""
	}
}
