package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"asmgr-desktop/mobile"
	"asmgr-desktop/session"

	"github.com/skip2/go-qrcode"
)

// The phone's way in, as the desktop turns it on and off. See the mobile package.

const defaultMobilePort = 9760

// MobileStatus says whether the phone's page is served, and where.
type MobileStatus struct {
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	Error   string `json:"error"`
}

// MobilePairing is a code for one phone to come in with, valid for minutes.
type MobilePairing struct {
	Code string `json:"code"`
	// URL is what the QR code holds: the tailnet address when Tailscale runs here,
	// as the phone has no other way to this computer, else LocalURL.
	URL       string    `json:"url"`
	LocalURL  string    `json:"localUrl"`
	QRPNG     string    `json:"qrPng"`
	ExpiresAt time.Time `json:"expiresAt"`
}

var (
	errMobileNeedsProject = errors.New("another window owns this project; turn mobile access on there")
	errMobilePort         = errors.New("the port has to be between 1024 and 65535")
	errMobileNotRunning   = errors.New("mobile access is off")
)

// SetMobileAccess turns serving on or off and remembers it. Only the window that owns
// the project may, and it changes nothing otherwise, not even the setting. A port that
// cannot be served on leaves things as they were: the old server, if any, and the
// setting that says so.
func (a *App) SetMobileAccess(enabled bool, port int) (MobileStatus, error) {
	if port == 0 {
		port = defaultMobilePort
	}
	if port < 1024 || port > 65535 {
		return a.GetMobileStatus(), errMobilePort
	}
	if err := a.mobileOwnerCheck(); err != nil {
		status := a.GetMobileStatus()
		status.Error = err.Error()
		return status, err
	}
	if !enabled {
		a.stopMobile()
		err := a.storage.UpdateSettings(func(settings *session.Settings) {
			settings.MobileEnabled, settings.MobilePort = false, port
		})
		return a.GetMobileStatus(), err
	}
	if err := a.startMobileServer(port); err != nil {
		status := a.GetMobileStatus()
		status.Error = err.Error()
		return status, err
	}
	err := a.storage.UpdateSettings(func(settings *session.Settings) {
		settings.MobileEnabled, settings.MobilePort = true, port
	})
	return a.GetMobileStatus(), err
}

// mobileOwnerCheck refuses a window that does not own the project, naming the one
// that does.
func (a *App) mobileOwnerCheck() error {
	a.projectMu.RLock()
	owner, pid := a.projectLocked, a.otherInstancePID
	a.projectMu.RUnlock()
	if owner {
		return nil
	}
	return fmt.Errorf("%w (pid %d)", errMobileNeedsProject, pid)
}

// GetMobileStatus is what the settings show.
func (a *App) GetMobileStatus() MobileStatus {
	a.mobileMu.Lock()
	defer a.mobileMu.Unlock()
	port := a.mobilePort
	if port == 0 {
		port = defaultMobilePort
		if _, _, settings, err := a.storage.LoadAllWithSettings(); err == nil && settings != nil && settings.MobilePort != 0 {
			port = settings.MobilePort
		}
	}
	return MobileStatus{Running: a.mobileServer != nil, Port: port}
}

// startMobile serves on port if this window owns the project.
func (a *App) startMobile(port int) error {
	if err := a.mobileOwnerCheck(); err != nil {
		return err
	}
	return a.startMobileServer(port)
}

// startMobileServer serves on port, replacing a server on another one only once the
// new one listens.
func (a *App) startMobileServer(port int) error {
	a.mobileMu.Lock()
	defer a.mobileMu.Unlock()
	if a.mobileServer != nil && a.mobilePort == port {
		return nil
	}
	if a.mobileDevices == nil {
		devices, err := mobile.OpenDevices(a.storage.MobileDevicesPath())
		if err != nil {
			return err
		}
		a.mobileDevices = devices
	}
	server := mobile.NewServer(a.mobileDevices, port)
	if err := server.Start(); err != nil {
		return err
	}
	a.stopMobileServerLocked()
	a.mobileServer, a.mobilePort = server, port
	log.Printf("[mobile] serving on 127.0.0.1:%d", port)
	return nil
}

