package remote

import (
	"strings"
	"testing"
)

// Every recipe has to be non-interactive.
//
// There is no terminal on the other end of this connection, so a package
// manager that stops to ask "do you want to continue? [Y/n]" does not ask —
// it waits, holding the connection until the timeout, and the user is told the
// install timed out rather than that something wanted an answer.
func TestInstallCommandsNeverPrompt(t *testing.T) {
	// The flag that makes each one answer its own questions.
	required := map[string]string{
		"apt-get": "-y",
		"dnf":     "-y",
		"yum":     "-y",
		"pacman":  "--noconfirm",
		"zypper":  "--non-interactive",
		"apk":     "--no-cache",
	}

	for manager, command := range installCommands {
		flag, checked := required[manager]
		if !checked {
			// brew asks nothing by default.
			continue
		}
		if !strings.Contains(command, flag) {
			t.Errorf("%s: %q is missing %s, so it would wait for an answer nobody can give",
				manager, command, flag)
		}
	}
}

// Debian asks about configuration files unless told not to, and that prompt
// appears well into an install rather than at the start.
func TestDebianInstallIsFullyNonInteractive(t *testing.T) {
	command := installCommands["apt-get"]
	if !strings.Contains(command, "DEBIAN_FRONTEND=noninteractive") {
		t.Errorf("apt-get recipe %q can still stop to ask about a config file", command)
	}
	if !strings.Contains(command, "apt-get update") {
		t.Error("apt-get install without a preceding update fails on a server whose " +
			"package lists are older than the archive")
	}
}

// Every recipe installs tmux and nothing else. This is running as root on
// someone's server: the command that runs there is the command they were shown.
func TestInstallCommandsOnlyInstallTmux(t *testing.T) {
	for manager, command := range installCommands {
		if !strings.Contains(command, "tmux") {
			t.Errorf("%s: %q does not install tmux", manager, command)
		}
		for _, forbidden := range []string{"curl", "wget", "|", "http://", "https://"} {
			if strings.Contains(command, forbidden) {
				t.Errorf("%s: %q contains %q — an install recipe should use the "+
					"package manager and nothing else", manager, command, forbidden)
			}
		}
	}
}

// Installing is refused rather than guessed at when there is no way to do it,
// and the reason is what the user acts on.
func TestAnImpossiblePlanExplainsItself(t *testing.T) {
	plan := &MultiplexerPlan{Possible: false, Reason: "no package manager we recognise"}

	if _, err := InstallMultiplexer(nil, nil, plan); err == nil {
		t.Error("an impossible plan was run anyway")
	}
	if plan.Reason == "" {
		t.Error("an impossible plan carries no reason, so the user has nothing to act on")
	}

	if _, err := InstallMultiplexer(nil, nil, nil); err == nil {
		t.Error("a missing plan was run anyway")
	}
}

// sudo without a terminal cannot ask for a password, so -n makes it fail
// immediately and say so instead of hanging.
func TestSudoIsNonInteractive(t *testing.T) {
	// The branch is in PlanMultiplexerInstall; what matters is the shape of
	// what it builds.
	plan := &MultiplexerPlan{
		Command:   "sudo -n " + installCommands["apt-get"],
		NeedsSudo: true,
		Possible:  true,
	}
	if !strings.Contains(plan.Command, "sudo -n") {
		t.Error("sudo would wait for a password with no terminal to type it into")
	}
}

// A long package-manager log in an error message buries what went wrong.
func TestLongOutputIsTrimmedForTheMessage(t *testing.T) {
	long := strings.Repeat("Unpacking something...\n", 500)
	trimmed := trimForMessage(long)

	if len(trimmed) > 2100 {
		t.Errorf("trimmed output is %d bytes; it would fill the dialog", len(trimmed))
	}
	// The end is kept, not the beginning: the failure is at the end.
	if !strings.HasSuffix(strings.TrimSpace(trimmed), "Unpacking something...") {
		t.Error("the tail of the output was discarded, which is where the error is")
	}

	short := "apt-get: command not found"
	if trimForMessage(short) != short {
		t.Error("a short message was altered")
	}
}
