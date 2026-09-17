package remote

import (
	"strings"
	"testing"

	"asmgr-desktop/remote/protocol"
)

// The version line is what the installer reads to decide whether to upload two
// megabytes over SSH, so it has to be parsed exactly — and a server with no
// helper has to come back as "none" rather than as a parse failure.
func TestParseHelperVersion(t *testing.T) {
	cases := []struct {
		name     string
		output   string
		present  bool
		protocol int
		build    string
		arch     string
	}{
		{
			name:     "a helper answers",
			output:   "asmgrd protocol=1 build=1.0.4 arch=linux/amd64\n",
			present:  true,
			protocol: 1,
			build:    "1.0.4",
			arch:     "linux/amd64",
		},
		{
			name:    "no helper installed",
			output:  "absent\n",
			present: false,
		},
		{
			name:    "nothing came back",
			output:  "",
			present: false,
		},
		{
			// A login shell printing its own message before the answer, which
			// is ordinary on a server with a chatty .profile.
			name:    "a shell message instead of a version",
			output:  "mesg: ttyname failed\n",
			present: false,
		},
		{
			// An older helper is not usable, but it is installed — and saying
			// so is what lets the app replace it rather than report an error.
			name:     "an older protocol",
			output:   "asmgrd protocol=0 build=old arch=linux/amd64\n",
			present:  true,
			protocol: 0,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseHelperVersion(testCase.output)
			if got.Present != testCase.present {
				t.Errorf("present = %v, want %v", got.Present, testCase.present)
			}
			if testCase.present {
				if got.Protocol != testCase.protocol {
					t.Errorf("protocol = %d, want %d", got.Protocol, testCase.protocol)
				}
				if testCase.build != "" && got.Build != testCase.build {
					t.Errorf("build = %q, want %q", got.Build, testCase.build)
				}
				if testCase.arch != "" && got.Arch != testCase.arch {
					t.Errorf("arch = %q, want %q", got.Arch, testCase.arch)
				}
			}
		})
	}
}

// An exact match, not "at least". An app talking to a newer helper is as wrong
// as the other way round, and a mismatch tolerated once is a mismatch nobody
// notices afterwards.
func TestUpToDateRequiresAnExactMatch(t *testing.T) {
	current := &InstalledHelper{Present: true, Protocol: protocol.Version}
	if !current.UpToDate() {
		t.Error("the current protocol version was rejected")
	}

	older := &InstalledHelper{Present: true, Protocol: protocol.Version - 1}
	if older.UpToDate() {
		t.Error("an older helper was accepted")
	}

	newer := &InstalledHelper{Present: true, Protocol: protocol.Version + 1}
	if newer.UpToDate() {
		t.Error("a newer helper was accepted; the app cannot know what it added")
	}

	absent := &InstalledHelper{}
	if absent.UpToDate() {
		t.Error("a server with no helper was reported as up to date")
	}
}

// The helper lives in the user's own home directory. It needs no privileges,
// and a path anywhere shared would invite something else to run it.
func TestHelperPathIsPerUserAndHidden(t *testing.T) {
	if !strings.HasPrefix(HelperPath, ".") {
		t.Errorf("helper path %q is not hidden", HelperPath)
	}
	if strings.HasPrefix(HelperPath, "/") {
		t.Errorf("helper path %q is absolute; it must be relative to the home directory",
			HelperPath)
	}
	if strings.Contains(HelperPath, "..") {
		t.Errorf("helper path %q escapes the home directory", HelperPath)
	}
}

// The protocol version is deliberately separate from the application version:
// tying them would re-upload the binary over SSH for every patch release.
func TestProtocolVersionIsItsOwnNumber(t *testing.T) {
	if protocol.Version < 1 {
		t.Errorf("protocol version = %d; it is what both ends agree on and must be set",
			protocol.Version)
	}
}
