package session

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Every session this app creates lives in a terminal multiplexer: tmux on Linux
// and macOS, psmux on Windows. Without one there is nothing to create a session
// in, so a missing binary is not a degraded mode — it is the app unable to do
// the only thing it does.
//
// It was not checked anywhere. A session was created and saved, and only the
// commands that followed failed, which put a permanent entry in the sidebar for
// a session that had never run and never could. The agent command is checked
// before saving for exactly that reason; this is the same check for the other
// half of what a session needs.

// MultiplexerInstallHint is a short, copyable instruction for installing the
// multiplexer this platform uses.
//
// Only the package managers likely to be present are named. A user who has
// something else does not need to be told what a package manager is, and a list
// covering every distribution would bury the one line that applies.
func MultiplexerInstallHint() string {
	switch runtime.GOOS {
	case "windows":
		return "winget install psmux, choco install psmux, or download it from " +
			"https://github.com/psmux/psmux/releases and put it on your PATH"
	case "darwin":
		return "brew install tmux"
	default:
		return "sudo apt install tmux, sudo dnf install tmux, " +
			"or sudo pacman -S tmux"
	}
}

// multiplexerLookup caches the result of resolving the binary.
//
// The check runs on session creation and on startup, and PATH lookups hit the
// filesystem once per directory in PATH. Cached briefly rather than for the
// process's lifetime so installing the binary while the app is open is noticed
// without a restart.
var multiplexerLookup struct {
	sync.Mutex
	binary  string
	err     error
	version string
	checked time.Time
}

const multiplexerLookupTTL = 5 * time.Second
const multiplexerProbeTimeout = 5 * time.Second

var multiplexerLookPath = exec.LookPath
var multiplexerProbe = probeMultiplexerVersion

func probeMultiplexerVersion(binary string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), multiplexerProbeTimeout)
	defer cancel()
	out, err := CommandContext(ctx, binary, "-V").CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("version check timed out after %s", multiplexerProbeTimeout)
	}
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return "", fmt.Errorf("version check returned no output")
	}
	return version, nil
}

// CheckMultiplexer reports whether the terminal multiplexer is available,
// naming it and how to install it when it is not.
func CheckMultiplexer() error {
	binary := TmuxBinary()

	multiplexerLookup.Lock()
	defer multiplexerLookup.Unlock()
	if multiplexerLookup.binary == binary &&
		time.Since(multiplexerLookup.checked) < multiplexerLookupTTL {
		return multiplexerLookup.err
	}

	var err error
	var version string
	resolved, lookErr := multiplexerLookPath(binary)
	if lookErr != nil {
		// The message carries the install line because this reaches the user as
		// a dialog with no other context: "psmux not found" alone leaves them to
		// discover what psmux is and where it comes from.
		err = fmt.Errorf("%s is required but was not found. Install it with: %s",
			binary, MultiplexerInstallHint())
	} else if version, err = multiplexerProbe(resolved); err != nil {
		err = fmt.Errorf("%s was found but cannot be started: %w. Reinstall it with: %s",
			binary, err, MultiplexerInstallHint())
	}

	multiplexerLookup.binary = binary
	multiplexerLookup.err = err
	multiplexerLookup.version = version
	multiplexerLookup.checked = time.Now()
	return err
}

// MultiplexerAvailable reports availability without the message, for callers
// deciding what to show rather than what to say.
func MultiplexerAvailable() bool {
	return CheckMultiplexer() == nil
}

// MultiplexerName is the binary this platform expects, for display.
func MultiplexerName() string {
	return TmuxBinary()
}

// ResetMultiplexerCheckCache drops the cached lookup, so a check made right
// after the user changes the configured binary sees the new one.
func ResetMultiplexerCheckCache() {
	multiplexerLookup.Lock()
	defer multiplexerLookup.Unlock()
	multiplexerLookup.checked = time.Time{}
	multiplexerLookup.version = ""
}

// MultiplexerVersion returns what the multiplexer reports about itself, for
// diagnostics. Empty when it cannot be run.
func MultiplexerVersion() string {
	if CheckMultiplexer() != nil {
		return ""
	}
	multiplexerLookup.Lock()
	defer multiplexerLookup.Unlock()
	return multiplexerLookup.version
}
