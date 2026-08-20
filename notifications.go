package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"

	"asmgr-desktop/session"
)

type attentionTransitionState struct {
	projectID string
	activity  map[string]string
	baselined bool
}

func (s *attentionTransitionState) reset() {
	s.projectID = ""
	s.activity = nil
	s.baselined = false
}

func (s *attentionTransitionState) observe(projectID string, current map[string]string) []string {
	var waiting []string
	if s.baselined && s.projectID == projectID {
		for id, activity := range current {
			if activity == "waiting" && s.activity[id] != "waiting" {
				waiting = append(waiting, id)
			}
		}
	}
	s.activity = make(map[string]string, len(current))
	for id, activity := range current {
		s.activity[id] = activity
	}
	s.projectID = projectID
	s.baselined = true
	return waiting
}

// startAttentionWatcher runs the background loop that turns agent activity
// transitions into notifications. It MUST live in the backend: the frontend's
// sidebar polling pauses while the window is unfocused — exactly when the
// user most needs to be told an agent is waiting for them.
//
// Edge-triggered: a notification fires only on a transition INTO "waiting"
// (from busy/idle/unknown), never repeatedly for a session that stays
// waiting. The baseline snapshot on the first enabled tick is silent, so
// enabling the feature (or starting the app) doesn't dump a burst of
// notifications for sessions that were already waiting.
func (a *App) startAttentionWatcher(parent context.Context) {
	a.attentionMu.Lock()
	if a.attentionCancel != nil {
		a.attentionMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	a.attentionCancel = cancel
	a.attentionWG.Add(1)
	a.attentionMu.Unlock()

	go func() {
		defer a.attentionWG.Done()
		var transitions attentionTransitionState
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			// These are separate storage snapshots because sidebar detection also
			// captures tmux panes. If a project switch lands between them, discard
			// the whole tick instead of combining one project's settings/names with
			// another project's activities.
			settingsProjectID := a.storage.GetActiveProjectID()
			_, _, settings, err := a.storage.LoadAllWithSettings()
			if err != nil || a.storage.GetActiveProjectID() != settingsProjectID ||
				settings == nil || !settings.NotifyOnWaiting {
				// Feature off: drop state so re-enabling starts with a
				// fresh, silent baseline.
				transitions.reset()
				continue
			}

			upd := a.GetSidebarUpdates()
			namesProjectID, instances, _, namesErr := a.storage.LoadAllWithProjectSnapshot()
			if namesErr != nil || upd.projectID == "" ||
				upd.projectID != settingsProjectID || namesProjectID != settingsProjectID ||
				a.storage.GetActiveProjectID() != settingsProjectID {
				continue
			}

			// A project switch is a new silent baseline. Otherwise every session
			// that was already waiting in the newly selected project looks like a
			// fresh transition merely because its ID was absent from the old map.
			waiting := transitions.observe(settingsProjectID, upd.Activities)
			if len(waiting) > 0 {
				names := make(map[string]string, len(instances))
				for _, inst := range instances {
					names[inst.ID] = inst.Name
				}
				for _, id := range waiting {
					name := names[id]
					if name == "" {
						name = id
					}
					a.sendAttentionNotification(ctx, settings, name, strings.TrimSpace(upd.StatusLines[id]))
				}
			}
		}
	}()
}

// stopAttentionWatcher cancels and reaps the watcher and every notification it
// launched. Holding attentionMu across Wait prevents a concurrent/repeated
// startup from adding to the same WaitGroup while shutdown is waiting on it.
func (a *App) stopAttentionWatcher() {
	a.attentionMu.Lock()
	defer a.attentionMu.Unlock()
	if a.attentionCancel == nil {
		return
	}
	a.attentionCancel()
	a.attentionWG.Wait()
	a.attentionCancel = nil
}

// sendAttentionNotification delivers one "agent is waiting" notification via
// the channels enabled in settings. Best-effort: failures are logged, never
// surfaced as errors.
func (a *App) sendAttentionNotification(ctx context.Context, settings *session.Settings, name, statusLine string) {
	body := statusLine
	if body == "" {
		if settings.Language == "hu" {
			body = "Beavatkozásra vár"
		} else {
			body = "Waiting for your input"
		}
	}
	title := fmt.Sprintf("⏳ %s", name)

	if settings.NotifyDesktop {
		a.attentionWG.Add(1)
		go func() {
			defer a.attentionWG.Done()
			var cmd *exec.Cmd
			switch goruntime.GOOS {
			case "linux":
				cmd = session.CommandContext(ctx, "notify-send", "-a", "ASMGR Desktop", "-u", "normal", title, body)
			case "darwin":
				script := fmt.Sprintf("display notification %q with title %q", body, title)
				cmd = session.CommandContext(ctx, "osascript", "-e", script)
			default:
				return
			}
			if err := cmd.Run(); err != nil && ctx.Err() == nil {
				log.Printf("[notify] desktop notification failed: %v", err)
			}
		}()
	}

	if settings.NotifyNtfy && settings.NtfyURL != "" {
		url := settings.NtfyURL
		a.attentionWG.Add(1)
		go func() {
			defer a.attentionWG.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			// Title goes into the body too: ntfy header values are ASCII-only
			// territory, and session names can carry accents/emoji.
			msg := fmt.Sprintf("%s – %s", title, body)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(msg))
			if err != nil {
				log.Printf("[notify] ntfy request build failed: %v", err)
				return
			}
			req.Header.Set("Tags", "hourglass_flowing_sand")
			resp, err := client.Do(req)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("[notify] ntfy push failed: %v", err)
				return
			}
			defer resp.Body.Close()

			// A refused push answers with a status, not an error: a topic typo
			// gives 404 and a protected one 403, both with err == nil. Without
			// this the only symptom is notifications that never arrive, with
			// nothing anywhere to say why — and the log is the one place a user
			// can be pointed at.
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				detail, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
				log.Printf("[notify] ntfy refused the push: %s %s: %s",
					resp.Status, url, strings.TrimSpace(string(detail)))
			}
		}()
	}
}
