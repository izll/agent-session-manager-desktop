package session

import "testing"

// The parsing has to be exact. Two sessions whose names differ by a single
// character are the case this exists for: resolving one to the other leaves a
// session with no client, which reads as a terminal that ignores everything.
func TestParseSessionIDLine(t *testing.T) {
	listing := "$61 asm_claude_asmgr-teszt2_1785309617457458900\n" +
		"$62 asm_claude_asmgr-teszt-2_1785265123749939800\n" +
		"$7 other\n"

	tests := []struct {
		name string
		want string
	}{
		{"asm_claude_asmgr-teszt2_1785309617457458900", "$61"},
		{"asm_claude_asmgr-teszt-2_1785265123749939800", "$62"},
		{"other", "$7"},
		// A prefix of a real name must NOT match: that is the mix-up itself.
		{"asm_claude_asmgr-teszt2", ""},
		{"asm_claude", ""},
		{"", ""},
		{"nonexistent", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lookupSessionID(listing, tt.name); got != tt.want {
				t.Fatalf("lookupSessionID(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// Session names may contain spaces, so the split has to happen once, at the
// first space, not on every one.
func TestParseSessionIDLineWithSpaces(t *testing.T) {
	listing := "$3 my session with spaces\n"
	if got := lookupSessionID(listing, "my session with spaces"); got != "$3" {
		t.Fatalf("got %q, want $3 — the name was split on the wrong space", got)
	}
}

// CRLF line endings must not become part of the name.
func TestParseSessionIDLineHandlesCRLF(t *testing.T) {
	if got := lookupSessionID("$9 win-session\r\n", "win-session"); got != "$9" {
		t.Fatalf("got %q, want $9 — a trailing CR leaked into the name", got)
	}
}
