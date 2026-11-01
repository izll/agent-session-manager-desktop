//go:build !windows

package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Against tmux itself. The password reaches the program and nothing else: no buffer
// is left to list, and it never stood on a command line.
func TestAPastedSecretLeavesNoBuffer(t *testing.T) {
	tmux := isolatedTmux(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "typed")
	inst := &Instance{ID: "paste", Name: "paste", Path: dir}
	name := inst.TmuxSessionName()
	if _, err := tmux("new-session", "-d", "-s", name, "-x", "80", "-y", "24", "cat > '"+out+"'"); err != nil {
		t.Fatal(err)
	}
	const secret = `se:cret $1 "x" 'y'`
	if err := inst.PasteSecretToWindow(context.Background(), 0, secret, true); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(out)
		if strings.TrimRight(string(data), "\n") == secret {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("typed = %q", data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	buffers, err := tmux("list-buffers")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(buffers)) != "" {
		t.Fatalf("a buffer was left: %q", buffers)
	}
}

// Without Enter it waits there to be looked at: the default, as a password typed at
// the wrong prompt is one that can still be deleted.
func TestAPastedSecretHasNoEnterUnlessAsked(t *testing.T) {
	tmux := isolatedTmux(t)
	inst := &Instance{ID: "paste2", Name: "paste2", Path: t.TempDir()}
	name := inst.TmuxSessionName()
	if _, err := tmux("new-session", "-d", "-s", name, "-x", "80", "-y", "24", "cat"); err != nil {
		t.Fatal(err)
	}
	if err := inst.PasteSecretToWindow(context.Background(), 0, "abc", false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	lines, err := tmux("capture-pane", "-p", "-t", name+":0")
	if err != nil {
		t.Fatal(err)
	}
	// cat echoes a finished line a second time; an unfinished one only once.
	if strings.Count(string(lines), "abc") != 1 {
		t.Fatalf("pane = %q", lines)
	}
}

func TestAPastedSecretIsNotOnACommandLine(t *testing.T) {
	source, err := os.ReadFile("instance.go")
	if err != nil {
		t.Fatal(err)
	}
	body := functionBody(t, string(source), "func (i *Instance) PasteSecretToWindow(") +
		functionBody(t, string(source), "func pasteThroughBuffer(")
	if !strings.Contains(body, `writer.RunInput(ctx, secret, "load-buffer", "-b", buffer, "-")`) {
		t.Fatal("the secret no longer goes in through standard input")
	}
	if strings.Contains(body, "%w") || strings.Contains(body, "%v") || strings.Contains(body, "err.Error") {
		t.Fatal("an error here could carry the secret")
	}
}

func TestPastingIntoAMissingWindowFails(t *testing.T) {
	isolatedTmux(t)
	inst := &Instance{ID: "paste3", Name: "paste3", Path: t.TempDir()}
	if err := inst.PasteSecretToWindow(context.Background(), 0, "x", false); err == nil {
		t.Fatal("no error for a tab that is not there")
	}
}

// A paste turns a line break into Enter, submitting half a password.
func TestASecretWithControlCharactersIsRefused(t *testing.T) {
	inst := &Instance{ID: "paste4", Name: "paste4", Path: t.TempDir()}
	for _, secret := range []string{"a\nb", "a\rb", "a\tb", "a\x1b[0m", "a\x7f", ""} {
		if err := inst.PasteSecretToWindow(context.Background(), 0, secret, false); !errors.Is(err, ErrSecretCannotBeTyped) {
			t.Errorf("%q: %v", secret, err)
		}
	}
}

type failingExecutor struct {
	Executor
	failPaste bool
	failLoad  bool
	deleteErr error
	deletes   int
}

func (f *failingExecutor) Run(ctx context.Context, args ...string) error {
	switch args[0] {
	case "paste-buffer":
		if f.failPaste {
			return errors.New("no")
		}
	case "delete-buffer":
		f.deletes++
		f.deleteErr = ctx.Err()
	}
	return nil
}

func (f *failingExecutor) RunInput(context.Context, string, ...string) error {
	if f.failLoad {
		return errors.New("no")
	}
	return nil
}

// A buffer left behind holds the password where any tab can read it.
func TestAFailedPasteLeavesNoBuffer(t *testing.T) {
	for name, executor := range map[string]*failingExecutor{
		"paste": {failPaste: true},
		"load":  {failLoad: true},
	} {
		t.Run(name, func(t *testing.T) {
			// Already out of time, as it is when a deadline is why this failed.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := pasteThroughBuffer(ctx, executor, executor, "s:0", "x"); err == nil {
				t.Fatal("no error")
			}
			if executor.deletes != 1 {
				t.Fatalf("deletes = %d", executor.deletes)
			}
			if executor.deleteErr != nil {
				t.Fatalf("cleanup ran on the context that had ended: %v", executor.deleteErr)
			}
		})
	}
}

func TestAPasteThatWorkedDeletesNothingElse(t *testing.T) {
	executor := &failingExecutor{}
	if err := pasteThroughBuffer(context.Background(), executor, executor, "s:0", "x"); err != nil {
		t.Fatal(err)
	}
	if executor.deletes != 0 {
		t.Fatalf("deletes = %d", executor.deletes)
	}
}
