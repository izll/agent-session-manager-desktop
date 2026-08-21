//go:build windows

package session

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Windows is the one platform where the app can install the multiplexer itself.
//
// It is also where it is most often missing: tmux comes with, or one package
// manager command away on, most Linux and macOS machines, while psmux is a
// separate download most people have never heard of. And winget is present on
// current Windows, needs no elevation for this package, and reports what it
// did — so the app can offer the install rather than only describe it.
//
// The other platforms deliberately do not do this. Linux would need a
// privilege prompt for a system package manager, and macOS would need Homebrew,
// which may itself not be installed. Printing the command for the user to run
// is the honest option there.

// wingetPackageID is the psmux package.
//
// Pinned to the id rather than the name so winget cannot resolve "psmux" to
// something else: `winget search psmux` also lists psmux.TerminalMap. Verified
// against the registry — this one is published from github.com/psmux, which is
// the project itself.
const wingetPackageID = "marlocarlo.psmux"

const (
	multiplexerInstallOutputLimit = 1 << 20
	multiplexerInstallWaitDelay   = 2 * time.Second
)

type boundedInstallOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (w *boundedInstallOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := multiplexerInstallOutputLimit - len(w.data)
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		w.data = append(w.data, p[:remaining]...)
	}
	if remaining < len(p) {
		w.truncated = true
	}
	return len(p), nil
}

func (w *boundedInstallOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := string(w.data)
	if w.truncated {
		result += "\n[output truncated]"
	}
	return result
}

// InstallMultiplexerSupported reports whether this platform can install it.
func InstallMultiplexerSupported() bool {
	_, err := exec.LookPath("winget")
	return err == nil
}

// InstallMultiplexer installs psmux with winget and reports what happened.
//
// Output is returned rather than logged: an install that fails does so for
// reasons only winget knows — no network, a source needing agreement, a package
// already installed but not on PATH — and the user can act on none of them
// unless they are shown.
func InstallMultiplexer() (output string, err error) {
	if _, lookErr := exec.LookPath("winget"); lookErr != nil {
		return "", fmt.Errorf("winget is not available on this system — "+
			"install psmux manually: %s", MultiplexerInstallHint())
	}

	// Long enough for a slow connection, bounded so a winget waiting on a
	// prompt we cannot see does not hang the button forever.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "winget", "install",
		"--id", wingetPackageID,
		"--exact",
		// Both agreements: without them winget stops on an interactive prompt
		// that has nowhere to appear, and the call would time out looking like
		// a hang.
		"--accept-package-agreements",
		"--accept-source-agreements",
		// No interactive installer UI, and no progress bar redrawing into a
		// buffer nobody renders.
		"--silent",
		"--disable-interactivity",
	)
	HideConsoleWindow(cmd)
	// A detached installer process may inherit winget's output descriptors.
	// Bound the post-cancellation pipe wait as well as winget itself.
	cmd.WaitDelay = multiplexerInstallWaitDelay

	var buf boundedInstallOutput
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	output = strings.TrimSpace(buf.String())

	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("the install did not finish within five minutes")
	}
	if runErr != nil {
		return output, fmt.Errorf("winget failed: %w", runErr)
	}

	// winget exiting 0 is not proof the app can use it: the package installs
	// into a directory that a running process's PATH may not include yet. So
	// the answer to "did this work" is the same lookup everything else uses.
	ResetMultiplexerCheckCache()
	if err := CheckMultiplexer(); err != nil {
		return output, fmt.Errorf("psmux was installed but is not on this " +
			"application's PATH yet — restart the app")
	}
	return output, nil
}
