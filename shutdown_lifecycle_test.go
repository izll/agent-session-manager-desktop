package main

import (
	"asmgr-desktop/session"
	"context"
	"strings"
	"testing"
	"time"
)

// The preview poller reads project storage and captures tmux panes. It must be
// gone before shutdown releases the project lock or removes GUI mirrors; a
// later cancel leaves a real window in which another tick can enter teardown.
func TestShutdownStopsPreviewPollingBeforeProjectTeardown(t *testing.T) {
	text := readGoSource(t, "app.go")
	start := strings.Index(text, "func (a *App) shutdown(ctx context.Context)")
	if start < 0 {
		t.Fatal("shutdown method not found")
	}
	text = text[start:]
	stop := strings.Index(text, "a.stopPreviewPolling()")
	tmuxStop := strings.Index(text, "a.stopTmuxMaintenance()")
	dictationStop := strings.Index(text, "a.dictation.Shutdown()")
	terminalStop := strings.Index(text, "a.termServer.Stop(stopCtx)")
	taskMasterStop := strings.Index(text, "stopAllTaskMasters()")
	teardown := strings.Index(text, "a.projectMu.Lock()")
	if stop < 0 || tmuxStop < 0 || dictationStop < 0 || terminalStop < 0 || taskMasterStop < 0 || teardown < 0 {
		t.Fatalf("shutdown lifecycle calls missing: preview=%d tmux=%d dictation=%d terminal=%d taskmaster=%d teardown=%d", stop, tmuxStop, dictationStop, terminalStop, taskMasterStop, teardown)
	}
	if stop > teardown {
		t.Fatal("preview polling is stopped after project teardown begins")
	}
	if terminalStop > teardown {
		t.Fatal("terminal listener and connections are stopped after project teardown begins")
	}
	if tmuxStop > teardown {
		t.Fatal("asynchronous tmux maintenance is stopped after project teardown begins")
	}
	if dictationStop > terminalStop {
		t.Fatal("dictation callbacks can outlive the terminal server they write to")
	}
	if taskMasterStop > teardown {
		t.Fatal("Task Master startup is cancelled only after project teardown starts")
	}
}

func TestDictationShutdownDetachesRuntimeBindings(t *testing.T) {
	service := NewDictationService()
	service.SetTerminalServer(&TerminalServer{})
	service.SetActiveTmuxSession("session", 3)
	service.SetStateChangeCallback(func(bool) {})
	service.SetErrorCallback(func(string, string) {})
	service.SetVoiceLevelCallback(func(float64) {})
	service.SetInterimTextCallback(func(string) {})
	service.SetBufferTextCallback(func(string) {})
	service.SetFieldTextCallback(func(string) {})
	service.SetFieldDeleteCallback(func(int) {})
	service.setBufferTextChangeCallback(func(string) {})

	service.Shutdown()

	service.ptyHandler.mu.Lock()
	if service.ptyHandler.termServer != nil || service.ptyHandler.sessionID != "" {
		t.Error("shutdown retained a terminal runtime binding")
	}
	service.ptyHandler.mu.Unlock()
	if service.stateChangeCallback() != nil || service.errorCallback() != nil ||
		service.voiceLevelCallback() != nil || service.interimTextCallback() != nil ||
		service.bufferTextCallback() != nil {
		t.Error("shutdown retained a Wails callback")
	}
	service.bufferHandler.mu.Lock()
	bufferCallback := service.bufferHandler.onTextChange
	service.bufferHandler.mu.Unlock()
	service.fieldHandler.mu.RLock()
	fieldAppend := service.fieldHandler.onAppendText
	fieldDelete := service.fieldHandler.onDeleteChars
	service.fieldHandler.mu.RUnlock()
	if bufferCallback != nil || fieldAppend != nil || fieldDelete != nil {
		t.Error("shutdown retained a text handler callback")
	}
}

func TestStopPreviewPollingIsIdempotentAndWaits(t *testing.T) {
	app := NewApp()
	stopped := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	app.previewCancel = cancel
	app.previewWG.Add(1)
	go func() {
		defer app.previewWG.Done()
		<-ctx.Done()
		close(stopped)
	}()

	app.stopPreviewPolling()
	select {
	case <-stopped:
	default:
		t.Fatal("stopPreviewPolling returned before the poller stopped")
	}
	app.stopPreviewPolling()
}

func TestStopOrphanCleanupCancelsAndReapsTmuxWork(t *testing.T) {
	original := cleanupTmuxList
	t.Cleanup(func() { cleanupTmuxList = original })
	started := make(chan struct{})
	cleanupTmuxList = func(ctx context.Context) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}

	app := NewApp()
	app.projectLocked = true
	app.startOrphanCleanup(context.Background())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("orphan cleanup did not start its tmux operation")
	}

	stopped := make(chan struct{})
	go func() {
		app.stopOrphanCleanup()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("orphan cleanup did not stop after cancellation")
	}
	if app.orphanCleanupStop != nil {
		t.Fatal("stopped orphan cleanup still has a cancel function")
	}
	app.stopOrphanCleanup()
}

func TestStopAutoUpdateCheckCancelsAndWaits(t *testing.T) {
	app := NewApp()
	app.startAutoCheckForUpdate(context.Background())
	stopped := make(chan struct{})
	go func() {
		app.stopAutoCheckForUpdate()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("automatic update check did not stop after cancellation")
	}
	if app.updateCheckStop != nil {
		t.Fatal("stopped update check still has a cancel function")
	}
	app.stopAutoCheckForUpdate()
}

func TestResizeMaintenanceKeepsProjectPinnedUntilCanceled(t *testing.T) {
	original := resizeTerminalTmux
	t.Cleanup(func() { resizeTerminalTmux = original })
	started := make(chan struct{})
	resizeTerminalTmux = func(ctx context.Context, _, _ string) {
		close(started)
		<-ctx.Done()
	}

	app := NewApp()
	app.storage = guardedTestStorage(t)
	app.projectLocked = true
	app.ptys["pty"] = &ptySession{
		session:  &session.Instance{ID: "asm_codex_resize"},
		windowID: 3,
	}
	app.startTmuxMaintenance(context.Background())
	if err := app.ResizeTerminal("pty", 120, 40, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("resize maintenance did not start")
	}

	mutationAcquired := make(chan struct{})
	go func() {
		done, err := app.beginProjectMutation()
		if err == nil {
			done()
		}
		close(mutationAcquired)
	}()
	select {
	case <-mutationAcquired:
		t.Fatal("resize maintenance released project ownership before tmux work finished")
	case <-time.After(50 * time.Millisecond):
	}

	app.stopTmuxMaintenance()
	select {
	case <-mutationAcquired:
	case <-time.After(time.Second):
		t.Fatal("cancelled resize maintenance did not release project ownership")
	}
	app.stopTmuxMaintenance()
}
