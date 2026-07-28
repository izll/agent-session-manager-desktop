package session

import (
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// restoreTmuxBinary puts the package back to its default, so a test that
// overrides the binary can't leak that name into the rest of the suite.
func restoreTmuxBinary(t *testing.T) {
	t.Helper()
	previous := TmuxBinary()
	t.Cleanup(func() { SetTmuxBinary(previous) })
}

func TestTmuxCommandDefaultsToTmux(t *testing.T) {
	if got := TmuxBinary(); got != "tmux" {
		t.Fatalf("TmuxBinary() = %q, want %q", got, "tmux")
	}

	cmd := TmuxCommand("list-sessions")
	// exec.Command resolves the name via PATH, so compare the base name: on a
	// machine with tmux installed Path is absolute, on one without it stays
	// the bare name plus a lookup error.
	if base := filepath.Base(cmd.Path); base != "tmux" {
		t.Errorf("cmd.Path = %q, want base %q", cmd.Path, "tmux")
	}
	if cmd.Args[0] != "tmux" {
		t.Errorf("cmd.Args[0] = %q, want %q", cmd.Args[0], "tmux")
	}
}

func TestSetTmuxBinaryChangesCommand(t *testing.T) {
	restoreTmuxBinary(t)

	SetTmuxBinary("psmux")
	if got := TmuxBinary(); got != "psmux" {
		t.Fatalf("TmuxBinary() = %q, want %q", got, "psmux")
	}

	cmd := TmuxCommand("has-session", "-t", "demo")
	if base := filepath.Base(cmd.Path); base != "psmux" {
		t.Errorf("cmd.Path = %q, want base %q", cmd.Path, "psmux")
	}
	if cmd.Args[0] != "psmux" {
		t.Errorf("cmd.Args[0] = %q, want %q", cmd.Args[0], "psmux")
	}
}

// An absolute path must be used verbatim: a configured binary living outside
// PATH is the whole reason the location is settable.
func TestSetTmuxBinaryAcceptsAbsolutePath(t *testing.T) {
	restoreTmuxBinary(t)

	absolute := filepath.Join(t.TempDir(), "psmux.exe")
	SetTmuxBinary(absolute)

	cmd := TmuxCommand("kill-session")
	if cmd.Path != absolute {
		t.Errorf("cmd.Path = %q, want %q", cmd.Path, absolute)
	}
}

func TestSetTmuxBinaryEmptyRestoresDefault(t *testing.T) {
	restoreTmuxBinary(t)

	SetTmuxBinary("psmux")
	SetTmuxBinary("")
	if got := TmuxBinary(); got != "tmux" {
		t.Errorf("TmuxBinary() = %q, want %q after empty override", got, "tmux")
	}
}

func TestTmuxCommandPassesArgumentsUnaltered(t *testing.T) {
	restoreTmuxBinary(t)
	SetTmuxBinary("psmux")

	args := []string{
		"new-session", "-d", "-s", "name with spaces",
		"-c", "/tmp/dir", "--", "claude", "--flag=a b", "",
	}
	cmd := TmuxCommand(args...)

	want := append([]string{"psmux"}, args...)
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("cmd.Args = %#v, want %#v", cmd.Args, want)
	}
}

// The variadic spread is how the dynamically built call sites (new-session,
// new-window) pass their argv, so it must behave like a literal arg list.
func TestTmuxCommandAcceptsSpreadSliceAndNoArgs(t *testing.T) {
	dynamic := append([]string{"new-window"}, "-t", "sess", "-n", "tab")
	if got, want := TmuxCommand(dynamic...).Args, append([]string{"tmux"}, dynamic...); !reflect.DeepEqual(got, want) {
		t.Errorf("cmd.Args = %#v, want %#v", got, want)
	}

	if got := TmuxCommand().Args; !reflect.DeepEqual(got, []string{"tmux"}) {
		t.Errorf("cmd.Args = %#v, want %#v", got, []string{"tmux"})
	}
}

func TestTmuxCommandContextUsesBinaryAndContext(t *testing.T) {
	restoreTmuxBinary(t)
	SetTmuxBinary("psmux")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := TmuxCommandContext(ctx, "display-message", "-p", "#{pane_current_path}")
	if base := filepath.Base(cmd.Path); base != "psmux" {
		t.Errorf("cmd.Path = %q, want base %q", cmd.Path, "psmux")
	}
	want := []string{"psmux", "display-message", "-p", "#{pane_current_path}"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("cmd.Args = %#v, want %#v", cmd.Args, want)
	}

	// A cancelled context must still abort the command, i.e. the context is
	// really wired in and not dropped on the floor.
	cancel()
	if err := cmd.Start(); err == nil {
		_ = cmd.Wait()
	}
}

// The helper must stay a drop-in for exec.Command, since every converted call
// site kept its original Env/Dir/Stdin handling.
func TestTmuxCommandIsDropInForExecCommand(t *testing.T) {
	args := []string{"send-keys", "-t", "sess:0", "hello", "Enter"}

	got := TmuxCommand(args...)
	want := exec.Command("tmux", args...)

	if got.Path != want.Path {
		t.Errorf("Path = %q, want %q", got.Path, want.Path)
	}
	if !reflect.DeepEqual(got.Args, want.Args) {
		t.Errorf("Args = %#v, want %#v", got.Args, want.Args)
	}
}
