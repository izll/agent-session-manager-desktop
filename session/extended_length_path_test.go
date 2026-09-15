package session

import (
	"path/filepath"
	"runtime"
	"testing"
)

// gopsutil reports open files in extended-length form on Windows, while every
// root here is built from os.UserHomeDir and has no prefix. filepath.Rel
// refuses to relate the two, so the candidate was discarded and detection
// returned nothing — silently, which is the worst shape for this bug.
//
// Measured on Windows 11 against a live agent:
//
//	inside("C:\...\presence", "\\?\C:\...\presence\x.lock") = false
//	Rel: can't make \\?\C:\... relative to C:\...
func TestTrimExtendedLengthPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{`\\?\C:\Users\User\.gemini\antigravity-cli\presence\x.lock`,
			`C:\Users\User\.gemini\antigravity-cli\presence\x.lock`},
		{`\\?\UNC\server\share\dir\x.lock`, `\\server\share\dir\x.lock`},
		// Untouched: no prefix, and the POSIX paths every other platform gives.
		{`C:\Users\User\x.lock`, `C:\Users\User\x.lock`},
		{"/home/izll/.gemini/antigravity-cli/presence/x.lock",
			"/home/izll/.gemini/antigravity-cli/presence/x.lock"},
		{"", ""},
	}
	for _, c := range cases {
		if got := trimExtendedLengthPrefix(c.in); got != c.want {
			t.Errorf("trimExtendedLengthPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The detectors have to accept what gopsutil actually hands them. Run on
// Windows this exercises the real path semantics; elsewhere it still proves
// the prefix is stripped before anything else looks at the string.
func TestDetectorsAcceptExtendedLengthPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		// The rest of the pipeline uses the host's separator, so build the
		// fixture with it and only check that the prefix itself is no longer
		// what stands in the way.
		prefixed := `\\?\` + filepath.Join("C:", "tmp", "x.lock")
		if trimExtendedLengthPrefix(prefixed) == prefixed {
			t.Fatal("the prefix survived, so Rel would still fail on Windows")
		}
		return
	}

	presence := `C:\Users\User\.gemini\antigravity-cli\presence`
	lock := `\\?\C:\Users\User\.gemini\antigravity-cli\presence\e20233a0-6da5-4307-8442-cf6d025fa794.lock`
	if got := antigravityConversationIDFromOpenPaths(presence, []string{lock}); got != "e20233a0-6da5-4307-8442-cf6d025fa794" {
		t.Errorf("antigravity: got %q from an extended-length lock path", got)
	}

	chats := `C:\Users\User\.cursor\chats`
	store := `\\?\C:\Users\User\.cursor\chats\ce7d1929e33b1dd2d17b39bda7050b67\b07fab2d-0766-45ce-b2eb-3e74079aaaac\store.db`
	if got := cursorChatIDFromOpenPaths(chats, "", []string{store}); got != "b07fab2d-0766-45ce-b2eb-3e74079aaaac" {
		t.Errorf("cursor: got %q from an extended-length store path", got)
	}
}
