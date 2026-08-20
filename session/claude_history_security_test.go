package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Session ids and project paths are private user data. A pair of old
// diagnostics wrote them unconditionally to predictable, world-readable /tmp
// files. Keep the security boundary explicit: this source must not grow a
// global temporary debug sink again.
func TestClaudeHistoryHasNoGlobalTempDebugSink(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(file), "claude_history.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/tmp/asmgr_", `os.WriteFile("/tmp/`} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("claude history contains an unsafe global debug sink %q", forbidden)
		}
	}
}
