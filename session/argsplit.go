package session

import "strings"

// SplitArgs splits a command-argument string into individual argv tokens
// using POSIX-shell-like quoting rules, but WITHOUT any shell evaluation:
//
//   - whitespace separates tokens
//   - single quotes '...' protect everything literally (no escapes inside)
//   - double quotes "..." group, with backslash escaping only \" \\ \$ \`
//     (we keep $ and ` literal — there is no expansion here at all)
//   - a backslash outside quotes escapes the next character literally
//
// Crucially there is NO variable/command/glob expansion. The result is
// passed as separate argv elements to tmux (new-session/new-window/
// respawn-pane), which then execs the program directly — no `sh -c`
// layer — so `$HOME`, `;`, `|`, `$(...)`, backticks etc. inside the
// Extra-Args / Custom-Command fields are inert literals, not injection.
//
// An unterminated quote is treated leniently: the token collected so far
// is returned (we never want a typo in the Extra-Args field to abort a
// session launch). Empty input yields nil.
func SplitArgs(s string) []string {
	var args []string
	var buf strings.Builder
	inToken := false

	const (
		none = iota
		single
		double
	)
	quote := none

	flush := func() {
		if inToken {
			args = append(args, buf.String())
			buf.Reset()
			inToken = false
		}
	}

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch quote {
		case single:
			if c == '\'' {
				quote = none
			} else {
				buf.WriteRune(c)
			}
		case double:
			if c == '\\' && i+1 < len(runes) {
				n := runes[i+1]
				// In double quotes only these are real escapes; anything
				// else keeps the backslash literally (POSIX behaviour).
				if n == '"' || n == '\\' || n == '$' || n == '`' {
					buf.WriteRune(n)
					i++
				} else {
					buf.WriteRune(c)
				}
			} else if c == '"' {
				quote = none
			} else {
				buf.WriteRune(c)
			}
		default: // none
			switch {
			case c == ' ' || c == '\t' || c == '\n' || c == '\r':
				flush()
			case c == '\'':
				inToken = true
				quote = single
			case c == '"':
				inToken = true
				quote = double
			case c == '\\' && i+1 < len(runes):
				inToken = true
				buf.WriteRune(runes[i+1])
				i++
			default:
				inToken = true
				buf.WriteRune(c)
			}
		}
	}
	// Unterminated quote: keep whatever we have rather than failing.
	flush()
	return args
}

// buildAgentArgv assembles the full argv for a normal (non-custom) agent:
// the base command, the already-built flag tokens, then the user's
// Extra-Args field split with SplitArgs. Returned as a []string so the
// caller can hand it to tmux as separate arguments (no `sh -c`).
func buildAgentArgv(command string, flagArgs []string, extraArgs string) []string {
	argv := make([]string, 0, 1+len(flagArgs)+4)
	argv = append(argv, command)
	argv = append(argv, flagArgs...)
	if extraArgs != "" {
		argv = append(argv, SplitArgs(extraArgs)...)
	}
	return argv
}

// ExtraArgsSetConversation reports whether a user's extra arguments already
// choose which conversation the agent opens.
//
// The app adds a conversation flag of its own — "--session-id <uuid>" for a
// fresh one, "--resume <id>" for a stored one — so a user who types their own
// gets both, and the agent refuses:
//
//	Error: --session-id can only be used with --continue or --resume
//	       if --fork-session is also specified.
//
// The user's choice wins: they typed a specific conversation, which is more
// deliberate than the one we would have generated.
//
// Matched on the flags every supported agent uses for this, including the
// "=value" form, since a flag written that way is one token.
func ExtraArgsSetConversation(extraArgs string) bool {
	for _, token := range SplitArgs(extraArgs) {
		name := token
		if at := strings.Index(name, "="); at > 0 {
			name = name[:at]
		}
		if conversationFlags[name] {
			return true
		}
	}
	return false
}

// conversationFlags are the ways an agent is told which conversation to open.
//
// Taken from the agent configurations rather than guessed: every ResumeFlag
// and SessionIDFlag in use, plus the shorthands the CLIs accept.
var conversationFlags = map[string]bool{
	"--resume":       true,
	"-r":             true,
	"--continue":     true,
	"-c":             true,
	"--session-id":   true,
	"--conversation": true,
	"--fork-session": true,
}

// customCommandArgv splits a Custom-agent command line into argv tokens.
// Falls back to a single token if splitting yields nothing but the input
// was non-empty (so a weird value still launches something visible).
func customCommandArgv(cmd string) []string {
	a := SplitArgs(cmd)
	if len(a) == 0 && strings.TrimSpace(cmd) != "" {
		return []string{cmd}
	}
	return a
}
