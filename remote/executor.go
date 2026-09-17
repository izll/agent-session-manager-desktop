package remote

import (
	"context"
	"fmt"
	"strings"
)

// Running multiplexer commands on a server.
//
// This is what the session package's Executor interface looks like from the
// remote side: the same tmux arguments it would run locally, sent through the
// helper instead.

// Executor issues tmux commands on one server.
type Executor struct {
	helper *Helper
	// extraPath is prepended to PATH on the server, for a tmux installed
	// somewhere a non-interactive shell does not look.
	extraPath string
	// name is what appears in logs and error messages.
	name string
}

// NewExecutor wires an executor to a live helper.
func NewExecutor(helper *Helper, name, extraPath string) *Executor {
	return &Executor{helper: helper, name: name, extraPath: extraPath}
}

// Run issues a command and reports only whether it worked.
func (e *Executor) Run(ctx context.Context, args ...string) error {
	result, err := e.helper.Run(ctx, e.command(args))
	if err != nil {
		return err
	}
	if result.TimedOut {
		return fmt.Errorf("tmux command timed out on %s", e.name)
	}
	if result.ExitCode != 0 {
		// The message matters: this is what surfaces when a session has gone
		// missing on the server, and "exit status 1" explains nothing.
		return fmt.Errorf("tmux %s failed on %s: %s",
			args[0], e.name, firstLine(result.Stderr, result.Output))
	}
	return nil
}

// Output issues a command and returns what it printed.
//
// Standard output only. The helper runs commands through a login shell — the
// one way an agent under ~/.local/bin is on the PATH at all — and a login
// shell prints messages of its own. Mixed in, those would corrupt every
// listing the session package parses.
func (e *Executor) Output(ctx context.Context, args ...string) ([]byte, error) {
	result, err := e.helper.Run(ctx, e.command(args))
	if err != nil {
		return nil, err
	}
	if result.TimedOut {
		return nil, fmt.Errorf("tmux command timed out on %s", e.name)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("tmux %s failed on %s: %s",
			args[0], e.name, firstLine(result.Stderr, result.Output))
	}
	return []byte(result.Output), nil
}

// Describe names the server, for logs.
func (e *Executor) Describe() string { return e.name }

// command turns tmux arguments into a shell command line.
//
// Every argument is quoted. Session names, window names and capture formats
// all reach here from user input and from tmux's own output — a window named
// with a space, or a format string full of braces and hashes, must arrive at
// tmux as it left, not as something the shell rewrote on the way.
func (e *Executor) command(args []string) string {
	var builder strings.Builder
	if extra := strings.TrimSpace(e.extraPath); extra != "" {
		builder.WriteString("export PATH=")
		builder.WriteString(shellQuote(extra))
		builder.WriteString(":$PATH; ")
	}
	builder.WriteString("tmux")
	for _, arg := range args {
		builder.WriteByte(' ')
		builder.WriteString(shellQuote(arg))
	}
	return builder.String()
}

// firstLine picks the most useful line for an error message, preferring
// stderr — which is where tmux says "can't find session" — and falling back to
// standard output.
func firstLine(candidates ...string) string {
	for _, candidate := range candidates {
		for _, line := range strings.Split(candidate, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				return trimmed
			}
		}
	}
	return "no output"
}

// RunShell runs a command other than tmux in a directory on the server.
//
// This is what the diff, the file browser and the history search need: their
// commands are git invocations and file reads against a working directory that
// lives on the far machine.
func (e *Executor) RunShell(ctx context.Context, dir string, args ...string) ([]byte, []byte, int, error) {
	if len(args) == 0 {
		return nil, nil, 0, fmt.Errorf("no command given")
	}

	var builder strings.Builder
	if extra := strings.TrimSpace(e.extraPath); extra != "" {
		builder.WriteString("export PATH=")
		builder.WriteString(shellQuote(extra))
		builder.WriteString(":$PATH; ")
	}
	for index, arg := range args {
		if index > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(shellQuote(arg))
	}

	result, err := e.helper.RunIn(ctx, dir, builder.String())
	if err != nil {
		return nil, nil, 0, err
	}
	if result.TimedOut {
		return nil, nil, 0, fmt.Errorf("%s timed out on %s", args[0], e.name)
	}
	return []byte(result.Output), []byte(result.Stderr), result.ExitCode, nil
}
