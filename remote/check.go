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
		result.add(StepMultiplexer, StepFailed,
			"tmux is not installed on this server, or is not on the PATH of a non-interactive shell")
	}

	// uname is how a Unix server names its architecture, and its absence says
	// more than its failure: a machine without it is not one the helper can be
	// built for. Reported as such rather than as "exited with status 1", which
	// is what the user would otherwise be left to interpret.
	arch, archErr := client.Run(ctx, "uname -m")
	trimmed := strings.TrimSpace(string(arch))
	switch {
	case archErr != nil || trimmed == "":
		result.add(StepArch, StepFailed,
			"this does not look like a Unix server — sessions can only run on Linux or macOS")
		result.OK = false
	case normaliseArch(trimmed) == "":
		// The helper is shipped for amd64 and arm64. Anything else cannot run
		// it, and saying so now beats failing at install time.
		result.add(StepArch, StepFailed, fmt.Sprintf("unsupported architecture %q", trimmed))
		result.OK = false
	default:
		result.add(StepArch, StepOK, trimmed)
	}

	agentsStatus, agentsDetail := agentStatus(ctx, client, target)
	result.add(StepAgents, agentsStatus, agentsDetail)
	return result
}

// agentStatus lists the agents present on the server.
func agentStatus(ctx context.Context, client *Client, target *Target) (StepStatus, string) {
	// One command rather than one per agent: each is a round trip, and this
	// runs while a dialog waits.
	var builder strings.Builder
	for _, command := range AgentCommands {
		fmt.Fprintf(&builder, "command -v %s >/dev/null 2>&1 && echo %s; ", command, command)
	}

	out, err := client.Run(ctx, withExtraPath(target, builder.String()))
	if err != nil {
		return StepSkipped, ""
	}
	found := strings.Fields(strings.TrimSpace(string(out)))
	if len(found) == 0 {
		return StepAttention, "no agents found — check the extra PATH setting"
	}
	return StepOK, strings.Join(found, ", ")
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
		return "password"
	case "key":
		return target.KeyPath
	default:
		return "ssh-agent"
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
