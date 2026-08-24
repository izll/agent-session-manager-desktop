package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

func TestAttentionWatcherHasSingleCancellableLifecycle(t *testing.T) {
	a := NewApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a.startAttentionWatcher(ctx)
	firstCancel := a.attentionCancel
	if firstCancel == nil {
		t.Fatal("attention watcher did not register its cancel function")
	}
	a.startAttentionWatcher(ctx)
	if a.attentionCancel == nil {
		t.Fatal("second start cleared the active watcher")
	}

	stopped := make(chan struct{})
	go func() {
		a.stopAttentionWatcher()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("attention watcher did not stop promptly after cancellation")
	}
	if a.attentionCancel != nil {
		t.Fatal("stopped attention watcher still looks active")
	}

	// Repeated shutdown is a no-op, not a second close of the same channel.
	a.stopAttentionWatcher()
}

func TestNtfyLogEndpointDoesNotExposeTopicOrCredentials(t *testing.T) {
	raw := "https://alice:secret@notify.example.test/private-topic?token=top-secret#fragment"
	got := notificationEndpointForLog(raw)
	if got != "https://notify.example.test" {
		t.Fatalf("redacted endpoint = %q", got)
	}
	for _, secret := range []string{"alice", "secret", "private-topic", "token", "top-secret", "fragment"} {
		if strings.Contains(got, secret) {
			t.Errorf("redacted endpoint leaked %q: %q", secret, got)
		}
	}
	if got := notificationEndpointForLog("://private-topic?token=secret"); got != "<invalid endpoint>" {
		t.Fatalf("invalid URL was logged as %q", got)
	}
}

func TestDesktopNotificationUsesThePlatformDelivery(t *testing.T) {
	original := desktopNotificationDeliver
	t.Cleanup(func() { desktopNotificationDeliver = original })
	delivered := make(chan [2]string, 1)
	desktopNotificationDeliver = func(_ context.Context, title, body string) error {
		delivered <- [2]string{title, body}
		return nil
	}

	a := NewApp()
	a.sendAttentionNotification(context.Background(), &session.Settings{NotifyDesktop: true}, "reviewer", "ready")
	a.attentionWG.Wait()
	select {
	case got := <-delivered:
		if got != [2]string{"⏳ reviewer", "ready"} {
			t.Fatalf("desktop notification = %#v", got)
		}
	default:
		t.Fatal("desktop notification did not reach the platform delivery implementation")
	}
}

func TestDesktopNotificationHasIndependentDeliveryTimeout(t *testing.T) {
	originalDeliver := desktopNotificationDeliver
	originalTimeout := desktopNotificationTimeout
	t.Cleanup(func() {
		desktopNotificationDeliver = originalDeliver
		desktopNotificationTimeout = originalTimeout
	})
	desktopNotificationTimeout = 30 * time.Millisecond
	desktopNotificationDeliver = func(ctx context.Context, _, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	}

	a := NewApp()
	started := time.Now()
	a.sendAttentionNotification(context.Background(), &session.Settings{NotifyDesktop: true}, "reviewer", "ready")
	a.attentionWG.Wait()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("wedged desktop notification retained its worker for %v", elapsed)
	}
}

func TestNativeNotificationLifecycleSurroundsTheWatcher(t *testing.T) {
	source := readGoSource(t, "app.go")
	startup := strings.Index(source, "func (a *App) startup(ctx context.Context)")
	shutdown := strings.Index(source, "func (a *App) shutdown(ctx context.Context)")
	if startup < 0 || shutdown < 0 || startup >= shutdown {
		t.Fatal("app lifecycle methods not found")
	}
	startupBody := source[startup:shutdown]
	initAt := strings.Index(startupBody, "desktopNotificationInitialize(ctx)")
	watchAt := strings.Index(startupBody, "a.startAttentionWatcher(ctx)")
	if initAt < 0 || watchAt < 0 || initAt > watchAt {
		t.Fatalf("native notifications must initialize before the watcher: init=%d watcher=%d", initAt, watchAt)
	}

	shutdownBody := source[shutdown:]
	stopAt := strings.Index(shutdownBody, "a.stopAttentionWatcher()")
	cleanupAt := strings.Index(shutdownBody, "desktopNotificationCleanup(ctx)")
	if stopAt < 0 || cleanupAt < 0 || stopAt > cleanupAt {
		t.Fatalf("watcher must stop before notification cleanup: stop=%d cleanup=%d", stopAt, cleanupAt)
	}
}

func TestAttentionTransitionsSilentlyRebaselineOnProjectSwitch(t *testing.T) {
	var state attentionTransitionState
	if got := state.observe("project-a", map[string]string{"a": "waiting"}); len(got) != 0 {
		t.Fatalf("initial baseline notified: %v", got)
	}
	if got := state.observe("project-a", map[string]string{"a": "busy"}); len(got) != 0 {
		t.Fatalf("non-waiting transition notified: %v", got)
	}
	if got := state.observe("project-a", map[string]string{"a": "waiting"}); len(got) != 1 || got[0] != "a" {
		t.Fatalf("waiting transition = %v, want [a]", got)
	}

	// Project B is already waiting when selected. That is baseline state, not a
	// new agent transition, so switching projects must not send a burst.
	if got := state.observe("project-b", map[string]string{"b": "waiting"}); len(got) != 0 {
		t.Fatalf("project switch notified existing waiting sessions: %v", got)
	}
}

func TestAttentionWatcherAcceptsDefaultProjectSidebarSnapshot(t *testing.T) {
	if !sidebarUpdateMatchesProject(SidebarUpdate{ProjectID: ""}, "") {
		t.Fatal("default project sidebar snapshot was treated as missing")
	}
	if sidebarUpdateMatchesProject(SidebarUpdate{ProjectID: "project-a"}, "project-b") {
		t.Fatal("foreign project sidebar snapshot was accepted")
	}
}
