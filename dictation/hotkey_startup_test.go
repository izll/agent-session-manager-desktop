package dictation

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The global hotkey listener installs a system-wide keyboard hook. On macOS
// that makes the OS demand Accessibility permission, so starting it when
// dictation is switched off means a modal on every launch for a feature the
// user is not using.
//
// These read the source rather than calling the code: Enable() starts a real
// OS-level hook, which a test must not do. What matters is the wiring — that
// startup is guarded, and that SaveSettings still starts the listener so
// turning dictation on does not require a restart. Both were wrong at some
// point: the guard was missing, and adding it alone silently broke the hotkey
// until the app was restarted.
func readSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("app_service.go")
	if err != nil {
		t.Fatalf("reading app_service.go: %v", err)
	}
	// Line endings normalised: git checks out CRLF on Windows by default, and
	// this test looks for "\n}\n" to find where a function ends. Reading the
	// raw bytes there finds nothing and reports the function as unterminated.
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestStartupDoesNotEnableHotkeyUnconditionally(t *testing.T) {
	src := readSource(t)

	// NewAppService ends at the "return app" that closes it.
	start := strings.Index(src, "func NewAppService()")
	if start < 0 {
		t.Fatal("NewAppService not found")
	}
	end := strings.Index(src[start:], "\n}\n")
	if end < 0 {
		t.Fatal("could not find the end of NewAppService")
	}
	body := src[start : start+end]

	if !strings.Contains(body, "hotkeyManager.Enable()") {
		t.Fatal("NewAppService no longer enables the hotkey at all; if this moved, update this test")
	}

	// The Enable() call must sit behind a check on the Enabled setting.
	guard := regexp.MustCompile(`if\s+app\.settings\.Enabled\s*\{`)
	if !guard.MatchString(body) {
		t.Error("startup enables the global hotkey without checking settings.Enabled — " +
			"this shows the macOS Accessibility prompt to users who have dictation switched off")
	}
}

func TestSaveSettingsEnablesHotkeyWhenSwitchedOn(t *testing.T) {
	src := readSource(t)

	start := strings.Index(src, "func (a *AppService) SaveSettings(")
	if start < 0 {
		t.Fatal("SaveSettings not found")
	}
	end := strings.Index(src[start:], "\n}\n")
	if end < 0 {
		t.Fatal("could not find the end of SaveSettings")
	}
	body := src[start : start+end]

	if !strings.Contains(body, "hotkeyManager.Enable()") {
		t.Error("SaveSettings does not enable the hotkey — with startup now guarded, " +
			"switching dictation on would not work until the app is restarted")
	}
	if !strings.Contains(body, "settings.Enabled") {
		t.Error("SaveSettings enables the hotkey without checking settings.Enabled")
	}
}