// reconcileMobileWithProject serves or stops as owning the project and its setting
// say. Called with projectMu held, or before anything else can take it.
func (a *App) reconcileMobileWithProject() {
	if !a.projectLocked {
		a.stopMobile()
		return
	}
	_, _, settings, err := a.storage.LoadAllWithSettings()
	if err != nil || settings == nil || !settings.MobileEnabled {
		a.stopMobile()
		return
	}
	port := settings.MobilePort
	if port == 0 {
		port = defaultMobilePort
	}
	if err := a.startMobileServer(port); err != nil {
		log.Printf("[mobile] could not serve on %d: %v", port, err)
	}
}

// startMobileIfEnabled brings back what was on when the app was last closed.
func (a *App) startMobileIfEnabled() {
	a.projectMu.RLock()
	defer a.projectMu.RUnlock()
	if a.projectLocked {
		a.reconcileMobileWithProject()
	}
}

func (a *App) stopMobile() {
	a.mobileMu.Lock()
	defer a.mobileMu.Unlock()
	a.stopMobileServerLocked()
}

func (a *App) stopMobileServerLocked() {
	if a.mobileServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.mobileServer.Stop(ctx)
	a.mobileServer = nil
	a.mobilePort = 0
}

// CreateMobilePairing makes a code for one phone, shown as a QR code. The address in
// it is this computer's own; through Tailscale the phone uses the tailnet name with
// the same code.
func (a *App) CreateMobilePairing() (MobilePairing, error) {
	a.mobileMu.Lock()
	server, devices, port := a.mobileServer, a.mobileDevices, a.mobilePort
	a.mobileMu.Unlock()
	if server == nil || devices == nil {
		return MobilePairing{}, errMobileNotRunning
	}
	code, expires, err := devices.NewPairing(time.Now())
	if err != nil {
		return MobilePairing{}, err
	}
	local := "http://127.0.0.1:" + strconv.Itoa(port) + "/#pair=" + code
	url := local
	// The tailnet address only when serve passes it on to this server: otherwise it
	// serves nothing, or something else, which would get the code.
	if name := tailnetName(); name != "" && serveProxiesTo(askTailscale("serve", "status", "--json"), port) {
		url = "https://" + name + "/#pair=" + code
	}
	image, err := qrcode.Encode(url, qrcode.Medium, 320)
	if err != nil {
		return MobilePairing{}, fmt.Errorf("qr: %w", err)
	}
	return MobilePairing{Code: code, URL: url, LocalURL: local, QRPNG: base64.StdEncoding.EncodeToString(image), ExpiresAt: expires}, nil
}

// ListMobileDevices gives the phones let in.
func (a *App) ListMobileDevices() []mobile.Device {
	devices, err := a.openMobileDevices()
	if err != nil {
		return []mobile.Device{}
	}
	return devices.List()
}

// RevokeMobileDevice shuts a phone out, at its next request.
func (a *App) RevokeMobileDevice(id string) error {
	devices, err := a.openMobileDevices()
	if err != nil {
		return err
	}
	return devices.Revoke(id)
}

func (a *App) openMobileDevices() (*mobile.Devices, error) {
	a.mobileMu.Lock()
	defer a.mobileMu.Unlock()
	if a.mobileDevices == nil {
		devices, err := mobile.OpenDevices(a.storage.MobileDevicesPath())
		if err != nil {
			return nil, err
		}
		a.mobileDevices = devices
	}
	return a.mobileDevices, nil
}

// tailscaleStatus asks Tailscale about this machine. A variable for tests.
var tailscaleStatus = func() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tailscaleTimeout)
	defer cancel()
	return tailscaleCommand(ctx, "status", "--json")
}

// tailnetName is this machine's name on the tailnet, or "" when Tailscale is not
// installed or not up.
func tailnetName() string {
	status, err := tailscaleStatus()
	if err != nil {
		return ""
	}
	return tailnetNameFrom(status)
}

func tailnetNameFrom(status []byte) string {
	var parsed struct {
		BackendState string
		Self         struct{ DNSName string }
	}
	if json.Unmarshal(status, &parsed) != nil || parsed.BackendState != "Running" {
		return ""
	}
	name := strings.ToLower(strings.TrimSuffix(parsed.Self.DNSName, "."))
	if !mobile.IsTailnetName(name) {
		return ""
	}
	return name
}
