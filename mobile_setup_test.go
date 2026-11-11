package main

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

func TestSetupStepFollowsTheState(t *testing.T) {
	for _, test := range []struct {
		installed bool
		backend   string
		mobileOn  bool
		serving   bool
		want      string
	}{
		{false, "", false, false, "install"},
		{true, "NeedsLogin", true, false, "login"},
		{true, "Stopped", true, false, "login"},
		{true, "NoState", true, false, "login"},
		{true, "", false, false, "login"},
		{true, "Running", false, false, "enable"},
		{true, "Running", false, true, "enable"},
		{true, "Running", true, false, "serve"},
		{true, "Running", true, true, "phone"},
	} {
		if got := setupStep(test.installed, test.backend, test.mobileOn, test.serving); got != test.want {
			t.Errorf("%+v: %q, want %q", test, got, test.want)
		}
	}
}

func TestServeStatusPointsAtOurPort(t *testing.T) {
	status := []byte(`{"TCP":{"443":{"HTTPS":true}},"Web":{"gep.tail1.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:9760"}}}}}`)
	if !serveProxiesTo(status, 9760) {
		t.Fatal("not found")
	}
	if serveProxiesTo(status, 9761) {
		t.Fatal("another port matched")
	}
	for _, other := range []string{``, `{}`, `not json`, `{"Web":{"x:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:97600"}}}}}`} {
		if serveProxiesTo([]byte(other), 9760) {
			t.Errorf("%q matched", other)
		}
	}
}

func TestSetupCommandsArePinned(t *testing.T) {
	linux := setupCommands("linux", 9760)
	if linux["install"] != "curl -fsSL https://tailscale.com/install.sh | sh" ||
		linux["login"] != "sudo tailscale up --operator=$USER" ||
		// With sudo: someone who had Tailscale before never ran our login step, and is
		// no operator, and serve is refused without it.
		linux["serve"] != "sudo tailscale serve --bg http://127.0.0.1:9760" {
		t.Fatalf("linux = %v", linux)
	}
	// The Mac's command line is in the app, on no PATH.
	mac := setupCommands("darwin", 9811)
	if mac["install"] != "" || mac["login"] != "" ||
		mac["serve"] != "/Applications/Tailscale.app/Contents/MacOS/Tailscale serve --bg http://127.0.0.1:9811" {
		t.Fatalf("darwin = %v", mac)
	}
	windows := setupCommands("windows", 9811)
	if windows["install"] != "" || windows["serve"] != "tailscale serve --bg http://127.0.0.1:9811" {
		t.Fatalf("windows = %v", windows)
	}
}

func TestSetupStatusGivesUpOnASlowTailscale(t *testing.T) {
	app := passwordTestApp(t)
	original, found := tailscaleCommand, tailscaleFound
	t.Cleanup(func() { tailscaleCommand, tailscaleFound = original, found })
	tailscaleFound = func() bool { return true }
	tailscaleCommand = func(ctx context.Context, args ...string) ([]byte, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			return nil, nil
		}
	}
	started := time.Now()
	setup := app.GetMobileSetup()
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("took %v", elapsed)
	}
	if setup.Step != "login" || setup.LoggedIn {
		t.Fatalf("setup = %+v", setup)
	}
}

func TestSetupReadsTheRealState(t *testing.T) {
	app := passwordTestApp(t)
	t.Cleanup(app.stopMobile)
	original, found := tailscaleCommand, tailscaleFound
	t.Cleanup(func() { tailscaleCommand, tailscaleFound = original, found })
	port := freePort(t)
	tailscaleFound = func() bool { return true }
	tailscaleCommand = func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "serve" {
			return []byte(`{"Web":{"gep.tail1.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:` + itoa(port) + `"}}}}}`), nil
		}
		return []byte(`{"BackendState":"Running","Self":{"DNSName":"gep.tail1.ts.net."}}`), nil
	}
	if setup := app.GetMobileSetup(); setup.Step != "enable" || !setup.LoggedIn || setup.TailnetName != "gep.tail1.ts.net" {
		t.Fatalf("before = %+v", setup)
	}
	if _, err := app.SetMobileAccess(true, port); err != nil {
		t.Fatal(err)
	}
	setup := app.GetMobileSetup()
	if setup.Step != "phone" || !setup.Serving || !strings.HasSuffix(setup.Commands["serve"], "serve --bg http://127.0.0.1:"+itoa(port)) {
		t.Fatalf("after = %+v", setup)
	}
}
