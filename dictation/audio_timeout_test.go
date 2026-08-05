package dictation

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Every mixer and device query is bounded.
//
// pactl blocks indefinitely when the sound server is not answering, and
// GetInputDevices runs on the way into the Dictation settings tab — so an
// unbounded call froze the whole window. That tab is opened by someone whose
// audio is already misbehaving, which is exactly when the daemon is least
// likely to reply.
func TestAudioCommandsAreBounded(t *testing.T) {
	t.Parallel()

	for _, file := range []string{"audio_names_linux.go", "audio_mute.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		source := string(data)
		for i, line := range strings.Split(source, "\n") {
			code := line
			if at := strings.Index(code, "//"); at >= 0 {
				code = code[:at]
			}
			// exec.Command with no context: the form that cannot time out.
			if strings.Contains(code, "exec.Command(") && !strings.Contains(code, "exec.CommandContext(") {
				t.Errorf("%s:%d runs an external command with no time limit. A sound "+
					"server that stops answering then hangs the caller:\n  %s",
					file, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// A bounded command actually gives up rather than waiting forever.
func TestABoundedCommandGivesUp(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	// sleep stands in for a query that never returns.
	err := exec.CommandContext(ctx, "sleep", "10").Run()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a command past its deadline returned success")
	}
	if elapsed > 2*time.Second {
		t.Errorf("waited %v for a 150ms deadline — the context is not being honoured", elapsed)
	}
}
