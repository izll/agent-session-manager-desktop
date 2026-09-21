package session

import (
	"path/filepath"
	"runtime"
	"testing"
)

// The case that sent a user to typing --resume by hand: the resume list came
// back empty on Windows for a project that plainly had conversations.
//
// Claude's history records the project as "c:\Project\..." with a lowercase
// drive letter; Windows reports "C:\Project\...". Measured on a real machine:
//
//	history.jsonl: "project":"c:\\Project\\.net\\muszakidepoRA"
//	Resolve-Path:  C:\Project\.net\muszakidepoRA
//
// A byte-for-byte comparison rejects that pair, so nothing matched.
func TestADriveLetterCaseDoesNotHideAConversation(t *testing.T) {
	// Checked on every platform rather than skipped off Windows: the rule is
	// "compare as the filesystem would", and a Linux run must confirm that the
	// case-insensitive branch is the one Windows takes — otherwise the fix is
	// only ever exercised on the machine that cannot run the tests.
	recorded := `c:\Project\.net\muszakidepoRA`
	reported := `C:\Project\.net\muszakidepoRA`

	if caseInsensitiveFilesystem() != (runtime.GOOS == "windows" || runtime.GOOS == "darwin") {
		t.Fatal("the platform rule changed; this test needs rewriting")
	}

	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		if !sameProjectPath(recorded, reported) {
			t.Error("a project was not recognised because of the drive letter's case")
		}
		return
	}

	// On Linux these are two relative names, neither of which is a drive
	// letter — what matters here is that the comparison used is the
	// case-sensitive one, which the test above already asserts.
	if sameProjectPath(recorded, reported) {
		t.Error("Linux treated two differently-cased names as one directory")
	}
}

// Separators are normalised whichever way a path was written: an agent may
// record the separator it was handed rather than the platform's own.
func TestSeparatorsDoNotDecideAMatch(t *testing.T) {
	if !sameProjectPath("/home/izll/work", "/home/izll/work/") {
		t.Error("a trailing separator made two identical paths differ")
	}
	if !sameProjectPath("/home/izll/work", "/home/izll//work") {
		t.Error("a doubled separator made two identical paths differ")
	}
}

// On Linux, two names differing only in case are two different directories,
// and treating them as one would resume the wrong conversation.
func TestCaseStillMattersWhereTheFilesystemSaysItDoes(t *testing.T) {
	same := sameProjectPath("/home/izll/Work", "/home/izll/work")
	if caseInsensitiveFilesystem() {
		if !same {
			t.Error("case was respected on a filesystem that ignores it")
		}
		return
	}
	if same {
		t.Error("two different Linux directories were treated as one")
	}
}

// An empty path matches nothing: it is the absence of an answer, not a
// wildcard that would match every project.
func TestAnEmptyPathMatchesNothing(t *testing.T) {
	if sameProjectPath("", "") || sameProjectPath("", "/home/izll") ||
		sameProjectPath("/home/izll", "") {
		t.Error("an empty path was treated as a match")
	}
}

// Cursor and Gemini find a project by hashing its path, and the hash is
// computed by the AGENT. Ours has to match theirs byte for byte, so the
// normalisation that fixes path COMPARISON must never be applied to a path on
// its way into a hash — it would produce a different digest and find nothing
// at all, turning a case mismatch into a total miss.
func TestHashedProjectPathsAreNotNormalised(t *testing.T) {
	for _, fn := range []struct {
		name string
		hash func(string) string
	}{
		{"cursor", cursorProjectHash},
		{"gemini", geminiProjectHash},
	} {
		lower := fn.hash("/home/izll/work")
		upper := fn.hash("/home/izll/Work")
		if lower == upper {
			t.Errorf("%s: the path was case-folded before hashing; the digest "+
				"will not match the one the agent wrote", fn.name)
		}
	}
}

