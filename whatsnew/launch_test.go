package whatsnew

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func entriesFor(versions ...string) []Entry {
	out := make([]Entry, len(versions))
	for i, v := range versions {
		out[i] = Entry{Version: v, Date: "2026-09-01"}
	}
	return out
}

var releases = entriesFor("1.1.18", "1.1.17", "1.1.16", "1.1.15", "1.1.14", "1.1.13", "1.0.0")

func TestDecide(t *testing.T) {
	cases := []struct {
		name  string
		state State
		want  Launch
	}{
		{
			name:  "an update shows every release since the one seen",
			state: State{Current: "1.1.18", LastSeen: "1.1.14", PriorInstall: true},
			want:  Launch{Show: true, Since: "1.1.14", Current: "1.1.18", Versions: []string{"1.1.18", "1.1.17", "1.1.16", "1.1.15"}},
		},
		{
			name:  "a one-step update shows one release",
			state: State{Current: "1.1.18", LastSeen: "1.1.17", PriorInstall: true},
			want:  Launch{Show: true, Since: "1.1.17", Current: "1.1.18", Versions: []string{"1.1.18"}},
		},
		{
			name:  "a skipped release number is simply absent",
			state: State{Current: "1.1.13", LastSeen: "1.0.0", PriorInstall: true},
			want:  Launch{Show: true, Since: "1.0.0", Current: "1.1.13", Versions: []string{"1.1.13"}},
		},
		{
			name:  "a fresh install shows nothing and records the version",
			state: State{Current: "1.1.18"},
			want:  Launch{Current: "1.1.18", Versions: []string{}, Record: "1.1.18"},
		},
		{
			name:  "an install older than the feature shows the running version",
			state: State{Current: "1.1.18", PriorInstall: true},
			want:  Launch{Show: true, Current: "1.1.18", Versions: []string{"1.1.18"}},
		},
		{
			name:  "an unreadable record in an existing install reads as none",
			state: State{Current: "1.1.18", LastSeen: "garbage", PriorInstall: true},
			want:  Launch{Show: true, Current: "1.1.18", Versions: []string{"1.1.18"}},
		},
		{
			name:  "the same version shows nothing",
			state: State{Current: "1.1.18", LastSeen: "1.1.18", PriorInstall: true},
			want:  Launch{Current: "1.1.18", Versions: []string{}},
		},
		{
			name:  "a downgrade shows nothing and keeps the newer record",
			state: State{Current: "1.1.15", LastSeen: "1.1.18", PriorInstall: true},
			want:  Launch{Current: "1.1.15", Versions: []string{}},
		},
		{
			name:  "a development build shows and records nothing",
			state: State{Current: "1.1.18", LastSeen: "1.1.14", PriorInstall: true, Dev: true},
			want:  Launch{Current: "1.1.18"},
		},
		{
			name:  "a pre-release shows and records nothing",
			state: State{Current: "1.2.0-rc.1", LastSeen: "1.1.14", PriorInstall: true},
			want:  Launch{Current: "1.2.0-rc.1"},
		},
		{
			name:  "a version string that is not a release shows nothing",
			state: State{Current: "dev"},
			want:  Launch{Current: "dev"},
		},
		{
			name:  "an update to a version without notes records it quietly",
			state: State{Current: "1.1.19", LastSeen: "1.1.18", PriorInstall: true},
			want:  Launch{Since: "1.1.18", Current: "1.1.19", Versions: []string{}, Record: "1.1.19"},
		},
		{
			name:  "a tagged version works too",
			state: State{Current: "v1.1.18", LastSeen: "1.1.16", PriorInstall: true},
			want:  Launch{Show: true, Since: "1.1.16", Current: "v1.1.18", Versions: []string{"1.1.18", "1.1.17"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Decide(releases, c.state)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got  %#v\nwant %#v", got, c.want)
			}
		})
	}
}

func TestBetweenSortsNewestFirst(t *testing.T) {
	got := Between(entriesFor("1.0.0", "1.2.0", "Unreleased", "1.1.0"), "0.9.0", "1.2.0")
	if want := []string{"1.2.0", "1.1.0", "1.0.0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The record round trip: stored, read back, never moved backwards.
func TestSeenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got := LoadSeen(dir); got != "" {
		t.Fatalf("empty dir reads %q", got)
	}
	if err := MarkSeen(dir, "1.1.14"); err != nil {
		t.Fatal(err)
	}
	if got := LoadSeen(dir); got != "1.1.14" {
		t.Fatalf("read back %q", got)
	}
	if err := MarkSeen(dir, "1.1.18"); err != nil {
		t.Fatal(err)
	}
	if err := MarkSeen(dir, "1.1.15"); err != nil {
		t.Fatal(err)
	}
	if got := LoadSeen(dir); got != "1.1.18" {
		t.Fatalf("a downgrade moved the record to %q", got)
	}
	if err := MarkSeen(dir, "dev"); err == nil {
		t.Fatal("a non-release version was recorded")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestSeenFileIsBounded(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, 10_000)
	for i := range big {
		big[i] = '1'
	}
	if err := os.WriteFile(filepath.Join(dir, SeenFile), big, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadSeen(dir); got != "" {
		t.Fatalf("an oversized record read as %d bytes", len(got))
	}
}

func TestHadPriorInstall(t *testing.T) {
	dir := t.TempDir()
	// The log is written before anything asks: it proves nothing.
	if err := os.WriteFile(filepath.Join(dir, "asmgr-desktop.log"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if HadPriorInstall(dir) {
		t.Fatal("a directory with only the log counts as an earlier install")
	}
	if HadPriorInstall(filepath.Join(dir, "missing")) || HadPriorInstall("") {
		t.Fatal("a missing directory counts as an earlier install")
	}
	for _, marker := range []string{"sessions.json", "projects.json", "last_update_check"} {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, marker), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if !HadPriorInstall(d) {
			t.Errorf("%s does not count as an earlier install", marker)
		}
	}
}
