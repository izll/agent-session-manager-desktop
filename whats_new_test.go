package main

import (
	"os"
	"reflect"
	"testing"

	"asmgr-desktop/whatsnew"
)

// The binary carries the changelog of the tree it was built from.
func TestChangelogIsEmbedded(t *testing.T) {
	onDisk, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(changelogMarkdown) != string(onDisk) {
		t.Fatal("the embedded changelog is not CHANGELOG.md")
	}
	entries := (&App{}).GetChangelog()
	if len(entries) == 0 || entries[0].Version == "" {
		t.Fatal("GetChangelog returned nothing")
	}
}

// A launch sequence against a real directory: a fresh install records the
// version silently; the next release shows its notes until they are marked
// seen; after that, nothing.
func TestWhatsNewLaunchSequence(t *testing.T) {
	dir := t.TempDir()
	entries := changelogEntries()
	if len(entries) < 3 {
		t.Fatal("too few releases to test with")
	}
	older, newer := entries[2].Version, entries[0].Version

	first := whatsNewOnLaunch(dir, whatsnew.State{Current: older})
	if first.Show {
		t.Fatal("a fresh install showed the notes")
	}
	if got := whatsnew.LoadSeen(dir); got != older {
		t.Fatalf("a fresh install recorded %q, want %q", got, older)
	}

	updated := whatsNewOnLaunch(dir, whatsnew.State{Current: newer, PriorInstall: true})
	want := []string{entries[0].Version, entries[1].Version}
	if !updated.Show || !reflect.DeepEqual(updated.Versions, want) || updated.Since != older {
		t.Fatalf("after updating: %#v", updated)
	}
	// Not recorded until read: a launch cut short shows it again.
	if again := whatsNewOnLaunch(dir, whatsnew.State{Current: newer, PriorInstall: true}); !again.Show {
		t.Fatal("the notes were recorded as seen before anyone saw them")
	}

	if err := whatsnew.MarkSeen(dir, newer); err != nil {
		t.Fatal(err)
	}
	if after := whatsNewOnLaunch(dir, whatsnew.State{Current: newer, PriorInstall: true}); after.Show {
		t.Fatal("the notes showed again after being seen")
	}
}
