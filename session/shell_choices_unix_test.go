//go:build !windows

package session

import (
	"path/filepath"
	"strings"
	"testing"
)

// The first entry is what "leave it alone" means, and the interface labels it
// itself — a hardcoded English label here would not translate.
func TestSystemDefaultIsOfferedFirst(t *testing.T) {
	choices := ShellChoices()

	if len(choices) == 0 {
		t.Fatal("no choices offered at all")
	}
	if choices[0].Command != "" {
		t.Errorf("first choice is %q, want the system default (empty)", choices[0].Command)
	}
	if choices[0].Label != "" {
		t.Errorf("first choice carries label %q; the frontend supplies a translated one", choices[0].Label)
	}
}

// /etc/shells lists what chsh accepts, which is not the same as what makes a
// usable tab: a multiplexer started inside a multiplexer pane is not a shell,
// and the restricted variants exist for locked-down accounts.
func TestMultiplexersAndRestrictedShellsAreNotOffered(t *testing.T) {
	for _, choice := range ShellChoices() {
		if choice.Command == "" {
			continue
		}
		name := filepath.Base(choice.Command)
		if nonInteractiveShells[name] {
			t.Errorf("%q was offered as a shell", choice.Command)
		}
	}
}

// /etc/shells lists the same shell under several paths (/bin/bash and
// /usr/bin/bash). Offering both looks like a choice between two things when it
// is one thing twice.
func TestEachShellIsOfferedOnce(t *testing.T) {
	seen := map[string]string{}
	for _, choice := range ShellChoices() {
		if choice.Command == "" {
			continue
		}
		name := filepath.Base(choice.Command)
		if previous, duplicate := seen[name]; duplicate {
			t.Errorf("%s offered twice: %q and %q", name, previous, choice.Command)
		}
		seen[name] = choice.Command
	}
}

// Every offered command has to be runnable: it is passed to respawn-pane, and
// a path that is not there produces a pane that dies on arrival.
func TestOfferedShellsAreAbsolutePaths(t *testing.T) {
	for _, choice := range ShellChoices() {
		if choice.Command == "" {
			continue
		}
		if !filepath.IsAbs(choice.Command) {
			t.Errorf("%q is not an absolute path", choice.Command)
		}
		if strings.TrimSpace(choice.Label) == "" {
			t.Errorf("%q has no label to show", choice.Command)
		}
	}
}
