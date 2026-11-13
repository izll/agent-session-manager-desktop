package session

import (
	"context"
	"fmt"
	"strings"
)

// Copy mode is tmux's scrollback: entered by scrolling up or selecting with the mouse.
// While a pane is in it, tmux shows a still picture of it, so a busy agent looks frozen
// until the mode ends — and the app hides tmux's own indicator of it. Nothing on screen
// says why, which is why the app reads it and says so itself.

// PaneScroll is whether a tab's pane is in copy mode, and scrolled back from the end.
type PaneScroll struct {
	InMode   bool `json:"inMode"`
	Scrolled bool `json:"scrolled"`
}

// PaneScrollContext reads it for one tab. A tab that is not there is not in copy mode.
func (i *Instance) PaneScrollContext(ctx context.Context, windowIdx int) (PaneScroll, error) {
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	output, err := i.execOn(i.serverForWindow(windowIdx)).Output(commandCtx, "display-message", "-p",
		"-t", i.copyModeTarget(windowIdx), "#{window_index}\t#{pane_in_mode}\t#{pane_mode}\t#{scroll_position}")
	if err != nil {
		return PaneScroll{}, err
	}
	fields := strings.Split(strings.TrimSpace(string(output)), "\t")
	// A window that is gone is answered for by another: only its own index counts.
	if len(fields) < 4 || fields[0] != fmt.Sprint(windowIdx) || fields[1] != "1" {
		return PaneScroll{}, nil
	}
	// Only the scrollback: tmux's other modes (a chooser, a clock) do not freeze output.
	// An older tmux that does not name the mode leaves it empty: copy mode is its likely one.
	if mode := fields[2]; mode != "" && mode != "copy-mode" && mode != "view-mode" {
		return PaneScroll{}, nil
	}
	return PaneScroll{InMode: true, Scrolled: fields[3] != "" && fields[3] != "0"}, nil
}

// SettleCopyModeContext ends copy mode where it holds nothing: at the bottom there is
// no place to lose, only output the user cannot see. Scrolled back, it stays.
func (i *Instance) SettleCopyModeContext(ctx context.Context, windowIdx int) (PaneScroll, error) {
	state, err := i.PaneScrollContext(ctx, windowIdx)
	if err != nil || !state.InMode || state.Scrolled {
		return state, err
	}
	return i.LeaveCopyModeContext(ctx, windowIdx)
}

// LeaveCopyModeContext ends copy mode, as q would, and answers how the pane stands after.
func (i *Instance) LeaveCopyModeContext(ctx context.Context, windowIdx int) (PaneScroll, error) {
	state, err := i.PaneScrollContext(ctx, windowIdx)
	if err != nil || !state.InMode {
		return state, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	if err := i.execOn(i.serverForWindow(windowIdx)).Run(commandCtx, "send-keys", "-X", "-t", i.copyModeTarget(windowIdx), "cancel"); err != nil {
		return state, err
	}
	return i.PaneScrollContext(ctx, windowIdx)
}

func (i *Instance) copyModeTarget(windowIdx int) string {
	return fmt.Sprintf("%s:%d", i.TmuxSessionName(), windowIdx)
}
