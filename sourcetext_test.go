package main

import (
	"os"
	"strings"
	"testing"
)

// readTextFile reads a Go file as text, with line endings normalised.
//
// Several tests here search a checked-in file — Go source, or a workflow
// YAML — for text on its own line — "\n}\n" to find where a
// function ends, or a statement on its own line. Git checks out CRLF on Windows
// by default, so the raw bytes there contain "\r\n" and every such search finds
// nothing: the test then reports the function as unterminated or the statement
// as missing, when the source is perfectly correct.
//
// Reading through this instead of os.ReadFile keeps those tests about the code
// rather than about the checkout.
func readTextFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}
