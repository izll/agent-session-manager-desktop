package session

import "log"

// A drag inside a pane belongs to tmux, not to the web terminal.
//
// Sessions are started with `mouse on`, so tmux reports mouse events itself and
// xterm.js never builds a selection from a plain drag — measured: a Shift-less
// drag reached mouseup with a zero-length selection while a Shift-held one
// carried the real text. Acting on the copy-on-select setting in the browser
// alone therefore cannot work, and for a long time didn't: the setting was
// read, the handler ran, and there was nothing to copy.
//
// tmux does copy the drag, into its own paste buffer. Getting that to the
// system clipboard is what OSC 52 is for: tmux base64-encodes the selection
// into an escape sequence and writes it to the outer terminal, which puts it on
// the clipboard. No helper process, no xclip, nothing to install — the text
// travels the pty that is already open.
//
// The usual binding pipes to xclip instead. That is exactly the trap this code
// avoids: xclip is not installed by default on Ubuntu or Fedora, and neither
// are xsel or wl-copy. A binding written against it works only for someone who
// already set one up by hand, which is precisely why copy-on-select appeared to
// work on one of the two test machines and nowhere else.

// terminalClipboardCapability advertises OSC 52 support to tmux.
//
// tmux will not emit the sequence unless the terminal claims the `Ms`
// capability through terminfo. It assumes it for xterm* automatically, but the
// TERM here is tmux-256color, so it has to be stated — without this the option
// below is set, the binding is right, and nothing is ever sent.
const terminalClipboardCapability = ",tmux-256color:Ms=\\E]52;%p1%s;%p2%s\\007"

// ConfigureClipboardForwarding makes a tmux session forward copies to the
// terminal's clipboard over OSC 52.
//
// set-clipboard is deliberately `external` rather than `on`: `on` additionally
// lets any program running inside a pane set the clipboard, which is a
// capability an agent with shell access should not be handed. `external` limits
// it to tmux's own copies — the drags and copy-mode yanks the user performed.
// Both options are set server- and globally-scoped rather than per session.
// The mirror sessions the terminal server creates for attaching never pass
// through here, so a per-session setting would leave whichever session the user
// is actually looking at without the capability — measured: a server whose
// terminal-overrides held no Ms entry at all despite sessions having been
// started through this function.
func ConfigureClipboardForwarding() {
	// -s: server option. One setting covers every session, including the
	// mirrors created elsewhere.
	TmuxCommand("set-option", "-s", "set-clipboard", "external").Run()
	// -ga: append to the global value. Without the Ms capability tmux never
	// emits OSC 52, no matter what set-clipboard says.
	TmuxCommand("set-option", "-ga", "terminal-overrides", terminalClipboardCapability).Run()
}

// MouseCopyBinding returns the tmux arguments binding the end of a drag for one
// key table.
//
// Both copy-mode tables are bound by the caller: tmux dispatches to copy-mode
// or copy-mode-vi depending on the user's mode-keys setting, and binding only
// one leaves the feature broken for whichever half of users has the other.
//
// copy-pipe is not involved. copy-selection-and-cancel puts the text in tmux's
// buffer, and set-clipboard turns that into an OSC 52 write by itself; the
// -and-cancel half leaves copy mode so the pane accepts typing straight away.
func MouseCopyBinding(table string, enabled bool) []string {
	if !enabled {
		// clear-selection, NOT copy-selection-no-clear.
		//
		// The "-no-clear" in that name refers to the selection, not the
		// clipboard: it still copies into tmux's paste buffer, and with
		// set-clipboard on, a buffer write is exactly what triggers the OSC 52
		// send. Bound to it, shift mode copied on every plain drag and the
		// setting appeared to do nothing — observed, with the binding verifiably
		// on the "off" branch while the clipboard kept filling.
		return []string{"bind-key", "-T", table, "MouseDragEnd1Pane",
			"send-keys", "-X", "clear-selection"}
	}
	return []string{"bind-key", "-T", table, "MouseDragEnd1Pane",
		"send-keys", "-X", "copy-selection-and-cancel"}
}

// ClickSelectBinding returns the binding for a double or triple click, which
// select a word and a line respectively.
//
// These are separate bindings from the drag, and tmux's defaults for both end
// in copy-pipe-and-cancel — so with set-clipboard on they copy unconditionally,
// no matter what the drag is bound to. Observed exactly that way: with the drag
// on the "off" branch and the setting on shift, a double click still filled the
// clipboard, and the resulting paste buffers held single words.
//
// selector is select-word or select-line; the rest matches the drag, so a
// double click obeys the same setting a drag does.
func ClickSelectBinding(table, key, selector string, enabled bool) []string {
	args := []string{"bind-key", "-T", table, key,
		// The separator has to be an escaped semicolon. A bare ";" ends the
		// bind-key command itself, so tmux binds only select-pane and runs the
		// rest immediately — which it answers with "not in a mode", and the
		// click keeps tmux's copying default.
		"select-pane", "\\;", "send-keys", "-X", selector, "\\;"}
	if !enabled {
		// Select but do not copy — the selection stays visible, which is what
		// a double click is for when copy-on-select is off.
		return append(args, "send-keys", "-X", "stop-selection")
	}
	return append(args, "send-keys", "-X", "copy-selection-and-cancel")
}

