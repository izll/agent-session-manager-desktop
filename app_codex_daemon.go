package main

import (
	"fmt"

	"asmgr-desktop/session"
)

// codexDaemonHeldEvent tells the frontend a Codex conversation was continued
// through the background server, which ignores YOLO.
const codexDaemonHeldEvent = "codex:daemonHeld"

// StopCodexDaemon stops Codex's background server on the machine a tab runs
// on: serverID names the server, "" is this computer. The session supplies the
// connection for a server; this computer needs none.
//
// Nothing is restarted. A tab continued through the server keeps running there
// until it is restarted, and a restart then opens the conversation without the
// server, with YOLO in effect.
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
