package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"asmgr-desktop/session"
)

// Without a window there is no Wails runtime, and its calls end the process.
func TestEmitIsSilentWithoutWindow(t *testing.T) {
	emitEvent(nil, "x")
	emitEvent(headlessContext(context.Background()), "x", 1)
	logStartupError(headlessContext(context.Background()), "nothing to show it in")
}

// Every event and error report goes through the one place that knows about the
// window, or headless mode stops at the first one sent.
func TestNoWailsCallOutsideTheEmitter(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	direct := regexp.MustCompile(`\b(wailsruntime|runtime)\.(Events\w+|Log\w+|\w*Notification\w*)\(`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "emit.go" {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if direct.MatchString(line) && !strings.Contains(line, "windowless-checked") {
				t.Errorf("%s:%d calls Wails directly: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
}

func TestServeOptions(t *testing.T) {
	opts, err := parseServeArgs([]string{"--port", "9811", "--pair"})
	if err != nil || opts.port != 9811 || !opts.pair {
		t.Fatalf("opts = %+v, %v", opts, err)
	}
	if opts, err := parseServeArgs(nil); err != nil || opts.port != defaultMobilePort || opts.pair {
		t.Fatalf("defaults = %+v, %v", opts, err)
	}
	for _, bad := range [][]string{{"--port"}, {"--port", "x"}, {"--port", "80"}, {"--what"}} {
		if _, err := parseServeArgs(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestServeRefusesWithoutProjectLock(t *testing.T) {
	app := passwordTestApp(t)
	app.projectLocked = false
	app.otherInstancePID = 4242
	var out bytes.Buffer
	code := serveHeadless(t.Context(), app, serveOptions{port: freePort(t)}, &out)
	if code == 0 || !strings.Contains(out.String(), "4242") {
		t.Fatalf("code = %d, out = %q", code, out.String())
	}
}

func TestServePrintsAPairingCodeAndStopsWhenAsked(t *testing.T) {
	app := passwordTestApp(t)
	port := freePort(t)
	var out syncBuffer
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	go func() { done <- serveHeadless(ctx, app, serveOptions{port: port, pair: true}, &out) }()
	deadline := time.Now().Add(5 * time.Second)
	for !reachable(port) {
		if time.Now().After(deadline) {
			t.Fatal("not serving")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for !strings.Contains(out.String(), "#pair=") {
		if time.Now().After(deadline) {
			t.Fatalf("no pairing printed: %q", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "http://127.0.0.1:"+strconv.Itoa(port)+"/#pair=") {
		t.Fatalf("out = %q", out.String())
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("code = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not stop")
	}
	if reachable(port) {
		t.Fatal("still serving after stopping")
	}
	// Serving from the command line is not a setting to keep for the desktop.
	_, _, settings, _ := app.storage.LoadAllWithSettings()
	if settings.MobileEnabled {
		t.Fatal("the desktop setting was turned on")
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The whole start-up, as --serve runs it: no window, and none of what the window
// brings with it, like dictation. In a home of its own, with a multiplexer of its own.
func TestHeadlessStartupInAFreshHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("TMUX_TMPDIR", home)
	t.Setenv("TMUX", "")
	// Start-up sets what the whole process shares; the tests after this one must find
	// it as it was.
	t.Setenv("PATH", os.Getenv("PATH"))
	route, debug, codexDaemon := session.RouteInstance, session.DebugLogging, session.CodexUseDaemon()
	t.Cleanup(func() {
		session.RouteInstance, session.DebugLogging = route, debug
		session.SetCodexDaemonHeldHandler(nil)
		session.SetCodexUseDaemon(codexDaemon)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	windowless := headlessContext(ctx)
	app := NewApp()
	app.startup(windowless)
	defer app.shutdown(windowless)
	if app.storage == nil || !app.projectLocked {
		t.Fatalf("storage %v, owns the project %v", app.storage != nil, app.projectLocked)
	}
	var out syncBuffer
	done := make(chan int, 1)
	serveCtx, stop := context.WithCancel(ctx)
	port := freePort(t)
	go func() { done <- serveHeadless(serveCtx, app, serveOptions{port: port}, &out) }()
	deadline := time.Now().Add(5 * time.Second)
	for !reachable(port) {
		if time.Now().After(deadline) {
			t.Fatalf("not serving: %q", out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	if code := <-done; code != 0 {
		t.Fatalf("code = %d", code)
	}
}
