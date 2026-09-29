package whatsnew

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"asmgr-desktop/updater"
)

func readChangelog(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// The file that ships: every release parses, with a date, newest first, and
// no bullet goes missing or merges into its neighbour.
func TestTheRealChangelogParses(t *testing.T) {
	data := readChangelog(t)
	entries := Parse(data)
	if len(entries) < 50 {
		t.Fatalf("only %d entries parsed", len(entries))
	}
	for i, e := range entries {
		if !updater.IsRelease(e.Version) {
			t.Errorf("entry %d: %q is not a release version", i, e.Version)
		}
		if !isoDate.MatchString(e.Date) {
			t.Errorf("%s: date %q", e.Version, e.Date)
		}
		if len(e.Sections) == 0 && len(e.Intro) == 0 {
			t.Errorf("%s: nothing parsed under it", e.Version)
		}
		for _, s := range e.Sections {
			if s.Kind == "" {
				t.Errorf("%s: a list with no section heading", e.Version)
			}
			for _, item := range s.Items {
				if strings.Contains(item, "\n") || strings.HasPrefix(item, "- ") {
					t.Errorf("%s: item not joined into one line: %q", e.Version, item)
				}
			}
		}
		if i > 0 {
			if cmp, _ := updater.CompareReleases(e.Version, entries[i-1].Version); cmp >= 0 {
				t.Errorf("%s is listed after %s", e.Version, entries[i-1].Version)
			}
		}
	}

	bullets := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "- ") {
			bullets++
		}
	}
	items := 0
	for _, e := range entries {
		for _, s := range e.Sections {
			items += len(s.Items)
		}
	}
	if items != bullets {
		t.Errorf("%d items parsed from %d bullets", items, bullets)
	}

	// The running version has notes: a release without them would show an
	// empty dialog, or none.
	if entries[0].Version == "" {
		t.Fatal("no newest entry")
	}
}

func TestTheRunningVersionHasNotes(t *testing.T) {
	src, err := os.ReadFile("../version.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`var Version = "([^"]+)"`).FindSubmatch(src)
	if m == nil {
		t.Fatal("no Version in version.go")
	}
	if !hasVersion(Parse(readChangelog(t)), string(m[1])) {
		t.Errorf("CHANGELOG.md has no entry for %s", m[1])
	}
}

const sample = `# Changelog

Intro text that is not a release.

## [Unreleased]

- Not released, not shown.

## 1.2.0 — 2026-10-01

A paragraph before the first section,
wrapped over two lines.

### Added

- **Bold lead.** Wrapped
  onto a second line with ` + "`code`" + `.
- Second item
  - that starts with a dash when wrapped.

  A second paragraph of the second item.

Text after the list.

### Security

- A kind the UI may not know.

## [1.1.0] - 2026-09-01

### Fixed

* Star bullets.

[1.1.0]: https://example.com/compare
`

func TestParseTheSubset(t *testing.T) {
	want := []Entry{
		{
			Version: "1.2.0", Date: "2026-10-01",
			Intro: []string{"A paragraph before the first section, wrapped over two lines."},
			Sections: []Section{
				{
					Kind: "added", Title: "Added",
					Intro: []string{"Text after the list."},
					Items: []string{
						"**Bold lead.** Wrapped onto a second line with `code`.",
						"Second item - that starts with a dash when wrapped. A second paragraph of the second item.",
					},
				},
				{Kind: "security", Title: "Security", Intro: []string{}, Items: []string{"A kind the UI may not know."}},
			},
		},
		{
			Version: "1.1.0", Date: "2026-09-01", Intro: []string{},
			Sections: []Section{{Kind: "fixed", Title: "Fixed", Intro: []string{}, Items: []string{"Star bullets."}}},
		},
	}
	got := Parse([]byte(sample))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

// A Windows checkout may turn the file's line endings into CRLF before it is
// embedded; the notes must read the same.
func TestParseCRLF(t *testing.T) {
	lf := Parse(readChangelog(t))
	crlf := Parse([]byte(strings.ReplaceAll(string(readChangelog(t)), "\n", "\r\n")))
	if !reflect.DeepEqual(lf, crlf) {
		t.Fatal("CRLF line endings parse differently")
	}
	// Bare carriage returns too, which an editor can leave behind.
	cr := Parse([]byte(strings.ReplaceAll(string(readChangelog(t)), "\n", "\r")))
	if !reflect.DeepEqual(lf, cr) {
		t.Fatal("CR line endings parse differently")
	}
	for _, e := range crlf {
		for _, s := range e.Sections {
			for _, item := range s.Items {
				if strings.Contains(item, "\r") {
					t.Fatalf("%s: carriage return left in %q", e.Version, item)
				}
			}
		}
	}
}

func TestParseNeverFails(t *testing.T) {
	for _, input := range []string{"", "#", "##", "### Added\n- orphan", "## not a version\n- x", "## 1.0.0\n- no date"} {
		entries := Parse([]byte(input))
		for _, e := range entries {
			if e.Version == "" {
				t.Errorf("%q: entry without a version", input)
			}
		}
	}
	if got := Parse([]byte("## 1.0.0\n- no date")); len(got) != 1 || got[0].Date != "" || got[0].Sections[0].Items[0] != "no date" {
		t.Errorf("an undated heading: %#v", got)
	}
}
