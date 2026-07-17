package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"regexp"

	"asmgr-desktop/session"
)

// Manager surface for Claude Code background agents (`claude --bg` /
// Ctrl+B). Lists live agents, exposes their logs, stops them, or attaches
// one as a regular asmgr session so it becomes visible/interactive again.

// BackgroundAgentInfo mirrors one `claude agents --json` entry (background
// kind only — interactive entries are other terminals' live sessions).
type BackgroundAgentInfo struct {
	ID        string `json:"id"` // short job id (claude stop/attach/logs take this)
	SessionID string `json:"sessionId"`
	PID       int    `json:"pid"`
	Cwd       string `json:"cwd"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	StartedAt int64  `json:"startedAt"`
}

// bgAgentIDRe: short ids are hex-ish tokens (e.g. c8b1c191). Strict so the
// id can be safely placed on a command line.
var bgAgentIDRe = regexp.MustCompile(`^[0-9a-f]{6,16}$`)

// ListBackgroundAgents returns the currently live background agents.
func (a *App) ListBackgroundAgents() []BackgroundAgentInfo {
	out, err := exec.Command("claude", "agents", "--json").Output()
	if err != nil {
		return nil
	}
	var raw []struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionId"`
		PID       int    `json:"pid"`
		Cwd       string `json:"cwd"`
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		StartedAt int64  `json:"startedAt"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}
	var agents []BackgroundAgentInfo
	for _, r := range raw {
		if r.Kind != "background" {
			continue
		}
		id := r.ID
		if id == "" && len(r.SessionID) >= 8 {
			id = r.SessionID[:8]
		}
		agents = append(agents, BackgroundAgentInfo{
			ID: id, SessionID: r.SessionID, PID: r.PID, Cwd: r.Cwd,
			Name: r.Name, Status: r.Status, StartedAt: r.StartedAt,
		})
	}
	return agents
}

// GetBackgroundAgentLogs returns the agent's recent output (`claude logs`),
// capped so a chatty agent can't flood the webview.
func (a *App) GetBackgroundAgentLogs(shortID string) (string, error) {
	if !bgAgentIDRe.MatchString(shortID) {
		return "", fmt.Errorf("invalid agent id")
	}
	out, err := exec.Command("claude", "logs", shortID).CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", fmt.Errorf("claude logs failed: %w", err)
	}
	const cap = 16 * 1024
	if len(out) > cap {
		out = out[len(out)-cap:]
	}
	return string(out), nil
}

// StopBackgroundAgent stops a background agent via the official CLI.
func (a *App) StopBackgroundAgent(shortID string) error {
	if !bgAgentIDRe.MatchString(shortID) {
		return fmt.Errorf("invalid agent id")
	}
	return exec.Command("claude", "stop", shortID).Run()
}

// AttachBackgroundAgent turns a background agent into a visible asmgr
// session: a custom-agent session in the agent's own working directory
// running `claude attach <id>`, optionally placed into a group. Returns the
// new session's ID so the frontend can select it.
func (a *App) AttachBackgroundAgent(shortID, cwd, name, groupID string) (string, error) {
	if !bgAgentIDRe.MatchString(shortID) {
		return "", fmt.Errorf("invalid agent id")
	}
	if name == "" {
		name = "bg " + shortID
	}
	inst, err := session.NewInstance(name, cwd, false, session.AgentCustom, "")
	if err != nil {
		return "", err
	}
	inst.CustomCommand = "claude attach " + shortID
	if err := a.storage.AddInstance(inst); err != nil {
		return "", err
	}
	if groupID != "" {
		if err := a.AssignToGroup(inst.ID, groupID); err != nil {
			log.Printf("[bg-agent] group assignment failed for %s: %v", inst.ID, err)
		}
	}
	if err := inst.Start(); err != nil {
		return inst.ID, err
	}
	if err := a.storage.UpdateInstance(inst); err != nil {
		return inst.ID, err
	}
	return inst.ID, nil
}

// AttachBackgroundAgentAsTab attaches a background agent as a new tab
// (custom-agent window running `claude attach <id>`) inside an EXISTING
// running session — typically one detected to share the agent's working
// directory. Returns the new tab's window index.
func (a *App) AttachBackgroundAgentAsTab(sessionID, shortID, name string) (int, error) {
	if !bgAgentIDRe.MatchString(shortID) {
		return -1, fmt.Errorf("invalid agent id")
	}
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return -1, err
	}
	if name == "" {
		name = "bg " + shortID
	}
	idx, err := inst.NewAgentWindow(name, session.AgentCustom, "claude attach "+shortID, "", "")
	if err != nil {
		return -1, err
	}
	return idx, a.storage.UpdateInstance(inst)
}
