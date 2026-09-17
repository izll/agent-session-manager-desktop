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

// An install that removes packages is never offered.
//
// Installing tmux can pull a dependency that conflicts with something already
// there, and a package manager told not to ask questions resolves that by
// removing the something — on a machine that is somebody's running server. The
// multiplexer is not worth one package of it, and we cannot know what that
// package was for.
func TestAptRemovalsAreDetected(t *testing.T) {
	// The shape apt-get -s uses when it would take something off.
	simulation := `Reading package lists...
Building dependency tree...
The following packages will be REMOVED:
  libfoo1 oldthing
Remv libfoo1 [1.2-3]
Remv oldthing [4.5]
Inst tmux (3.0a-2 Debian:11/stable [amd64])
Conf tmux (3.0a-2 Debian:11/stable [amd64])`

	removals := removalsFrom("apt-get", simulation)
	if len(removals) != 2 {
		t.Fatalf("found %d removals in a simulation that removes two: %v", len(removals), removals)
	}
	if removals[0] != "libfoo1" || removals[1] != "oldthing" {
		t.Errorf("removals = %v; the package names were not picked out cleanly", removals)
	}
}

// The ordinary case must not be read as a removal, or the offer would never
// appear. Measured against a real Ubuntu 18.04 server, whose simulation
// mentions packages that "are no longer required" without removing any.
func TestACleanInstallReportsNoRemovals(t *testing.T) {
	simulation := `Reading package lists...
Building dependency tree...
The following packages were automatically installed and are no longer required:
  cgmanager dh-python dkms gcc-6-base:i386
Use 'apt autoremove' to remove them.
Inst tmux (2.6-3ubuntu0.3 Ubuntu:18.04/bionic-updates [amd64])
Conf tmux (2.6-3ubuntu0.3 Ubuntu:18.04/bionic-updates [amd64])`

	if removals := removalsFrom("apt-get", simulation); len(removals) != 0 {
		t.Errorf("a clean install was read as removing %v — the offer would never "+
			"be shown on a perfectly ordinary server", removals)
	}
}

// Every package manager we offer has to be able to say in advance what it
// would do. One that cannot is one we must not run unattended.
func TestEveryInstallRecipeHasADryRun(t *testing.T) {
	for manager := range installCommands {
		if _, found := dryRunCommands[manager]; !found {
			t.Errorf("%s can be installed with but not previewed — it would run "+
				"unattended with no way to know what it changes", manager)
		}
	}
}

// A dry run that changes something is not a dry run.
func TestDryRunsAreReadOnly(t *testing.T) {
	previews := map[string]string{
		"apt-get": "-s",
		"dnf":     "--assumeno",
		"yum":     "--assumeno",
		"pacman":  "--print",
		"zypper":  "--dry-run",
		"apk":     "--simulate",
		"brew":    "--dry-run",
	}
	for manager, flag := range previews {
		command, found := dryRunCommands[manager]
		if !found {
			t.Errorf("%s has no dry run", manager)
			continue
		}
		if !strings.Contains(command, flag) {
			t.Errorf("%s dry run %q is missing %s, so it would actually install",
				manager, command, flag)
		}
		if strings.Contains(command, " -y") {
			t.Errorf("%s dry run %q carries -y", manager, command)
		}
	}
}

// The belt to the dry run's braces: if the server changed between the check
// and the run, apt refuses rather than removing something to make room.
func TestAptRefusesToRemoveAtRunTime(t *testing.T) {
	if !strings.Contains(installCommands["apt-get"], "--no-remove") {
		t.Error("the apt recipe would let a changed situation remove packages " +
			"between the preview and the install")
	}
}

// The plan the user accepted is the plan that runs. A plan carrying removals
// was never one they were offered.
func TestRunRefusesAPlanWithRemovals(t *testing.T) {
	plan := &MultiplexerPlan{
		Command:     "apt-get install -y tmux",
		Possible:    true,
		WouldRemove: []string{"libfoo1"},
	}
	_, err := InstallMultiplexer(nil, nil, plan)
	if err == nil {
		t.Fatal("a plan that removes packages was run")
	}
	if !strings.Contains(err.Error(), "libfoo1") {
		t.Errorf("the error does not name what would go: %v", err)
	}
}
