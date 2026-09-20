package main

import "testing"

// A name typed into the picker is a name, not a path. Anything that could
// reach outside the directory on screen is refused rather than cleaned up: a
// name that quietly becomes a different name is worse than one that is turned
// down with a reason.
func TestANewFolderNameCannotLeaveTheDirectoryOnScreen(t *testing.T) {
	refused := []string{
		"",
		"   ",
		"..",
		".",
		"../escape",
		"nested/name",
		`windows\name`,
		"/absolute",
		"-rf", // mkdir would read this as an option, not a name
	}
	for _, name := range refused {
		if got, err := validateNewDirectoryName(name); err == nil {
			t.Errorf("%q was accepted as %q", name, got)
		}
	}
}

func TestOrdinaryFolderNamesAreAccepted(t *testing.T) {
	accepted := map[string]string{
		"project":      "project",
		"  spaced  ":   "spaced",
		"with space":   "with space",
		"dot.in.name":  "dot.in.name",
		".hidden":      ".hidden",
		"ékezetes-név": "ékezetes-név",
		"name-with-−":  "name-with-−", // a dash inside is fine; only a leading one is not
	}
	for name, want := range accepted {
		got, err := validateNewDirectoryName(name)
		if err != nil {
			t.Errorf("%q was refused: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("%q became %q, want %q", name, got, want)
		}
	}
}

// The name reaches a shell inside a script, so it is quoted. A single quote in
// the name is the case that breaks naive quoting.
func TestAQuoteInTheNameStaysInsideTheName(t *testing.T) {
	quoted := shellQuoteForServer("it's here")
	if quoted != `'it'\''s here'` {
		t.Errorf("quoting produced %s", quoted)
	}
}
