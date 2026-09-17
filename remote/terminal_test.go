package remote

import (
	"strings"
	"testing"
)

// UTF-8 is not a nicety here. Without it tmux mangles every accented character
// and every box-drawing character an agent draws — which is most of what an
// agent draws. The same fault was diagnosed once already on a local terminal,
// where a GUI launch inherits no locale at all.
func TestAttachForcesAUTF8Locale(t *testing.T) {
	command := attachCommand("work:0", "")

	if !strings.Contains(command, "LANG=en_US.UTF-8") ||
		!strings.Contains(command, "LC_ALL=en_US.UTF-8") {
		t.Errorf("the attach does not force a UTF-8 locale: %s", command)
	}
	if !strings.Contains(command, "tmux attach-session -t 'work:0'") {
		t.Errorf("the attach target is wrong or unquoted: %s", command)
	}
}

// Set on the command line rather than through SSH's environment request: a
// server only accepts variables its AcceptEnv allows — by default none of
// these — and a rejected one is not reported back. It would look like the
// locale was set when it was not.
func TestLocaleIsNotSentAsAnSSHEnvironmentRequest(t *testing.T) {
	command := attachCommand("work:0", "")
	if !strings.HasPrefix(command, "env LANG=") {
		t.Errorf("the locale is not part of the command: %s", command)
	}
}

// A session name reaches this from the user, and a window target is built from
// it. Quoting is what keeps a name with a space — or worse — from becoming
// several arguments.
func TestAttachTargetIsQuoted(t *testing.T) {
	command := attachCommand("my work:2", "")
	if !strings.Contains(command, `'my work:2'`) {
		t.Errorf("the target was not quoted: %s", command)
	}
}

// A tmux installed outside the system directories is invisible to a
// non-interactive shell, which is what an SSH command gets.
func TestAttachAppliesTheExtraPath(t *testing.T) {
	plain := attachCommand("work:0", "")
	if strings.Contains(plain, "export PATH") {
		t.Errorf("a server with no extra PATH had one added: %s", plain)
	}

	withPath := attachCommand("work:0", "/opt/tools/bin")
	if !strings.HasPrefix(withPath, "export PATH='/opt/tools/bin':$PATH; ") {
		t.Errorf("the extra PATH is missing or misplaced: %s", withPath)
	}
	if !strings.Contains(withPath, "tmux attach-session") {
		t.Errorf("the attach was lost: %s", withPath)
	}
}

// Attaching needs a running helper: it is the thing that proves the connection
// was set up rather than merely opened.
func TestAttachRefusesWithoutAHelper(t *testing.T) {
	client := &Client{}
	if _, err := client.AttachTerminalTo(nil, "work:0", "", 80, 24); err == nil {
		t.Error("a terminal was attached on a connection with no helper")
	}
}
