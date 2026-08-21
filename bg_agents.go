package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sync"
	"time"

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

const (
	backgroundAgentOutputLimit = 1 << 20
	backgroundAgentLogTail     = 16 * 1024
)

var (
	backgroundAgentCommandTimeout = 15 * time.Second
	backgroundAgentCommand        = session.CommandContext
)

func runBackgroundAgentCommand(cmd *exec.Cmd) error {
	err := cmd.Run()
	// WaitDelay bounds an inherited stdout/stderr pipe after the direct Claude
	// CLI exits. The descendant still belongs to our Unix process group, so
	// explicitly cancel it before returning instead of leaking a detached helper.
	if errors.Is(err, exec.ErrWaitDelay) && cmd.Cancel != nil {
		_ = cmd.Cancel()
	}
	return err
}

// boundedCommandOutput accepts every byte so the child cannot block on a full
// pipe, but retains only a fixed amount in memory. JSON listings keep their
// prefix and reject truncation; logs keep their newest tail.
type boundedCommandOutput struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	tail      bool
	truncated bool
}

func (b *boundedCommandOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	if b.limit <= 0 {
		b.truncated = b.truncated || written > 0
		return written, nil
	}
	if b.tail {
		if len(p) >= b.limit {
			b.data = append(b.data[:0], p[len(p)-b.limit:]...)
			b.truncated = true
			return written, nil
		}
		if excess := len(b.data) + len(p) - b.limit; excess > 0 {
			copy(b.data, b.data[excess:])
			b.data = b.data[:len(b.data)-excess]
			b.truncated = true
		}
		b.data = append(b.data, p...)
		return written, nil
	}
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		keep := len(p)
		if keep > remaining {
			keep = remaining
		}
		b.data = append(b.data, p[:keep]...)
	}
	if written > remaining {
		b.truncated = true
	}
	return written, nil
}

func (b *boundedCommandOutput) Bytes() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...), b.truncated
}

// ListBackgroundAgents returns the currently live background agents.
func (a *App) ListBackgroundAgents() []BackgroundAgentInfo {
	ctx, cancel := context.WithTimeout(context.Background(), backgroundAgentCommandTimeout)
	defer cancel()
	output := &boundedCommandOutput{limit: backgroundAgentOutputLimit}
	cmd := backgroundAgentCommand(ctx, "claude", "agents", "--json")
	configureBackgroundAgentCommand(cmd)
	cmd.Stdout = output
	err := runBackgroundAgentCommand(cmd)
	out, truncated := output.Bytes()
	if err != nil {
		return nil
	}
	if truncated {
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
	ctx, cancel := context.WithTimeout(context.Background(), backgroundAgentCommandTimeout)
	defer cancel()
	output := &boundedCommandOutput{limit: backgroundAgentLogTail, tail: true}
	cmd := backgroundAgentCommand(ctx, "claude", "logs", shortID)
	configureBackgroundAgentCommand(cmd)
	cmd.Stdout = output
	cmd.Stderr = output
	err := runBackgroundAgentCommand(cmd)
	out, _ := output.Bytes()
	if err != nil && len(out) == 0 {
		return "", fmt.Errorf("claude logs failed: %w", errors.Join(err, ctx.Err()))
	}
	return string(out), nil
}

// StopBackgroundAgent stops a background agent via the official CLI.
func (a *App) StopBackgroundAgent(shortID string) error {
	if !bgAgentIDRe.MatchString(shortID) {
		return fmt.Errorf("invalid agent id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), backgroundAgentCommandTimeout)
	defer cancel()
	cmd := backgroundAgentCommand(ctx, "claude", "stop", shortID)
	configureBackgroundAgentCommand(cmd)
	return runBackgroundAgentCommand(cmd)
}

// AttachBackgroundAgent turns a background agent into a visible asmgr
// session: a custom-agent session in the agent's own working directory
// running `claude attach <id>`, optionally placed into a group. Returns the
// new session's ID so the frontend can select it.
func (a *App) AttachBackgroundAgent(shortID, cwd, name, groupID string) (string, error) {
	done, err := a.beginProjectMutation()
	if err != nil {
		return "", err
	}
	defer done()
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
		if err := a.storage.SetInstanceGroup(inst.ID, groupID); err != nil {
			log.Printf("[bg-agent] group assignment failed for %s: %v", inst.ID, err)
		}
	}
	if err := inst.Start(); err != nil {
		return inst.ID, errors.Join(err, inst.Stop(), a.storage.RemoveInstance(inst.ID))
	}
	if err := persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error { return errors.Join(inst.Stop(), a.storage.RemoveInstance(inst.ID)) },
	); err != nil {
		return inst.ID, err
	}
	return inst.ID, nil
}

// AttachBackgroundAgentAsTab attaches a background agent as a new tab
// (custom-agent window running `claude attach <id>`) inside an EXISTING
// running session — typically one detected to share the agent's working
// directory. Returns the new tab's window index.
func (a *App) AttachBackgroundAgentAsTab(sessionID, shortID, name string) (int, error) {
	done, err := a.beginProjectMutation()
	if err != nil {
		return -1, err
	}
	defer done()
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
	return idx, persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error { return inst.DeleteWindow(idx) },
	)
}
