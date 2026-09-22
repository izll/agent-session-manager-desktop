package session

import (
	"strings"
	"testing"
)

// Tests here that read a .go file and assert something about one function all
// have to agree on two things, and getting either wrong makes a guard stop
// guarding without failing.
//
// Line endings. git checks the repository out with CRLF on Windows, where the
// source holds "\r\n}\r\n" and a search for "\n}\n" finds nothing. A test that
// cuts the function body only `if end > 0` then silently keeps the whole file
// as the body, so it passes on a call that lives in some other function
// entirely.

// functionBody returns the body of the named function, from its declaration to
// the closing brace in the first column.
//
// It fails rather than returning the rest of the file: a guard that quietly
// widens to everything after the declaration is worse than no guard, because
// it still reports success.
func functionBody(t *testing.T, source, decl string) string {
	t.Helper()
	at := strings.Index(source, decl)
	if at < 0 {
		t.Fatalf("%s not found; if it was renamed, update this test", decl)
	}
	body := source[at:]
	end := strings.Index(body, "\n}\n")
	if end < 0 {
		t.Fatalf("could not find the end of %s", decl)
	}
	return body[:end]
}
