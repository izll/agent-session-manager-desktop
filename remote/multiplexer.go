package remote

import (
	"context"
	"fmt"
	"strings"
)

// Installing tmux on a server that has none.
//
// Without a multiplexer nothing here works: sessions live in it, and that is
// what lets an agent keep running after the app closes. So a server without
// one is not degraded, it is unusable — and since we are already connected and
// can see which distribution it runs, offering to install it beats telling the
// user to go and do it by hand.
//
// It is offered, never done quietly. This installs software on someone's
// machine, and the exact command is shown before anything runs.

// MultiplexerPlan is what would be done to install tmux on a server.
type MultiplexerPlan struct {
	// Command is the whole thing that would run, shown to the user first.
	Command string `json:"command"`
	// PackageManager names what was detected, so the user can tell at a glance
	// whether we understood their server.
	PackageManager string `json:"packageManager"`
	// NeedsSudo says the command is prefixed with sudo because the login is
	// not root.
	NeedsSudo bool `json:"needsSudo"`
	// Possible is false when the server runs something we have no recipe for,
	// or when the install would remove packages.
	Possible bool `json:"possible"`
	// Reason explains an impossible plan.
	Reason string `json:"reason,omitempty"`
	// WouldRemove lists packages the install would take off the server.
	//
	// Always empty in a plan that is Possible: a multiplexer is not worth one
	// package of somebody's server, and we cannot know what it was for. When
	// this is set the user is shown the list and told to sort it out by hand.
	WouldRemove []string `json:"wouldRemove,omitempty"`
	// Preview is what the package manager said it would do, shown as evidence
	// rather than asked to be trusted.
	Preview string `json:"preview,omitempty"`
}

// PlanMultiplexerInstall works out how tmux would be installed on this server.
//
// Nothing is installed here — this only looks.
func PlanMultiplexerInstall(ctx context.Context, client *Client) (*MultiplexerPlan, error) {
	// Which package manager, and whether we are root. One command, because
	// each round trip is a visible pause in a dialog.
	out, err := client.Run(ctx, strings.Join([]string{
		"id -u",
		"for m in apt-get dnf yum pacman zypper apk brew; do " +
			"command -v $m >/dev/null 2>&1 && echo $m; done",
		"command -v sudo >/dev/null 2>&1 && echo has-sudo",
	}, "; "))
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(string(out))
	isRoot := len(fields) > 0 && fields[0] == "0"
	hasSudo := false
	manager := ""
	for _, field := range fields[1:] {
		switch field {
		case "has-sudo":
			hasSudo = true
		default:
			if manager == "" {
				manager = field
			}
		}
	}

	plan := &MultiplexerPlan{PackageManager: manager}
	if manager == "" {
		plan.Reason = "no package manager we recognise (apt, dnf, yum, pacman, zypper, apk or brew)"
		return plan, nil
	}

	install, known := installCommands[manager]
	if !known {
		plan.Reason = fmt.Sprintf("no recipe for %s", manager)
		return plan, nil
	}

	switch {
	case isRoot:
		plan.Command = install
	case hasSudo:
		// Non-interactive: a sudo that stops to ask for a password would hang
		// with the prompt going nowhere, since there is no terminal attached.
		// Better to fail and say so.
		plan.Command = "sudo -n " + install
		plan.NeedsSudo = true
	default:
		plan.Reason = "this account is not root and sudo is not available"
		return plan, nil
	}

	// Ask the package manager what it would do, before agreeing to let it do
	// anything. An install can pull a dependency that conflicts with something
	// already there, and the resolution is to remove that something — quietly,
	// because we told it not to ask questions. On a server that is somebody's
	// running machine, not a scratch box.
	preview, removals, err := previewInstall(ctx, client, manager, plan.Command)
	if err != nil {
		plan.Reason = fmt.Sprintf("could not check what the install would do: %v", err)
		return plan, nil
	}
	plan.Preview = preview
	if len(removals) > 0 {
		plan.WouldRemove = removals
		plan.Reason = "installing tmux here would remove other packages"
		return plan, nil
	}

	plan.Possible = true
	return plan, nil
}

// previewInstall runs the package manager's dry run and reports what it would
// take off the machine.
func previewInstall(ctx context.Context, client *Client, manager, command string) (string, []string, error) {
	dryRun, supported := dryRunCommands[manager]
	if !supported {
		// No dry run means no way to know, and no way to know means no.
		return "", nil, fmt.Errorf("%s cannot say in advance what it would change", manager)
	}

	// The dry run mirrors the real command's environment, or it can answer a
	// different question from the one that will be asked.
	prefix := ""
	if strings.HasPrefix(command, "sudo -n ") {
		prefix = "sudo -n "
	}
	out, err := client.Run(ctx, prefix+dryRun)
	output := string(out)
	if err != nil {
		return output, nil, fmt.Errorf("%w: %s", err, trimForMessage(output))
	}
	return output, removalsFrom(manager, output), nil
}