// clickSelectKeys maps each click binding to the selection it makes.
var clickSelectKeys = map[string]string{
	"DoubleClick1Pane": "select-word",
	"TripleClick1Pane": "select-line",
}

// RootClickBinding returns the root-table binding for a double or triple click.
//
// This is the one that actually decides, and missing it is why binding the
// copy-mode tables alone changed nothing. A pane is normally NOT in copy mode,
// so a click lands in the root table, and tmux's default there does the whole
// job itself:
//
//	copy-mode -H ; send-keys -X select-word ; run-shell -d 0.3 ; copy-pipe-and-cancel
//
// It enters copy mode, selects, and copies — never dispatching to the
// copy-mode table binding at all. Observed exactly that: with every copy-mode
// binding verifiably on the "off" branch, a double click still filled the
// clipboard.
//
// The -H flag hides the copy-mode indicator, and the run-shell -d 0.3 is tmux's
// own pause letting the selection render before it is taken. Both are kept: the
// only change is what happens at the end.
func RootClickBinding(key, selector string, enabled bool) []string {
	// When the pane is already in a mode, or the program inside it is reading
	// the mouse itself, the event has to pass through untouched — that is what
	// lets an agent handle its own clicks.
	const passthrough = "#{||:#{pane_in_mode},#{mouse_any_flag}}"

	action := "copy-mode -H ; send-keys -X " + selector +
		" ; run-shell -d 0.3 ; send-keys -X copy-selection-and-cancel"
	if !enabled {
		// Select the word and leave it visible, without copying. stop-selection
		// ends the drag-selection state so the pane is not left mid-selection.
		action = "copy-mode -H ; send-keys -X " + selector +
			" ; run-shell -d 0.3 ; send-keys -X stop-selection"
	}

	// The branches are plain command strings, not { } blocks.
	//
	// tmux's own default is written with blocks, but those are parsed from a
	// config file; passed as exec arguments they arrive as ordinary strings and
	// the braces end up inside the command, which tmux reports as a syntax
	// error when the key fires — seen in the status line on the first attempt.
	// The quoted-string form of if-shell takes the same three arguments and
	// binds cleanly.
	return []string{"bind-key", "-T", "root", key,
		"select-pane", "-t", "=", "\\;",
		"if-shell", "-F", passthrough, "send-keys -M", action}
}

// copyModeTables are the two key tables a mouse drag can land in.
var copyModeTables = []string{"copy-mode", "copy-mode-vi"}

// SetMouseCopyEnabled applies the copy-on-select choice to the running tmux
// server.
//
// The key tables are global, so one call reaches every open pane — the setting
// takes effect on sessions that are already running rather than only on the
// next one started.
//
// That reach cuts both ways: a binding written here outlives the app, because
// the tmux server does. Anything that stops setting these bindings has to
// restore them rather than simply leave them, or a stale one keeps deciding
// what a drag does long after the code that wrote it is gone.
func SetMouseCopyEnabled(enabled bool) {
	for _, table := range copyModeTables {
		if err := TmuxCommand(MouseCopyBinding(table, enabled)...).Run(); err != nil {
			// A failure here is why the setting silently does nothing, so it
			// has to be visible rather than dropped.
			log.Printf("[clipboard] binding %s failed: %v", table, err)
		}
		// Double and triple click are bound separately, and tmux's defaults for
		// both copy unconditionally — so leaving them alone would let a double
		// click fill the clipboard with the setting off.
		for key, selector := range clickSelectKeys {
			if err := TmuxCommand(ClickSelectBinding(table, key, selector, enabled)...).Run(); err != nil {
				log.Printf("[clipboard] binding %s/%s failed: %v", table, key, err)
			}
		}
	}

	// The root table is the one that decides for a pane not already in copy
	// mode — which is the normal case. Without these, every binding above is
	// unreachable for a click.
	for key, selector := range clickSelectKeys {
		if err := TmuxCommand(RootClickBinding(key, selector, enabled)...).Run(); err != nil {
			log.Printf("[clipboard] root binding %s failed: %v", key, err)
		}
	}
	log.Printf("[clipboard] copy-on-select=%v applied to %v", enabled, copyModeTables)
}
