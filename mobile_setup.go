package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"asmgr-desktop/session"
)

// Getting a phone to the app, step by step: Tailscale installed, logged in, mobile
// access on, served on the tailnet, and the phone paired. Each step is read from the
// machine, so the guide says where things stand however they got there.

// MobileSetup is where setting up the phone's way in stands.
type MobileSetup struct {
	// Step is the one to do next: install, login, enable, serve or phone.
	Step        string            `json:"step"`
	Installed   bool              `json:"installed"`
	LoggedIn    bool              `json:"loggedIn"`
	Serving     bool              `json:"serving"`
	TailnetName string            `json:"tailnetName"`
	Platform    string            `json:"platform"`
	Commands    map[string]string `json:"commands"`
}

const tailscaleTimeout = 3 * time.Second

// tailscaleCommand runs tailscale. A variable for tests.
var tailscaleCommand = func(ctx context.Context, args ...string) ([]byte, error) {
	return session.CommandContext(ctx, tailscaleBinary(), args...).Output()
}

// tailscaleFound says whether tailscale is installed. A variable for tests.
var tailscaleFound = func() bool { return tailscaleBinary() != "" }

// macTailscale is where the Mac app keeps its command line, which is on no PATH.
const macTailscale = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"

func tailscaleBinary() string {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path
	}
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(macTailscale); err == nil {
			return macTailscale
		}
	}
	return ""
}

func askTailscale(args ...string) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), tailscaleTimeout)
	defer cancel()
	output, err := tailscaleCommand(ctx, args...)
	if err != nil {
		return nil
	}
	return output
}

// setupStep is the step to do next.
func setupStep(installed bool, backend string, mobileOn, serving bool) string {
	switch {
	case !installed:
		return "install"
	case backend != "Running":
		return "login"
	case !mobileOn:
		return "enable"
	case !serving:
		return "serve"
	default:
		return "phone"
	}
}

// serveProxiesTo says whether tailscale serve passes the tailnet on to this port.
func serveProxiesTo(serveStatus []byte, port int) bool {
	var status struct {
		Web map[string]struct {
			Handlers map[string]struct{ Proxy string }
		}
	}
	if json.Unmarshal(serveStatus, &status) != nil {
		return false
	}
	target := "http://127.0.0.1:" + strconv.Itoa(port)
	for _, site := range status.Web {
		for _, handler := range site.Handlers {
			if handler.Proxy == target || handler.Proxy == target+"/" {
				return true
			}
		}
	}
	return false
}

// setupCommands are what each step runs, fixed: only the port goes in, as a number.
// On a Mac and on Windows Tailscale's own app installs and logs in.
func setupCommands(goos string, port int) map[string]string {
	serve := " serve --bg http://127.0.0.1:" + strconv.Itoa(port)
	commands := map[string]string{"install": "", "login": "", "serve": "tailscale" + serve}
	switch goos {
	case "linux":
		commands["install"] = "curl -fsSL https://tailscale.com/install.sh | sh"
		commands["login"] = "sudo tailscale up --operator=$USER"
		// With sudo all the same: whoever had Tailscale already never ran the login
		// above, is no operator, and serve is refused to them without it.
		commands["serve"] = "sudo tailscale" + serve
	case "darwin":
		// The constant, not a lookup: what is typed is fixed.
		commands["serve"] = macTailscale + serve
	}
	return commands
}

// GetMobileSetup reads where setting up the phone's way in stands.
func (a *App) GetMobileSetup() MobileSetup {
	mobile := a.GetMobileStatus()
	setup := MobileSetup{
		Installed: tailscaleFound(),
		Platform:  runtime.GOOS,
		Commands:  setupCommands(runtime.GOOS, mobile.Port),
	}
	backend := ""
	if setup.Installed {
		var status struct{ BackendState string }
		raw := askTailscale("status", "--json")
		if json.Unmarshal(raw, &status) == nil {
			backend = status.BackendState
		}
		setup.LoggedIn = backend == "Running"
		setup.TailnetName = tailnetNameFrom(raw)
		if setup.LoggedIn && mobile.Running {
			setup.Serving = serveProxiesTo(askTailscale("serve", "status", "--json"), mobile.Port)
		}
	}
	setup.Step = setupStep(setup.Installed, backend, mobile.Running, setup.Serving)
	return setup
}
