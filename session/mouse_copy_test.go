package session

import (
	"strings"
	"testing"
)

// A drag inside a pane is tmux's event, not the web terminal's.
//
// Sessions are started with `mouse on`, so xterm.js never builds a selection
// from a plain drag — measured on a real machine: Shift-less mouseup reported a
// zero-length selection while a Shift-held one reported 53 characters. That is
// why the copy-on-select setting has to reach tmux's key bindings; acting on it
// in the browser alone can never work, and for a long time didn't.
func TestSelectModeCopiesAndLeavesCopyMode(t *testing.T) {
	joined := strings.Join(MouseCopyBinding("copy-mode-vi", true), " ")

	if !strings.Contains(joined, "MouseDragEnd1Pane") {
		t.Errorf("the binding must fire at the end of a drag; got %q", joined)
	}
	if !strings.Contains(joined, "copy-selection-and-cancel") {
		// Without -and-cancel the pane stays in copy mode and swallows the next
		// keystroke instead of typing it.
		t.Errorf("a drag must copy and leave copy mode; got %q", joined)
	}
}

// Nothing may pipe to an external clipboard tool.
//
// xclip is the conventional choice and the reason this bug hid for so long: one
// of the two test machines had it wired up in a hand-written ~/.tmux.conf, so
// copy-on-select appeared to work there and nowhere else. It is not installed
// by default on Ubuntu or Fedora, and neither are xsel or wl-copy — the machine
// at 192.168.1.38 had none of the three. OSC 52 needs none of them.
func TestNothingDependsOnAnExternalClipboardTool(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		joined := strings.Join(MouseCopyBinding("copy-mode", enabled), " ")
		for _, tool := range []string{"xclip", "xsel", "wl-copy", "pbcopy"} {
			if strings.Contains(joined, tool) {
				t.Errorf("binding depends on %q, which most users do not have installed: %q", tool, joined)
			}
		}
	}
}

// Shift mode must not put anything on the clipboard.
//
// Copy-on-select is opt-in: someone who only meant to highlight text should not
// find it on their clipboard.
func TestShiftModeDoesNotCopy(t *testing.T) {
	joined := strings.Join(MouseCopyBinding("copy-mode-vi", false), " ")

	// Any copy-selection variant writes tmux's paste buffer, and with
	// set-clipboard on, a buffer write is what sends OSC 52.
	// copy-selection-no-clear is the trap: the "-no-clear" names the selection,
	// not the clipboard, so binding it made shift mode copy on every plain drag
	// and the setting appeared to do nothing at all.
	if strings.Contains(joined, "copy-selection") {
		t.Errorf("shift mode must not copy on a plain drag, and every "+
			"copy-selection variant copies; got %q", joined)
	}
	if !strings.Contains(joined, "clear-selection") {
		// Leaving it unbound would break dragging altogether rather than just
		// not copying.
		t.Errorf("shift mode should end the drag without copying; got %q", joined)
	}
}

// Both mode-keys tables have to be bound.
//
// tmux dispatches to copy-mode or copy-mode-vi depending on the user's
// mode-keys setting. Binding only one leaves the feature broken for whichever
// half of users has the other — invisible to whoever wrote the binding.
func TestBothCopyModeTablesAreCovered(t *testing.T) {
	if len(copyModeTables) != 2 {
		t.Fatalf("expected both copy-mode tables, got %v", copyModeTables)
	}
	for _, table := range copyModeTables {
		joined := strings.Join(MouseCopyBinding(table, true), " ")
		if !strings.Contains(joined, "-T "+table) {
			t.Errorf("table %q is not addressed: %q", table, joined)
		}
	}
}

// A double click is a separate binding, and tmux's default for it copies.
//
// Binding only the drag left a double click filling the clipboard with the
// setting off — observed, with the paste buffers holding single words. Both
// clicks have to follow the setting for it to mean anything.
func TestClickSelectionFollowsTheSetting(t *testing.T) {
	on := strings.Join(ClickSelectBinding("copy-mode-vi", "DoubleClick1Pane", "select-word", true), " ")
	if !strings.Contains(on, "copy-selection-and-cancel") {
		t.Errorf("in select mode a double click should copy; got %q", on)
	}

	off := strings.Join(ClickSelectBinding("copy-mode-vi", "DoubleClick1Pane", "select-word", false), " ")
	if strings.Contains(off, "copy-selection") || strings.Contains(off, "copy-pipe") {
		t.Errorf("in shift mode a double click must select without copying; got %q", off)
	}
	if !strings.Contains(off, "select-word") {
		t.Errorf("the word should still be selected, just not copied; got %q", off)
	}
}

