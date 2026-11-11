package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"asmgr-desktop/session"
)

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func reachable(port int) bool {
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func TestMobileAccessStartsAndStops(t *testing.T) {
	app := passwordTestApp(t)
	t.Cleanup(app.stopMobile)
	port := freePort(t)
	status, err := app.SetMobileAccess(true, port)
	if err != nil || !status.Running || status.Port != port {
		t.Fatalf("status = %+v, %v", status, err)
	}
	if !reachable(port) {
		t.Fatal("not serving")
	}
	if status, err = app.SetMobileAccess(false, port); err != nil || status.Running {
		t.Fatalf("status = %+v, %v", status, err)
	}
	if reachable(port) {
		t.Fatal("still serving")
	}
}

func TestMobileAccessNeedsTheProjectLock(t *testing.T) {
	app := passwordTestApp(t)
	app.projectLocked = false
	port := freePort(t)
	if _, err := app.SetMobileAccess(true, port); err == nil {
		t.Fatal("started without owning the project")
	}
	if reachable(port) || app.GetMobileStatus().Running {
		t.Fatal("serving without owning the project")
	}
}

func TestMobileSettingSurvivesRestart(t *testing.T) {
	app := passwordTestApp(t)
	port := freePort(t)
	if _, err := app.SetMobileAccess(true, port); err != nil {
		t.Fatal(err)
	}
	app.stopMobile()
	restarted := &App{storage: app.storage, projectLocked: true, servers: newServerPool()}
	t.Cleanup(restarted.stopMobile)
	restarted.startMobileIfEnabled()
	if !reachable(port) {
		t.Fatal("not serving after a restart")
	}
}

func TestBusyPortTurnsTheSwitchBackOff(t *testing.T) {
	app := passwordTestApp(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	status, err := app.SetMobileAccess(true, port)
	if err == nil || status.Running {
		t.Fatalf("status = %+v, %v", status, err)
	}
	_, _, settings, _ := app.storage.LoadAllWithSettings()
	if settings.MobileEnabled {
		t.Fatal("the setting stayed on")
	}
}

func TestPortOutOfRangeIsRefused(t *testing.T) {
	app := passwordTestApp(t)
	for _, port := range []int{80, 1023, 65536} {
		if _, err := app.SetMobileAccess(true, port); err == nil {
			t.Errorf("port %d accepted", port)
		}
	}
}

func TestPairingQRShowsTheURL(t *testing.T) {
	app := passwordTestApp(t)
	t.Cleanup(app.stopMobile)
	port := freePort(t)
	if _, err := app.SetMobileAccess(true, port); err != nil {
		t.Fatal(err)
	}
	pairing, err := app.CreateMobilePairing()
	if err != nil {
		t.Fatal(err)
	}
	if pairing.Code == "" || !strings.HasPrefix(pairing.URL, "http://127.0.0.1:"+strconv.Itoa(port)+"/#pair=") ||
		!strings.HasSuffix(pairing.URL, pairing.Code) {
		t.Fatalf("pairing = %+v", pairing)
	}
	data, err := base64.StdEncoding.DecodeString(pairing.QRPNG)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() < 256 {
		t.Fatalf("qr is %d px", img.Bounds().Dx())
	}
}

func TestPairingNeedsTheServer(t *testing.T) {
	app := passwordTestApp(t)
	if _, err := app.CreateMobilePairing(); err == nil {
		t.Fatal("a pairing with nothing to pair with")
	}
}

func TestDevicesCanBeListedAndRevoked(t *testing.T) {
	app := passwordTestApp(t)
	t.Cleanup(app.stopMobile)
	port := freePort(t)
	if _, err := app.SetMobileAccess(true, port); err != nil {
		t.Fatal(err)
	}
	pairing, _ := app.CreateMobilePairing()
	body := strings.NewReader(`{"code":"` + pairing.Code + `","name":"Telefon"}`)
	resp, err := http.Post("http://127.0.0.1:"+strconv.Itoa(port)+"/api/pair", "application/json", body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("pair: %v %v", resp, err)
	}
	resp.Body.Close()
	devices := app.ListMobileDevices()
	if len(devices) != 1 || devices[0].Name != "Telefon" {
		t.Fatalf("devices = %+v", devices)
	}
	if err := app.RevokeMobileDevice(devices[0].ID); err != nil || len(app.ListMobileDevices()) != 0 {
		t.Fatalf("revoke: %v, %+v", err, app.ListMobileDevices())
	}
}

// Moving to a port that is taken must not leave the old server serving behind a
// switch that says off.
func TestABusyNewPortKeepsTheOldOneServing(t *testing.T) {
	app := passwordTestApp(t)
	t.Cleanup(app.stopMobile)
	old := freePort(t)
	if _, err := app.SetMobileAccess(true, old); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	busy := listener.Addr().(*net.TCPAddr).Port
	status, err := app.SetMobileAccess(true, busy)
	if err == nil {
		t.Fatal("no error")
	}
	if !status.Running || status.Port != old || !reachable(old) {
		t.Fatalf("status = %+v, serving old %v", status, reachable(old))
	}
	_, _, settings, _ := app.storage.LoadAllWithSettings()
	if !settings.MobileEnabled || settings.MobilePort != old {
		t.Fatalf("setting = %v %d", settings.MobileEnabled, settings.MobilePort)
	}
}

// A window that does not own the project changes nothing, not even the setting the
// owner reads at its next start.
func TestANonOwnerWindowChangesNothing(t *testing.T) {
	app := passwordTestApp(t)
	if err := app.storage.UpdateSettings(func(settings *session.Settings) {
		settings.MobileEnabled, settings.MobilePort = true, 9760
	}); err != nil {
		t.Fatal(err)
	}
	app.projectLocked = false
	app.otherInstancePID = 4242
	if _, err := app.SetMobileAccess(false, 9760); err == nil || !strings.Contains(err.Error(), "4242") {
		t.Fatalf("err = %v", err)
	}
	_, _, settings, _ := app.storage.LoadAllWithSettings()
	if !settings.MobileEnabled {
		t.Fatal("the owner's setting was turned off")
	}
}

// Switching to a project another window owns stops serving; back, it serves again.
func TestServingFollowsTheProjectOwnership(t *testing.T) {
	app := passwordTestApp(t)
	t.Cleanup(app.stopMobile)
	port := freePort(t)
	if _, err := app.SetMobileAccess(true, port); err != nil {
		t.Fatal(err)
	}
	app.projectLocked = false
	app.reconcileMobileWithProject()
	if reachable(port) || app.GetMobileStatus().Running {
		t.Fatal("still serving a project it no longer owns")
	}
	app.projectLocked = true
	app.reconcileMobileWithProject()
	if !reachable(port) {
		t.Fatal("not serving again")
	}
}

// Moving to another port moves the server: the old one does not go on serving.
func TestAPortChangeStopsTheOldServer(t *testing.T) {
	app := passwordTestApp(t)
	t.Cleanup(app.stopMobile)
	old, next := freePort(t), freePort(t)
	if _, err := app.SetMobileAccess(true, old); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetMobileAccess(true, next); err != nil {
		t.Fatal(err)
	}
	if reachable(old) || !reachable(next) {
		t.Fatalf("old %v, new %v", reachable(old), reachable(next))
	}
}

// Through the real project switch: another project, with its own setting off.
func TestSwitchingProjectAppliesItsMobileSetting(t *testing.T) {
	app := passwordTestApp(t)
	t.Cleanup(app.stopMobile)
	port := freePort(t)
	if _, err := app.SetMobileAccess(true, port); err != nil {
		t.Fatal(err)
	}
	other, err := app.storage.AddProject("other")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SelectProject(other.ID); err != nil {
		t.Fatal(err)
	}
	if reachable(port) {
		t.Fatal("still serving after switching to a project where it is off")
	}
}

// Through Tailscale the phone reaches the machine by its tailnet name, and that is
// what the QR code has to carry.
func TestTheTailnetNameIsReadFromTailscale(t *testing.T) {
	status := []byte(`{"BackendState":"Running","Self":{"DNSName":"gep.tail1234.ts.net.","HostName":"gep"}}`)
	if got := tailnetNameFrom(status); got != "gep.tail1234.ts.net" {
		t.Fatalf("name = %q", got)
	}
	for _, other := range []string{
		`{"BackendState":"Stopped","Self":{"DNSName":"gep.tail1234.ts.net."}}`,
		`{"BackendState":"Running","Self":{"DNSName":""}}`,
		`{"BackendState":"Running","Self":{"DNSName":"evil.example.com."}}`,
		`{"BackendState":"Running","Self":{"DNSName":"x.com?.ts.net."}}`,
		`{"BackendState":"Running","Self":{"DNSName":"x.com#.ts.net."}}`,
		`{"BackendState":"Running","Self":{"DNSName":"x\\y.tail.ts.net."}}`,
		`{"BackendState":"Running","Self":{"DNSName":"gep.ts.net."}}`,
		`not json`,
	} {
		if got := tailnetNameFrom([]byte(other)); got != "" {
			t.Errorf("%s: %q", other, got)
		}
	}
}

func TestPairingPrefersTheTailnetAddress(t *testing.T) {
	app := passwordTestApp(t)
	t.Cleanup(app.stopMobile)
	originalStatus, originalCommand := tailscaleStatus, tailscaleCommand
	t.Cleanup(func() { tailscaleStatus, tailscaleCommand = originalStatus, originalCommand })
	port := freePort(t)
	serving := true
	tailscaleStatus = func() ([]byte, error) {
		return []byte(`{"BackendState":"Running","Self":{"DNSName":"gep.tail1234.ts.net."}}`), nil
	}
	tailscaleCommand = func(_ context.Context, args ...string) ([]byte, error) {
		if !serving {
			return []byte(`{}`), nil
		}
		return []byte(`{"Web":{"gep.tail1234.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:` + strconv.Itoa(port) + `"}}}}}`), nil
	}
	if _, err := app.SetMobileAccess(true, port); err != nil {
		t.Fatal(err)
	}
	pairing, err := app.CreateMobilePairing()
	if err != nil {
		t.Fatal(err)
	}
	if pairing.URL != "https://gep.tail1234.ts.net/#pair="+pairing.Code {
		t.Fatalf("url = %q", pairing.URL)
	}
	if pairing.LocalURL != "http://127.0.0.1:"+strconv.Itoa(port)+"/#pair="+pairing.Code {
		t.Fatalf("local = %q", pairing.LocalURL)
	}
	// Up, but not passing the tailnet on to this server: that address serves nothing,
	// or something else, which would get the code.
	serving = false
	if pairing, _ = app.CreateMobilePairing(); pairing.URL != pairing.LocalURL {
		t.Fatalf("not served: %q", pairing.URL)
	}
	tailscaleStatus = func() ([]byte, error) { return nil, errors.New("not installed") }
	if pairing, _ = app.CreateMobilePairing(); pairing.URL != pairing.LocalURL {
		t.Fatalf("without tailscale: %q", pairing.URL)
	}
}
