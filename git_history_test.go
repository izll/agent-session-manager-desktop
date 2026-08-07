package main

import "testing"

// A commit message is free text: it can contain newlines, tabs, quotes and
// anything else a person can type. Parsing the log by splitting on a character
// an author might have used is a bug that only shows up on somebody else's
// history, which is why the fields are separated by control characters that
// cannot appear in them.
func TestParsingACommitWithAwkwardText(t *testing.T) {
	log := "abc123" + gitFieldSep + "abc123" + gitFieldSep +
		"fix: handle a, b and \"c\"" + gitFieldSep +
		"A Person" + gitFieldSep + "person@example.com" + gitFieldSep +
		"2026-08-07T10:00:00+02:00" + gitFieldSep +
		"HEAD -> main, origin/main" + gitFieldSep +
		"parent1 parent2" + gitFieldSep +
		"A body\nover several lines\n\nwith a blank one." + gitRecordSep

	commits := parseGitLog(log)

	if len(commits) != 1 {
		t.Fatalf("parsed %d commits, want 1", len(commits))
	}
	c := commits[0]
	if c.Subject != `fix: handle a, b and "c"` {
		t.Errorf("Subject = %q; commas and quotes must survive", c.Subject)
	}
	if c.Body != "A body\nover several lines\n\nwith a blank one." {
		t.Errorf("Body = %q; a multi-line body must survive whole", c.Body)
	}
	if c.Author != "A Person" || c.Email != "person@example.com" {
		t.Errorf("author = %q <%q>", c.Author, c.Email)
	}
	if len(c.Parents) != 2 {
		t.Errorf("Parents = %v, want two: that is what makes this a merge", c.Parents)
	}
}

// git decorates refs with its own notation: "HEAD -> main" names the branch
// HEAD is on, and a tag is prefixed. Neither is part of the name.
func TestRefDecorationIsStrippedToNames(t *testing.T) {
	cases := []struct {
		name       string
		decoration string
		want       []string
	}{
		{"nothing", "", nil},
		{"head arrow", "HEAD -> main", []string{"main"}},
		{"several", "HEAD -> main, origin/main", []string{"main", "origin/main"}},
		{"tag", "tag: v1.0.0", []string{"v1.0.0"}},
		{"mixed", "HEAD -> dev, tag: v2, origin/dev", []string{"dev", "v2", "origin/dev"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGitRefs(tc.decoration)
			if len(got) != len(tc.want) {
				t.Fatalf("parsed %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("ref %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// An empty repository, a filtered log that matched nothing, or a trailing
// separator must not produce a phantom commit.
func TestEmptyAndPartialOutputProduceNoCommits(t *testing.T) {
	for _, output := range []string{
		"",
		"\n",
		gitRecordSep,
		// Too few fields to be a commit — a truncated read rather than a record.
		"abc" + gitFieldSep + "abc" + gitRecordSep,
		// A record with no hash names no commit.
		gitFieldSep + "abc" + gitFieldSep + "s" + gitFieldSep + "a" + gitFieldSep +
			"e" + gitFieldSep + "d" + gitFieldSep + "" + gitFieldSep + "" + gitRecordSep,
	} {
		if commits := parseGitLog(output); len(commits) != 0 {
			t.Errorf("parsed %d commits from %q, want none", len(commits), output)
		}
	}
}

// Several commits arrive in one read, and the newline git puts between records
// must not become part of the next hash.
func TestConsecutiveCommitsAreSeparated(t *testing.T) {
	one := func(hash, subject string) string {
		return hash + gitFieldSep + hash[:3] + gitFieldSep + subject + gitFieldSep +
			"A" + gitFieldSep + "a@b" + gitFieldSep + "2026-08-07T10:00:00+02:00" +
			gitFieldSep + "" + gitFieldSep + "" + gitRecordSep
	}
	// git separates records with a newline, which lands at the front of the next.
	commits := parseGitLog(one("aaa111", "first") + "\n" + one("bbb222", "second"))

	if len(commits) != 2 {
		t.Fatalf("parsed %d commits, want 2", len(commits))
	}
	if commits[0].Hash != "aaa111" || commits[1].Hash != "bbb222" {
		t.Errorf("hashes = %q, %q; the record separator's newline leaked in",
			commits[0].Hash, commits[1].Hash)
	}
	if commits[1].Subject != "second" {
		t.Errorf("second subject = %q", commits[1].Subject)
	}
}
