package main

import (
	"os"
	"strings"
	"testing"
)

// Several tests here read a .go file and assert something about a particular
// function — that a call is present, that a flag is absent. They all have to
// agree on two things, and getting either wrong makes a guard stop guarding
// without failing.
//
// Line endings. git checks the repository out with CRLF on Windows, where the
// source then holds "\r\n}\r\n" and a search for "\n}\n" finds nothing. A test
// that cuts the function body only `if end > 0` then silently keeps the whole
// file as the body, so it passes on a call that lives in some other function
// entirely. One test cut with no such guard, and that is what failed the 1.1.7
// release build on Windows while passing everywhere else.

// readSourceFile returns a source file with its line endings normalised, so a
// pattern written with "\n" matches on Windows too.
func readSourceFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}
