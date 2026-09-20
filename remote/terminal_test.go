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
	command := mirrorAttachCommand("work:0", "view_work_0", "", 80, 24)

	if !strings.Contains(command, "LANG=en_US.UTF-8") ||
		!strings.Contains(command, "LC_ALL=en_US.UTF-8") {
		t.Errorf("the attach does not force a UTF-8 locale: %s", command)
	}
	if !strings.Contains(command, "attach-session") {
		t.Errorf("the attach was lost: %s", command)
	}
	// The window is linked into the mirror from the real session, so the
	// target it came from has to survive into the command.
	if !strings.Contains(command, "work:0") {
		t.Errorf("the source window is missing: %s", command)
	}
}

// Set on the command line rather than through SSH's environment request: a
// server only accepts variables its AcceptEnv allows — by default none of
// these — and a rejected one is not reported back. It would look like the
// locale was set when it was not.
func TestLocaleIsNotSentAsAnSSHEnvironmentRequest(t *testing.T) {
	command := mirrorAttachCommand("work:0", "view_work_0", "", 80, 24)
	if !strings.HasPrefix(command, "env LANG=") {
		t.Errorf("the locale is not part of the command: %s", command)
	}
}

// A session name reaches this from the user, and a window target is built from
// it. Quoting is what keeps a name with a space — or worse — from becoming
// several arguments.
func TestAttachTargetIsQuoted(t *testing.T) {
	command := mirrorAttachCommand("my work:2", "view_my_work_2", "", 80, 24)
	// Quoted twice over: the inner script is itself a quoted argument to
	// sh -c, so a bare name would break out of both.
	if !strings.Contains(command, `my work:2`) {
		t.Errorf("the target is missing: %s", command)
	}
	if strings.Contains(command, "-t my work:2 ") {
		t.Errorf("the target reached tmux unquoted: %s", command)
	}
}

// A tmux installed outside the system directories is invisible to a
// non-interactive shell, which is what an SSH command gets.
func TestAttachAppliesTheExtraPath(t *testing.T) {
	plain := mirrorAttachCommand("work:0", "view_work_0", "", 80, 24)
	if strings.Contains(plain, "export PATH") {
		t.Errorf("a server with no extra PATH had one added: %s", plain)
	}

	withPath := mirrorAttachCommand("work:0", "view_work_0", "/opt/tools/bin", 80, 24)
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

// Every tab of a session attaches to the same multiplexer session, and a
// multiplexer sizes a window to its SMALLEST client. With three tabs open the
// smallest decided for all of them, and the rows the others could have used
// came back as a band of dots — measured live at 223x60, 223x60 and 223x66,
// six rows dead.
//
// Each attach therefore goes through a mirror of its own window, whose only
// client is that tab.
func TestEachTabAttachesThroughItsOwnMirror(t *testing.T) {
	command := mirrorAttachCommand("work:7", "view_work_7", "", 100, 30)

	if !strings.Contains(command, "link-window") {
		t.Error("the window is no longer linked into a mirror; tabs will " +
			"share one size and the smallest will win")
	}
	if !strings.Contains(command, "new-session") {
		t.Error("no mirror is created")
	}
	// Created at this tab's size, so the first frame is drawn correctly rather
	// than corrected a moment later.
	if !strings.Contains(command, "-x 100") || !strings.Contains(command, "-y 30") {
		t.Errorf("the mirror is not created at the tab's size: %s", command)
	}
}

// A mirror is named after the window it shows. A fresh name per attach would
// leave the previous ones behind, each still counted as a client watching the
// window — which is the very thing the mirror exists to prevent.
func TestAMirrorIsNamedAfterItsWindow(t *testing.T) {
	first := mirrorNameFor("asm_claude_x:100")
	again := mirrorNameFor("asm_claude_x:100")
	other := mirrorNameFor("asm_claude_x:101")

	if first != again {
		t.Error("reattaching the same tab produces a different mirror each time")
	}
	if first == other {
		t.Error("two windows share one mirror")
	}
	// Characters a multiplexer treats specially in a target must not survive
	// into a session name.
	if strings.ContainsAny(first, ":.$") {
		t.Errorf("the mirror name carries a character tmux reads as a target: %q", first)
	}
}
