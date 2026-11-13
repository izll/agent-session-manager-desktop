package main

import "asmgr-desktop/session"

// A tab's pane in tmux copy mode shows a still picture while its agent goes on
// working. The terminal asks about the tab it shows, to say so and to offer a way out.

// GetTerminalScroll reads whether the tab's pane is in copy mode.
func (a *App) GetTerminalScroll(sessionID string, windowIdx int) (session.PaneScroll, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return session.PaneScroll{}, err
	}
	return inst.PaneScrollContext(a.lifecycleContext(), windowIdx)
}

// SettleTerminalScroll is for a tab being shown: copy mode left at the bottom, where
// it keeps no place and only hides output, is ended.
func (a *App) SettleTerminalScroll(sessionID string, windowIdx int) (session.PaneScroll, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return session.PaneScroll{}, err
	}
	return inst.SettleCopyModeContext(a.lifecycleContext(), windowIdx)
}

// LeaveTerminalScroll ends copy mode on the user's word, scrolled up or not.
func (a *App) LeaveTerminalScroll(sessionID string, windowIdx int) (session.PaneScroll, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return session.PaneScroll{}, err
	}
	return inst.LeaveCopyModeContext(a.lifecycleContext(), windowIdx)
}
