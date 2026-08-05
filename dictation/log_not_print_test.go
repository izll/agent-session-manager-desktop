package dictation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Dictation writes to its log, not to stdout.
//
// It used to print: "Started listening", the buffer-mode decisions, the missing
// xdotool warning. Launched from a desktop menu there is no terminal to read,
// and the log viewer — where anyone would look — carried none of it. The one
// failure a user cannot diagnose from the outside was also the one that left no
// trace.
//
// debug.go is exempt: it reports on the logging system itself, and cannot write
// to a log that is not open yet.
func TestDictationLogsRatherThanPrints(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") ||
			name == "debug.go" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}

		for i, line := range strings.Split(string(data), "\n") {
			code := line
			if at := strings.Index(code, "//"); at >= 0 {
				code = code[:at]
			}
			if strings.Contains(code, "fmt.Print") {
				t.Errorf("%s:%d prints instead of logging. Launched from a desktop "+
					"menu there is no terminal to read it in, and it will not reach "+
					"the log viewer:\n  %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}
