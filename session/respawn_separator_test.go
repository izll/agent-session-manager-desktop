package session

import (
	"os"
	"strings"
	"testing"
)

// psmux — the Windows multiplexer — documents respawn-pane as "restart the
// pane's shell", and drops a command given without a "--" separator instead of
// reporting anything. The pane came back as a bare PowerShell prompt whatever
// agent had been asked for, and stopping a tab left a fresh shell rather than a
// dead pane. Measured on Windows: with the separator the command runs, without
// it the shell starts; tmux accepts both, so one form serves both platforms.
func TestRespawnPaneArgsSeparateTheCommand(t *testing.T) {
	args := respawnPaneArgs(nil, "sess:2", "agy", "--conversation", "abc")

	joined := strings.Join(args, " ")
	if joined != "respawn-pane -k -t sess:2 -- agy --conversation abc" {
		t.Errorf("respawn-pane args = %q", joined)
	}

	// The separator has to come after the target and before the command: in
	// front of -t it would be read as the command itself.
	sep, target := indexOf(args, "--"), indexOf(args, "-t")
	if sep < 0 {
		t.Fatal("no -- separator; psmux drops the command and starts a shell")
	}
	if sep < target {
		t.Error("the separator precedes -t, so the target is read as the command")
	}
}

// A terminal tab restarts in the directory it was left in, which adds flags
// between -k and -t. They must stay on the flag side of the separator.
func TestRespawnPaneArgsKeepFlagsBeforeTheSeparator(t *testing.T) {
	args := respawnPaneArgs([]string{"-c", "/tmp/work"}, "sess:1", "bash")

	joined := strings.Join(args, " ")
	if joined != "respawn-pane -k -c /tmp/work -t sess:1 -- bash" {
		t.Errorf("respawn-pane args = %q", joined)
	}
}

// Every respawn-pane in the package has to go through the helper; one raw call
// left behind is one pane that silently comes back as a shell.
func TestEveryRespawnGoesThroughTheHelper(t *testing.T) {
	source, err := os.ReadFile("instance.go")
	if err != nil {
		t.Fatalf("reading instance.go: %v", err)
	}
	body := string(source)

	helper := strings.Index(body, "func respawnPaneArgs(")
	if helper < 0 {
		t.Fatal("respawnPaneArgs is gone")
	}
	helperEnd := strings.Index(body[helper:], "\n}\n")
	if helperEnd < 0 {
		t.Fatal("respawnPaneArgs is unbalanced")
	}

	for _, part := range []string{body[:helper], body[helper+helperEnd:]} {
		if strings.Contains(part, `"respawn-pane"`) {
			t.Error("a respawn-pane command is built outside respawnPaneArgs, so it " +
				"has no -- separator and psmux will start a shell instead")
		}
	}
}

func indexOf(list []string, want string) int {
	for i, item := range list {
		if item == want {
			return i
		}
	}
	return -1
}