// The command separator must be an escaped semicolon.
//
// A bare ";" terminates the bind-key command itself: tmux then binds only
// select-pane and executes the remainder straight away, answering "not in a
// mode" — measured. The click keeps tmux's copying default, so the setting
// silently does nothing, which is the exact failure this whole area keeps
// producing.
func TestClickBindingSeparatorIsEscaped(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		args := ClickSelectBinding("copy-mode", "TripleClick1Pane", "select-line", enabled)
		for _, arg := range args {
			if arg == ";" {
				t.Errorf("a bare %q ends bind-key early; it has to be %q: %v", ";", "\\;", args)
			}
		}
		if !contains(args, "\\;") {
			t.Errorf("the sub-commands must be separated by an escaped semicolon: %v", args)
		}
	}
}

func contains(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

// The root table is the binding that actually decides for a click.
//
// A pane is normally not in copy mode, so a click lands in the root table,
// whose tmux default enters copy mode, selects and copies in one go — never
// reaching the copy-mode table binding. Binding only the copy-mode tables
// therefore changed nothing at all: every check showed the "off" branch bound
// while a double click kept filling the clipboard.
func TestRootClickBindingDecides(t *testing.T) {
	on := strings.Join(RootClickBinding("DoubleClick1Pane", "select-word", true), " ")
	if !strings.Contains(on, "-T root") {
		t.Errorf("the click has to be bound in the root table; got %q", on)
	}
	if !strings.Contains(on, "copy-selection-and-cancel") {
		t.Errorf("select mode should copy on a double click; got %q", on)
	}

	off := strings.Join(RootClickBinding("DoubleClick1Pane", "select-word", false), " ")
	if strings.Contains(off, "copy-selection") || strings.Contains(off, "copy-pipe") {
		t.Errorf("shift mode must not copy on a double click; got %q", off)
	}
	if !strings.Contains(off, "select-word") {
		t.Errorf("the word should still be selected; got %q", off)
	}
}

// A pane reading the mouse itself must keep receiving the event.
//
// That passthrough is what lets an agent handle its own clicks — Claude Code in
// fullscreen mode does exactly this. Dropping the condition would take the
// mouse away from every such program.
func TestRootClickPassesThroughToProgramsReadingTheMouse(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		joined := strings.Join(RootClickBinding("TripleClick1Pane", "select-line", enabled), " ")
		if !strings.Contains(joined, "mouse_any_flag") {
			t.Errorf("a program reading the mouse must still get the event; got %q", joined)
		}
		if !strings.Contains(joined, "send-keys -M") {
			t.Errorf("passthrough sends the event on unchanged; got %q", joined)
		}
	}
}

// The if-shell branches must be plain strings, not { } blocks.
//
// tmux's own default is written with braces, but those are config-file syntax.
// Passed as exec arguments they arrive as ordinary text, the braces end up
// inside the command, and tmux reports a syntax error in the status line when
// the key fires — which is what happened on the first attempt.
func TestRootClickBindingAvoidsBraceBlocks(t *testing.T) {
	for _, arg := range RootClickBinding("DoubleClick1Pane", "select-word", true) {
		// A command block opens with "{ " and closes with " }". A format
		// expression like #{mouse_any_flag} also contains braces and is
		// perfectly valid — matching on braces alone flags it wrongly.
		if strings.HasPrefix(arg, "{ ") || strings.HasSuffix(arg, " }") {
			t.Errorf("brace blocks are config-file syntax and fail as arguments: %q", arg)
		}
	}
}

// tmux only emits OSC 52 if the terminal claims the Ms capability.
//
// It assumes it for xterm* automatically, but the TERM used here is
// tmux-256color. Without the override the option is set, the binding is right,
// and nothing is ever sent — a silent failure that looks exactly like the bug
// being fixed.
func TestTerminalAdvertisesTheClipboardCapability(t *testing.T) {
	if !strings.Contains(terminalClipboardCapability, "Ms=") {
		t.Errorf("the Ms capability is what enables OSC 52; got %q", terminalClipboardCapability)
	}
	if !strings.Contains(terminalClipboardCapability, "tmux-256color") {
		t.Errorf("the capability has to name the TERM actually in use; got %q", terminalClipboardCapability)
	}
	if !strings.Contains(terminalClipboardCapability, "52") {
		t.Errorf("the escape sequence must be OSC 52; got %q", terminalClipboardCapability)
	}
}

// The clipboard options are server- and global-scoped, never per session.
//
// The terminal server creates its own mirror sessions for attaching, and those
// never pass through session start-up. Scoping the options to a session
// therefore left the one the user was actually looking at without them —
// measured: a running server whose terminal-overrides contained no Ms entry at
// all, despite every session having been started through the app.
func TestClipboardForwardingIsNotScopedToOneSession(t *testing.T) {
	// The function takes no session argument, which is the guarantee: there is
	// nothing to scope it to. This test exists to fail loudly if someone adds
	// one back.
	var f func() = ConfigureClipboardForwarding
	if f == nil {
		t.Fatal("ConfigureClipboardForwarding must stay callable without a session")
	}
}