// dryRunCommands ask each package manager what it would do, changing nothing.
var dryRunCommands = map[string]string{
	"apt-get": "DEBIAN_FRONTEND=noninteractive apt-get install -s tmux",
	"dnf":     "dnf install --assumeno tmux",
	"yum":     "yum install --assumeno tmux",
	"pacman":  "pacman -S --print tmux",
	"zypper":  "zypper --non-interactive install --dry-run tmux",
	"apk":     "apk add --simulate tmux",
	"brew":    "brew install --dry-run tmux",
}

// removalsFrom picks the packages a dry run says it would remove.
//
// Each package manager announces this differently, and the wording is what
// stands between a user and an unexpectedly missing package.
func removalsFrom(manager, output string) []string {
	var removals []string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch manager {
		case "apt-get":
			// "Remv package [version]" in the simulation output.
			if after, found := strings.CutPrefix(trimmed, "Remv "); found {
				removals = append(removals, firstField(after))
			}
		case "dnf", "yum":
			if strings.HasPrefix(trimmed, "Removing:") || strings.HasPrefix(trimmed, "Erasing:") {
				removals = append(removals, trimmed)
			}
		case "pacman":
			if strings.Contains(trimmed, "Packages to remove") {
				removals = append(removals, trimmed)
			}
		case "zypper":
			if strings.Contains(trimmed, "to remove") || strings.Contains(trimmed, "REMOVED") {
				removals = append(removals, trimmed)
			}
		case "apk":
			if strings.HasPrefix(trimmed, "(") && strings.Contains(trimmed, "Purging") {
				removals = append(removals, trimmed)
			}
		}
	}
	return removals
}

func firstField(value string) string {
	if fields := strings.Fields(value); len(fields) > 0 {
		return fields[0]
	}
	return value
}

// installCommands is one recipe per package manager.
//
// Non-interactive throughout: there is no terminal on the other end, so
// anything that asks a question hangs instead of asking it.
var installCommands = map[string]string{
	// --no-remove is the belt to the dry run's braces: if the situation on the
	// server changed between the check and the run, apt refuses rather than
	// removing something to make room.
	"apt-get": "DEBIAN_FRONTEND=noninteractive apt-get update && " +
		"DEBIAN_FRONTEND=noninteractive apt-get install -y --no-remove tmux",
	"dnf":    "dnf install -y tmux",
	"yum":    "yum install -y tmux",
	"pacman": "pacman -S --noconfirm tmux",
	"zypper": "zypper --non-interactive install tmux",
	"apk":    "apk add --no-cache tmux",
	// brew refuses to run as root, and the sudo branch above never applies to
	// it: a Mac server runs brew as the logged-in user.
	"brew": "brew install tmux",
}

// InstallMultiplexer runs the plan.
//
// The plan is passed back in rather than recomputed, so what runs is what the
// user was shown.
func InstallMultiplexer(ctx context.Context, client *Client, plan *MultiplexerPlan) (string, error) {
	if plan == nil || !plan.Possible || plan.Command == "" {
		return "", fmt.Errorf("there is no way to install tmux on this server automatically")
	}
	if len(plan.WouldRemove) > 0 {
		// Belongs to the plan the user accepted, and a plan that removes
		// packages is never one they were offered.
		return "", fmt.Errorf("this install would remove %s — refusing",
			strings.Join(plan.WouldRemove, ", "))
	}

	out, runErr := client.Run(ctx, plan.Command)
	output := string(out)

	// Whether it worked is decided by asking the server, not by the exit
	// status: a package manager can report success while installing nothing
	// useful, and can report failure over a warning.
	version, checkErr := client.Run(ctx, "tmux -V")
	if checkErr == nil && strings.Contains(string(version), "tmux") {
		return strings.TrimSpace(string(version)), nil
	}

	if runErr != nil {
		return "", fmt.Errorf("the install command failed: %w\n%s", runErr, trimForMessage(output))
	}
	if strings.Contains(output, "sudo: a password is required") ||
		strings.Contains(output, "sudo: no tty present") {
		return "", fmt.Errorf("sudo asked for a password, and there is no terminal here to type it into — " +
			"install tmux on the server yourself, or allow this command without a password")
	}
	return "", fmt.Errorf("tmux still is not there after the install:\n%s", trimForMessage(output))
}

// trimForMessage keeps an error readable when a package manager has been
// verbose.
func trimForMessage(output string) string {
	trimmed := strings.TrimSpace(output)
	const limit = 2000
	if len(trimmed) <= limit {
		return trimmed
	}
	return "…" + trimmed[len(trimmed)-limit:]
}
