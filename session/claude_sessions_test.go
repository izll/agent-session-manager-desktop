package session

import (
	"path/filepath"
	"testing"
)

func TestPathWithinProjectUsesPathBoundaries(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "work", "repo")
	for _, tc := range []struct {
		name      string
		candidate string
		want      bool
	}{
		{name: "same", candidate: base, want: true},
		{name: "descendant", candidate: filepath.Join(base, "nested"), want: true},
		{name: "prefix sibling", candidate: base + "2", want: false},
		{name: "parent", candidate: filepath.Dir(base), want: false},
		{name: "empty", candidate: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathWithinProject(tc.candidate, base); got != tc.want {
				t.Fatalf("pathWithinProject(%q, %q) = %v, want %v", tc.candidate, base, got, tc.want)
			}
		})
	}
}
