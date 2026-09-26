package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A drag inside a pane is tmux's event, not the web terminal's.
//
// Sessions are started with `mouse on`, so xterm.js never builds a selection
// from a plain drag — measured on a real machine: Shift-less mouseup reported a
// zero-length selection while a Shift-held one reported 53 characters. That is
// why the copy-on-select setting has to reach tmux's key bindings; acting on it
// in the browser alone can never work, and for a long time didn't.
func TestSelectModeCopiesOnADrag(t *testing.T) {
	joined := strings.Join(MouseCopyBinding("copy-mode-vi", true), " ")

	if !strings.Contains(joined, "MouseDragEnd1Pane") {
		t.Errorf("the binding must fire at the end of a drag; got %q", joined)
	}
	// copy-selection, and deliberately not copy-selection-and-cancel.
	//
	// This once required the -and-cancel form, on the grounds that without it
	// the pane stays in copy mode and swallows the next keystroke. That is
	// true of a mode entered with -H; the mode is now entered with -e, which
	// ends by itself once the view is back at the bottom. Cancelling here also
	// returned the view to the bottom, losing the place the user had scrolled
	// to in order to select something there.
	if !strings.Contains(joined, "copy-selection") {
		t.Errorf("a drag must copy; got %q", joined)
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

func TestSetMouseCopyEnabledContextHasOneOverallDeadline(t *testing.T) {
	if testing.Short() || runtime.GOOS == "windows" {
		t.Skip("uses a helper process")
	}
	bin := filepath.Join(t.TempDir(), "wedged-tmux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldBinary := TmuxBinary()
	SetTmuxBinary(bin)
	t.Cleanup(func() { SetTmuxBinary(oldBinary) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	SetMouseCopyEnabledContext(ctx, true)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("binding all tables multiplied the deadline: %v", elapsed)
	}
}

// A double click is a separate binding, and tmux's default for it copies.
//
// Binding only the drag left a double click filling the clipboard with the
// setting off — observed, with the paste buffers holding single words. Both
// clicks have to follow the setting for it to mean anything.
func TestClickSelectionFollowsTheSetting(t *testing.T) {
	on := strings.Join(ClickSelectBinding("copy-mode-vi", "DoubleClick1Pane", "select-word", true), " ")
	// copy-selection, not copy-selection-and-cancel: what matters is that it
	// copies. The -and-cancel form also returns the view to the bottom, which
	// loses the place a user scrolled to in order to select something there.
	if !strings.Contains(on, "copy-selection") {
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
	if !strings.Contains(on, "copy-selection") {
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

// A pane must never be left in copy mode with no way out but "q".
//
// That was the bug behind what read as a freeze: the mode was entered with -H,
// which hides the indicator, and two bindings ended without leaving it. The
// pane then swallowed every keystroke with nothing on screen to explain why.
// Measured on a real pane: it ran 0 of the commands typed into it, and 2 of 2
// after q.
//
// The fix is the entry, not the ending. -e leaves the mode by itself once the
// view is back at the bottom, so no ending has to cancel — and none may,
// because cancelling also returns the view to the bottom, throwing away the
// place a user had scrolled to in order to select something there.
func TestCopyModeIsEnteredSoItCanEndByItself(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		for key, selector := range clickSelectKeys {
			joined := strings.Join(RootClickBinding(key, selector, enabled), " ")
			if !strings.Contains(joined, "copy-mode -e") {
				t.Errorf("root %s (enabled=%v) does not enter copy mode with -e, "+
					"so the pane cannot leave the mode on its own:\n  %s",
					key, enabled, joined)
			}
			if strings.Contains(joined, "copy-mode -H") {
				t.Errorf("root %s (enabled=%v) hides the copy-mode indicator; "+
					"a stranded pane then gives the user nothing to go on:\n  %s",
					key, enabled, joined)
			}
		}
	}
}

// Nothing may cancel while the view is scrolled up.
//
// cancel — and the -and-cancel endings — return the view to the bottom. A
// selection is often made after scrolling up to find something, and jumping
// back to the end at that moment loses exactly what the user was looking at.
// At the bottom it moves nothing, and there the mode has to end (see
// TestASelectionAtTheBottomLeavesCopyMode).
func TestNoBindingThrowsAwayTheScrollPosition(t *testing.T) {
	// The part of a binding that runs while scrolled up: the first branch of
	// an if-shell on scrolledUp, and nothing under an if-shell on atBottom.
	scrolledPart := func(args []string) string {
		var kept []string
		for i := 0; i < len(args); i++ {
			if args[i] == "if-shell" && i+2 < len(args) && args[i+1] == "-F" {
				switch args[i+2] {
				case scrolledUp:
					if i+3 < len(args) {
						kept = append(kept, args[i+3])
					}
					i += 4
					continue
				case atBottom:
					i += 3
					continue
				}
			}
			kept = append(kept, args[i])
		}
		return strings.Join(kept, " ")
	}
	check := func(what string, args []string) {
		t.Helper()
		if part := scrolledPart(args); strings.Contains(part, "cancel") {
			t.Errorf("%s cancels while scrolled up, which returns the view to the "+
				"bottom and loses the place the user scrolled to:\n  %s", what, strings.Join(args, " "))
		}
	}

	for _, enabled := range []bool{true, false} {
		for _, table := range copyModeTables {
			check("drag end", MouseCopyBinding(table, enabled))
			for key, selector := range clickSelectKeys {
				check("click "+key, ClickSelectBinding(table, key, selector, enabled))
			}
		}
	}
}

// A selection made without scrolling up left the pane in copy mode: a drag
// enters it through tmux's own copy-mode -M, without -e, so nothing ended it,
// keystrokes went to tmux and every later click in the window was taken as a
// selection. At the bottom every selection now leaves the mode.
func TestASelectionAtTheBottomLeavesCopyMode(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		for _, table := range copyModeTables {
			drag := MouseCopyBinding(table, enabled)
			if n := len(drag); n < 2 || drag[n-5] != "if-shell" || drag[n-3] != scrolledUp || !strings.Contains(drag[n-1], "cancel") {
				t.Errorf("drag end (%s, enabled=%v) does not leave the mode at the bottom:\n  %s",
					table, enabled, strings.Join(drag, " "))
			}
			for key, selector := range clickSelectKeys {
				click := strings.Join(ClickSelectBinding(table, key, selector, enabled), " ")
				if !strings.HasSuffix(click, "if-shell -F "+atBottom+" send-keys -X cancel") {
					t.Errorf("%s in %s (enabled=%v) does not leave the mode at the bottom:\n  %s",
						key, table, enabled, click)
				}
			}
		}
		// A root click comes from a pane not in the mode, which is at the
		// bottom by definition.
		for key, selector := range clickSelectKeys {
			root := RootClickBinding(key, selector, enabled)
			if action := root[len(root)-1]; !strings.HasSuffix(action, "send-keys -X cancel") {
				t.Errorf("root %s (enabled=%v) stays in copy mode: %s", key, enabled, action)
			}
		}
	}
}

// The same, against a real tmux on a socket of its own: the bindings are
// stored whole, the condition reads the scroll position, and a drag's end
// leaves the mode at the bottom but not scrolled up.
func TestSelectionEndingAgainstTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("psmux has no copy-mode-vi table")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	socket := fmt.Sprintf("asmgr-copy-%d", os.Getpid())
	tm := func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-L", socket, "-f", os.DevNull}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	defer tm("kill-server")
	tm("new-session", "-d", "-s", "p", "-x", "80", "-y", "10", "seq 1 200; sleep 60")
	tm(MouseCopyBinding("copy-mode-vi", true)...)
	if got := tm("list-keys", "-T", "copy-mode-vi", "MouseDragEnd1Pane"); !strings.Contains(got, "copy-selection-and-cancel") {
		t.Fatalf("the drag binding was not stored whole: %q", got)
	}

	dragEnd := func() {
		args := MouseCopyBinding("copy-mode-vi", true)
		at := len(args) - 5 // the if-shell and its arguments
		tm(append([]string{"if-shell", "-t", "p"}, args[at+1:]...)...)
	}
	inMode := func() string { return tm("display", "-p", "-t", "p", "#{pane_in_mode}") }

	tm("copy-mode", "-t", "p")
	tm("send-keys", "-t", "p", "-X", "page-up")
	dragEnd()
	if inMode() != "1" {
		t.Error("a selection made scrolled up left copy mode, throwing the view to the bottom")
	}
	tm("send-keys", "-t", "p", "-X", "history-bottom")
	if inMode() != "1" {
		t.Skip("this tmux ends the mode at the bottom by itself")
	}
	dragEnd()
	if inMode() != "0" {
		t.Error("a selection at the bottom left the pane in copy mode")
	}
}
