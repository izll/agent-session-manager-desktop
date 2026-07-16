package main

import (
	"fmt"
	"log"
	"net/http"
	"os/exec"
	goruntime "runtime"
	"strings"
	"time"

	"asmgr-desktop/session"
)

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
func (a *App) startAttentionWatcher() {
	go func() {
		last := make(map[string]string)
		baselined := false
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			_, _, settings, err := a.storage.LoadAllWithSettings()
			if err != nil || settings == nil || !settings.NotifyOnWaiting {
				// Feature off: drop state so re-enabling starts with a
				// fresh, silent baseline.
				if baselined {
					last = make(map[string]string)
					baselined = false
				}
				continue
			}

			upd := a.GetSidebarUpdates()

			if baselined {
				instances, _, _ := a.storage.LoadAll()
				names := make(map[string]string, len(instances))
				for _, inst := range instances {
					names[inst.ID] = inst.Name
				}
				for id, act := range upd.Activities {
					if act == "waiting" && last[id] != "waiting" {
						name := names[id]
						if name == "" {
							name = id
						}
						a.sendAttentionNotification(settings, name, strings.TrimSpace(upd.StatusLines[id]))
					}
				}
			}

			// Refresh state (and drop sessions that disappeared).
			last = make(map[string]string, len(upd.Activities))
			for id, act := range upd.Activities {
				last[id] = act
			}
			baselined = true
		}
	}()
}

// sendAttentionNotification delivers one "agent is waiting" notification via
// the channels enabled in settings. Best-effort: failures are logged, never
// surfaced as errors.
func (a *App) sendAttentionNotification(settings *session.Settings, name, statusLine string) {
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
		go func() {
			var cmd *exec.Cmd
			switch goruntime.GOOS {
			case "linux":
				cmd = exec.Command("notify-send", "-a", "ASMGR Desktop", "-u", "normal", title, body)
			case "darwin":
				script := fmt.Sprintf("display notification %q with title %q", body, title)
				cmd = exec.Command("osascript", "-e", script)
			default:
				return
			}
			if err := cmd.Run(); err != nil {
				log.Printf("[notify] desktop notification failed: %v", err)
			}
		}()
	}

	if settings.NotifyNtfy && settings.NtfyURL != "" {
		url := settings.NtfyURL
		go func() {
			client := &http.Client{Timeout: 5 * time.Second}
			// Title goes into the body too: ntfy header values are ASCII-only
			// territory, and session names can carry accents/emoji.
			msg := fmt.Sprintf("%s – %s", title, body)
			req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(msg))
			if err != nil {
				log.Printf("[notify] ntfy request build failed: %v", err)
				return
			}
			req.Header.Set("Tags", "hourglass_flowing_sand")
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("[notify] ntfy push failed: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
}
