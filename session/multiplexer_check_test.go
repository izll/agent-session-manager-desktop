package session

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// A session cannot exist without the multiplexer, so a missing one has to be
// reported before anything is written. It was not checked at all: the session
// was created and saved, the commands that followed failed one by one, and the
// sidebar kept an entry for a session that had never run and never could.
func TestMissingMultiplexerIsReported(t *testing.T) {
	original := TmuxBinary()
	t.Cleanup(func() {
		SetTmuxBinary(original)
		ResetMultiplexerCheckCache()
	})

	SetTmuxBinary("asmgr-no-such-multiplexer-binary")
	ResetMultiplexerCheckCache()

	err := CheckMultiplexer()
	if err == nil {
		t.Fatal("no error for a binary that does not exist")
	}
	if MultiplexerAvailable() {
		t.Error("MultiplexerAvailable is true for a binary that does not exist")
	}

	// The message is the user's only source of information here — it arrives as
	// a dialog with no surrounding context.
	msg := err.Error()
	if !strings.Contains(msg, "asmgr-no-such-multiplexer-binary") {
		t.Errorf("the message does not name what is missing: %q", msg)
	}
	if !strings.Contains(msg, MultiplexerInstallHint()) {
		t.Errorf("the message does not say how to install it: %q", msg)
	}
}

// Naming the binary is not enough on its own: "psmux is required" leaves a
// Windows user to work out what psmux is and where it comes from.
func TestInstallHintNamesSomethingToRun(t *testing.T) {
	hint := MultiplexerInstallHint()
	if hint == "" {
		t.Fatal("no install hint for this platform")
	}

	var want string
	switch runtime.GOOS {
	case "windows":
		want = "psmux"
	default:
		want = "tmux"
	}
	if !strings.Contains(hint, want) {
		t.Errorf("hint = %q, which does not mention %s", hint, want)
	}
	// A hint that does not tell the user what to type is not a hint.
	if !strings.ContainsAny(hint, " ") || len(hint) < 10 {
		t.Errorf("hint = %q, which is too terse to act on", hint)
	}
}

// Installing the multiplexer while the app is open has to be noticed. The
// lookup is cached to keep session creation off the filesystem, and a cache
// with no expiry would make the user restart to be believed.
func TestTheLookupCacheExpires(t *testing.T) {
	if multiplexerLookupTTL <= 0 {
		t.Fatal("the cache never expires, so installing the multiplexer while " +
			"the app is running would go unnoticed")
	}
	if multiplexerLookupTTL > 30*time.Second {
		t.Errorf("cache TTL is %v, long enough that a user who installs the "+
			"multiplexer and retries is still refused", multiplexerLookupTTL)
	}
}

// Changing the configured binary must not be answered from the previous one's
// result.
func TestChangingTheBinaryIsNoticed(t *testing.T) {
	original := TmuxBinary()
	t.Cleanup(func() {
		SetTmuxBinary(original)
		ResetMultiplexerCheckCache()
	})

	SetTmuxBinary("asmgr-no-such-multiplexer-binary")
	ResetMultiplexerCheckCache()
	if CheckMultiplexer() == nil {
		t.Fatal("expected the missing binary to fail")
	}

	// A binary that certainly exists on every platform this builds for.
	SetTmuxBinary(existingBinaryForTest())
	if err := CheckMultiplexer(); err != nil {
		t.Errorf("after pointing at a binary that exists: %v — the previous "+
			"answer was reused", err)
	}
}

func existingBinaryForTest() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}
