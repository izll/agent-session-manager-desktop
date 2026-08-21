package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"asmgr-desktop/session"
)

var (
	desktopNotificationInitialize = platformInitializeDesktopNotifications
	desktopNotificationCleanup    = platformCleanupDesktopNotifications
	desktopNotificationDeliver    = platformDeliverDesktopNotification
	desktopNotificationTimeout    = 5 * time.Second
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
			_, _, settings, err := a.storage.LoadAllWithSettingsContext(ctx)
			if err != nil || a.storage.GetActiveProjectID() != settingsProjectID ||
				settings == nil || !settings.NotifyOnWaiting {
				// Feature off: drop state so re-enabling starts with a
				// fresh, silent baseline.
				transitions.reset()
				continue
			}

			upd := a.getSidebarUpdates(ctx)
			namesProjectID, instances, _, namesErr := a.storage.LoadAllWithProjectSnapshotContext(ctx)
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
			// notify-send/osascript and the Windows notification bridge are
			// external facilities. A wedged desktop bus must not retain one child
			// and goroutine for every later waiting transition until app shutdown.
			deliveryCtx, cancel := context.WithTimeout(ctx, desktopNotificationTimeout)
			defer cancel()
			if err := desktopNotificationDeliver(deliveryCtx, title, body); err != nil && ctx.Err() == nil {
				log.Printf("[notify] desktop notification failed: %v", err)
			}
		}()
	}

	if settings.NotifyNtfy && settings.NtfyURL != "" {
		url := settings.NtfyURL
		logEndpoint := notificationEndpointForLog(url)
		a.attentionWG.Add(1)
		go func() {
			defer a.attentionWG.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			// Title goes into the body too: ntfy header values are ASCII-only
			// territory, and session names can carry accents/emoji.
			msg := fmt.Sprintf("%s – %s", title, body)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(msg))
			if err != nil {
				// Topic paths, query parameters and URL credentials are often the
				// only secret protecting a push topic. Never copy the raw URL (or a
				// url.Error that embeds it) into the user-visible application log.
				log.Printf("[notify] ntfy request for %s is invalid", logEndpoint)
				return
			}
			req.Header.Set("Tags", "hourglass_flowing_sand")
			resp, err := client.Do(req)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("[notify] ntfy push to %s failed", logEndpoint)
				return
			}
			defer resp.Body.Close()

			// A refused push answers with a status, not an error: a topic typo
			// gives 404 and a protected one 403, both with err == nil. Without
			// this the only symptom is notifications that never arrive, with
			// nothing anywhere to say why — and the log is the one place a user
			// can be pointed at.
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				// The response body is remote-controlled and may echo a protected
				// topic or credential. The status and redacted origin are enough to
				// diagnose authorization/not-found/server failures without leaking it.
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
				log.Printf("[notify] ntfy at %s refused the push: %s",
					logEndpoint, resp.Status)
			}
		}()
	}
}

func notificationEndpointForLog(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<invalid endpoint>"
	}
	// URL.Host deliberately excludes User, Path, RawQuery and Fragment.
	return parsed.Scheme + "://" + parsed.Host
}