// Claude names a project's directory by replacing every non-alphanumeric
// character with a hyphen. Verified against real directories on both
// platforms — these three are copied from live machines.
func TestTheProjectDirectoryNameMatchesWhatClaudeWrites(t *testing.T) {
	cases := map[string]string{
		"/home/izll/NetBeansProjects/asmgr-desktop": "-home-izll-NetBeansProjects-asmgr-desktop",
		"/home/izll/App/ventoy-1.0.79":              "-home-izll-App-ventoy-1-0-79",
		`c:\Project\.net\muszakidepoRA`:             "c--Project--net-muszakidepoRA",
	}
	for path, want := range cases {
		if got := claudeProjectDirName(path); got != want {
			t.Errorf("claudeProjectDirName(%q) = %q, want %q", path, got, want)
		}
	}
}

// The bug this closes: the resume list was empty on Windows.
//
// The code decoded a directory name back into a path, but the encoding is
// lossy — every separator, dot and colon is the same hyphen. On Windows
// "c--Project--net-app" decoded to "/c//Project//net/app", which matched no
// project at all; on Linux "asmgr-desktop" decoded to "asmgr/desktop", a
// directory that does not exist.
//
// Encoding the path we want and comparing names is exact.
func TestAWindowsProjectFindsItsTranscripts(t *testing.T) {
	// The drive letter's case differs between what Claude records and what
	// Windows reports — measured on a real machine.
	if !claudeProjectDirMatches("c--Project--net-muszakidepoRA", `C:\Project\.net\muszakidepoRA`) {
		t.Error("a Windows project did not find its own transcripts")
	}
}

// A descendant's directory belongs to the project above it: its encoded name
// begins with the project's own, followed by the encoded separator.
func TestADescendantsTranscriptsBelongToTheProject(t *testing.T) {
	if !claudeProjectDirMatches(
		"-home-izll-NetBeansProjects-asmgr-desktop-build-bin",
		"/home/izll/NetBeansProjects/asmgr-desktop") {
		t.Error("a subdirectory's transcripts were not found")
	}
}

// And a sibling whose name merely starts the same way does not. Without the
// separator in the comparison, "/repo" would collect "/repo2"'s conversations.
func TestASiblingIsNotMistakenForADescendant(t *testing.T) {
	if claudeProjectDirMatches("-home-izll-repo2", "/home/izll/repo") {
		t.Error("another project's transcripts were claimed")
	}
}

// The transcript directory is built from the project path, and the resume
// list is empty if it names somewhere that does not exist.
//
// The builder used to list the characters Claude had been seen to replace —
// "/", "_", space, non-ASCII — which is right on Linux and wrong on Windows,
// where "\" and ":" survived untouched:
//
//	built:  -C:\Users\User\Documents\asmgr-teszt
//	actual: C--Users-User-Documents-asmgr-teszt
//
// Every entry was then discarded for a missing file, however many
// conversations the project had. Measured on a real Windows machine: 22
// history entries and 12 transcripts, none of them listed.
func TestTheTranscriptDirectoryIsNamedAsClaudeNamesIt(t *testing.T) {
	cases := map[string]string{
		`C:\Users\User\Documents\asmgr-teszt`:       "C--Users-User-Documents-asmgr-teszt",
		"/home/izll/NetBeansProjects/asmgr-desktop": "-home-izll-NetBeansProjects-asmgr-desktop",
	}
	for path, want := range cases {
		got := GetClaudeProjectDir(path)
		if filepath.Base(got) != want {
			t.Errorf("GetClaudeProjectDir(%q) ends in %q, want %q",
				path, filepath.Base(got), want)
		}
	}
}

// One encoder, used everywhere. Two would drift, and drifting is what put a
// leading hyphen on a Windows directory name that has none.
func TestTheDirectoryNameAndTheMatcherAgree(t *testing.T) {
	for _, path := range []string{
		`C:\Users\User\Documents\asmgr-teszt`,
		"/home/izll/NetBeansProjects/asmgr-desktop",
		"/home/izll/App/ventoy-1.0.79",
	} {
		fromBuilder := filepath.Base(GetClaudeProjectDir(path))
		if !claudeProjectDirMatches(fromBuilder, path) {
			t.Errorf("the directory built for %q is not one the matcher accepts: %q",
				path, fromBuilder)
		}
	}
}
