package session

import (
	"slices"
	"strings"
	"testing"
)

func TestAsmGrBindingsDoNotDependOnPOSIXShellOrTmuxBinaryName(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		command string
	}{
		{"root scroll", asmgrSessionBinding("root", "S-PageUp", "copy-mode -eu"), "copy-mode -eu"},
		{"copy scroll", asmgrSessionBinding("copy-mode-vi", "S-PageDown", "send-keys -X page-down"), "send-keys -X page-down"},
		{"detach", asmgrSessionBinding("", "C-q", "resize-window -x 100 -y 30 ; detach-client"), "resize-window -x 100 -y 30 ; detach-client"},
		{"yolo", asmgrSessionBinding("", "C-y", `run-shell "asmgr yolo \"#{session_name}\" \"#{window_index}\""`), `run-shell "asmgr yolo \"#{session_name}\" \"#{window_index}\""`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			joined := strings.Join(tt.args, " ")
			for _, forbidden := range []string{"tmux ", "grep", "/dev/null", "$("} {
				if strings.Contains(joined, forbidden) {
					t.Fatalf("binding still depends on %q: %q", forbidden, joined)
				}
			}
			if !slices.Contains(tt.args, "-F") || !slices.Contains(tt.args, "#{m/r:^asm_,#{session_name}}") {
				t.Fatalf("binding does not use the shell-free asmgr session condition: %#v", tt.args)
			}
			if !slices.Contains(tt.args, tt.command) {
				t.Fatalf("binding lost its native command %q: %#v", tt.command, tt.args)
			}
		})
	}
}

func TestAsmGrBindingUsesCorrectKeyScope(t *testing.T) {
	tableBinding := asmgrSessionBinding("root", "S-PageUp", "copy-mode -eu")
	if got := tableBinding[:4]; !slices.Equal(got, []string{"bind-key", "-T", "root", "S-PageUp"}) {
		t.Fatalf("table binding prefix = %#v", got)
	}

	globalBinding := asmgrSessionBinding("", "C-q", "detach-client")
	if got := globalBinding[:3]; !slices.Equal(got, []string{"bind-key", "-n", "C-q"}) {
		t.Fatalf("global binding prefix = %#v", got)
	}
}
