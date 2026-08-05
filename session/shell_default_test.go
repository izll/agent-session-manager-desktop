package session

import (
	"runtime"
	"strings"
	"testing"
)

// Restarting a stopped pane has to name a command, and the name has to be one
// that exists on the platform. The Unix lookup ($SHELL, falling back to bash)
// was used on every platform, so a Windows machine — which has no $SHELL and
// normally no bash — restarted a terminal tab into a command that is not there.
// The pane died immediately and came back empty, while creating the same tab
// worked, because creation passes no command at all and lets the multiplexer
// start the shell itself.
func TestDefaultShellExistsOnThisPlatform(t *testing.T) {
	shell := defaultShell()

	if strings.TrimSpace(shell) == "" {
		t.Fatal("defaultShell returned nothing; respawn-pane would re-run the pane's dead start command")
	}

	if runtime.GOOS == "windows" {
		if strings.Contains(strings.ToLower(shell), "bash") {
			t.Errorf("defaultShell() = %q on Windows, which normally has no bash", shell)
		}
		return
	}

	if strings.EqualFold(shell, "cmd.exe") {
		t.Errorf("defaultShell() = %q on %s", shell, runtime.GOOS)
	}
}

// The environment decides, so a user whose shell is not the default still gets
// their own on restart rather than a hardcoded one.
func TestDefaultShellFollowsTheEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("COMSPEC", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`)
	} else {
		t.Setenv("SHELL", "/usr/bin/fish")
	}

	got := defaultShell()
	if runtime.GOOS == "windows" {
		if !strings.Contains(strings.ToLower(got), "powershell") {
			t.Errorf("defaultShell() = %q, want the COMSPEC value", got)
		}
		return
	}
	if got != "/usr/bin/fish" {
		t.Errorf("defaultShell() = %q, want the SHELL value", got)
	}
}

// An unset variable must still yield something runnable.
func TestDefaultShellFallsBackWhenUnset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("COMSPEC", "")
	} else {
		t.Setenv("SHELL", "")
	}

	if got := defaultShell(); strings.TrimSpace(got) == "" {
		t.Error("defaultShell returned nothing with the environment variable unset")
	}
}

// The setting wins over the platform default. This is the whole point on
// Windows, where cmd.exe and PowerShell are both reasonable and the
// environment does not say which the user wants.
func TestConfiguredShellOverridesThePlatformDefault(t *testing.T) {
	t.Cleanup(func() { SetTerminalShell("") })

	SetTerminalShell("powershell.exe")
	if got := defaultShell(); got != "powershell.exe" {
		t.Errorf("defaultShell() = %q, want the configured shell", got)
	}

	// Clearing it returns to the platform default rather than leaving an
	// empty command, which respawn-pane would reject.
	SetTerminalShell("")
	if got := defaultShell(); got != platformDefaultShell() {
		t.Errorf("defaultShell() = %q after clearing, want the platform default %q",
			got, platformDefaultShell())
	}
}

// Whitespace is not a shell: a field left with a stray space would otherwise
// become a command that cannot run.
func TestBlankConfiguredShellIsIgnored(t *testing.T) {
	t.Cleanup(func() { SetTerminalShell("") })

	SetTerminalShell("   ")
	if got := defaultShell(); got != platformDefaultShell() {
		t.Errorf("defaultShell() = %q, want the platform default", got)
	}
}
