package main

import "testing"

func TestDiffHiddenRepoKey(t *testing.T) {
	cases := []struct{ server, top, want string }{
		{"", "/home/me/repo", "/home/me/repo"},
		// The same path on a server is a different checkout.
		{"build-box", "/home/me/repo", "build-box:/home/me/repo"},
		// Not a repository: nothing to key on.
		{"", "", ""},
		{"build-box", "", ""},
	}
	for _, c := range cases {
		if got := diffHiddenRepoKey(c.server, c.top); got != c.want {
			t.Errorf("diffHiddenRepoKey(%q, %q) = %q, want %q", c.server, c.top, got, c.want)
		}
	}
}
