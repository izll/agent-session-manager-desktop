package remote

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// Session names, window names and capture formats all reach the shell from
// user input or from tmux's own output. A window called "my notes" must arrive
// as one argument, and a format string full of #{} braces must arrive as it
// left — not as something the shell expanded on the way.
func TestEveryArgumentIsQuoted(t *testing.T) {
	executor := NewExecutor(nil, "server", "")

	command := executor.command([]string{
		"list-windows", "-t", "my session", "-F", "#{window_index}:#{window_name}",
	})

	if !strings.Contains(command, `'my session'`) {
		t.Errorf("a session name with a space was not quoted: %s", command)
	}
	if !strings.Contains(command, `'#{window_index}:#{window_name}'`) {
		t.Errorf("a format string was not quoted, so the shell would rewrite it: %s", command)
	}
	if !strings.HasPrefix(command, "tmux ") {
		t.Errorf("command does not start with tmux: %s", command)
	}
}

// A name containing a quote would otherwise end the quoting and hand the rest
// of it to the shell as commands. Session names are typed by the user, so this
// is reachable input rather than a hypothetical.
//
// Checked by asking a real shell to split the command, because that is the
// thing whose opinion matters. Counting quotes by hand gets this wrong: the
// POSIX escape is '\” — close, an escaped quote outside the quoting, open
// again — and a naive count reads that as unbalanced.
func TestQuotesInsideNamesCannotEscape(t *testing.T) {
	// Asks a real shell to split the command, because the question is what a
	// shell does with it — not what we believe it does. That shell is the one
	// on the server, which is always Unix; there is none to ask on Windows,
	// and the quoting this checks is only ever used against a Unix server.
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX shell to ask; the quoting is only used against Unix servers")
	}

	dangerous := `it's; rm -rf /`
	command := NewExecutor(nil, "server", "").command([]string{"new-session", "-s", dangerous})

	// Replace tmux with printf so the shell shows what it would have passed,
	// one argument per line, without running anything.
	shown := strings.Replace(command, "tmux ", `printf '%s\n' `, 1)
	out, err := exec.Command("/bin/sh", "-c", shown).Output()
	if err != nil {
		t.Fatalf("the command is not even valid shell: %v\n%s", err, command)
	}

	args := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(args) != 3 {
		t.Fatalf("the shell split this into %d arguments, want 3: %q\nfrom: %s",
			len(args), args, command)
	}
	if args[2] != dangerous {
		t.Errorf("the session name arrived as %q, want %q — the shell rewrote it "+
			"on the way", args[2], dangerous)
	}
}

// The PATH addition is what makes a tmux installed outside the system
// directories visible to a non-interactive shell.
func TestExtraPathIsAppliedToTmuxCommands(t *testing.T) {
	plain := NewExecutor(nil, "server", "").command([]string{"list-sessions"})
	if strings.Contains(plain, "PATH") {
		t.Errorf("a server with no extra PATH had one added: %s", plain)
	}

	withPath := NewExecutor(nil, "server", "/opt/tools/bin").command([]string{"list-sessions"})
	if !strings.Contains(withPath, "export PATH='/opt/tools/bin':$PATH") {
		t.Errorf("the extra PATH is missing or unquoted: %s", withPath)
	}
	if !strings.Contains(withPath, "tmux 'list-sessions'") {
		t.Errorf("the command was lost: %s", withPath)
	}
}

// An error from a remote tmux is what the user sees when a session has gone
// missing on the server. "exit status 1" explains nothing; tmux's own message
// says which session and why.
func TestErrorsPreferTmuxOwnMessage(t *testing.T) {
	message := firstLine("", "can't find session: asmgr_work\n")
	if message != "can't find session: asmgr_work" {
		t.Errorf("message = %q", message)
	}

	// stderr wins, because that is where tmux writes its complaints.
	fromStderr := firstLine("no server running on /tmp/tmux-0/default\n", "some output")
	if fromStderr != "no server running on /tmp/tmux-0/default" {
		t.Errorf("stderr was not preferred: %q", fromStderr)
	}

	// Blank lines are skipped rather than reported as the message.
	skipped := firstLine("\n\n  \n", "the real line\n")
	if skipped != "the real line" {
		t.Errorf("blank lines were reported as the message: %q", skipped)
	}

	if firstLine("", "") != "no output" {
		t.Error("an empty failure leaves the user with nothing at all")
	}
}

func TestDescribeNamesTheServer(t *testing.T) {
	if got := NewExecutor(nil, "build box", "").Describe(); got != "build box" {
		t.Errorf("Describe() = %q", got)
	}
}
