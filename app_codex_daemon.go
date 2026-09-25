package main

import (
	"fmt"

	"asmgr-desktop/session"
)

// codexDaemonHeldEvent tells the frontend a Codex conversation was continued
// through the background server, which ignores YOLO.
const codexDaemonHeldEvent = "codex:daemonHeld"

// CodexDaemonHeldEvent is the notice as the frontend receives it: with the
// project it belongs to, since session ids are only unique inside a project and
// the notice offers to restart the tab it names.
type CodexDaemonHeldEvent struct {
	session.CodexDaemonHeldNotice
	ProjectID string `json:"projectId"`
}

// codexDaemonHeldPayload pins a notice to the project that is active as it is
// sent — the one whose session was just started.
func (a *App) codexDaemonHeldPayload(notice session.CodexDaemonHeldNotice) CodexDaemonHeldEvent {
	event := CodexDaemonHeldEvent{CodexDaemonHeldNotice: notice}
	if a.storage != nil {
		event.ProjectID = a.storage.GetActiveProjectID()
	}
	return event
}

// StopCodexDaemon stops Codex's background server on the machine a tab runs
// on: serverID names the server, "" is this computer. The session supplies the
// connection for a server; this computer needs none.
//
// Nothing is restarted here. A tab continued through the server keeps running
// there until it is restarted — the notice offers that next, through RestartTab
// — and a restart then opens the conversation without the server, with YOLO in
// effect.
func (a *App) StopCodexDaemon(sessionID, serverID string) error {
	inst := &session.Instance{}
	if serverID != "" {
		found, err := a.storage.GetInstance(sessionID)
		if err != nil {
			return fmt.Errorf("session not found: %w", err)
		}
		inst = found
	}
	return inst.StopCodexDaemon(serverID)
}
