package session

import (
	"net/url"
	"path/filepath"
	"strings"
)

// readOnlySQLiteDSN builds the connection string for opening an agent's own
// database without being able to write to it.
//
// Every SQLite store this package reads belongs to a running agent — Cursor's
// chat store, Antigravity's conversation summaries, OpenCode's message
// history. They are opened to list past conversations, never to change one, and
// a write from here would corrupt work the user is in the middle of.
//
// The "file:" prefix is what makes that guarantee hold. The driver only parses
// query parameters when the DSN is a URI: given a bare path, "?mode=ro" is read
// as part of the filename, the database opens writable, and nothing reports it.
// Measured both ways against a real store — with the prefix an INSERT is
// refused, without it the INSERT succeeds.
//
// The path is escaped rather than concatenated, because a store under a
// directory with a space or an accent in its name would otherwise produce a
// malformed URI.
func readOnlySQLiteDSN(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))

	// A Windows path carries a drive letter, which has to follow the triple
	// slash: file:///C:/Users/... — without the leading slash the drive is
	// read as the URI's host.
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}

	escaped := (&url.URL{Path: clean}).EscapedPath()
	return "file://" + escaped + "?mode=ro"
}
