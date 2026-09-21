package session

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Matching the project directory an agent recorded against the one a session
// holds.
//
// The two are written by different programs, and on Windows they disagree in
// ways the filesystem itself does not care about. Claude's history records
// "c:\Project\app" with a lowercase drive letter; Windows reports
// "C:\Project\app". A byte-for-byte comparison rejects the pair, and the
// resume list comes back empty for a project that plainly has conversations —
// which is what sends someone to typing --resume by hand.

// sameProjectPath reports whether two paths name the same directory.
//
// Case is ignored on Windows and macOS, whose filesystems are case-insensitive
// by default, and respected on Linux, where two names differing only in case
// are two different directories. Separators are normalised either way, since a
// path may arrive with whichever the writer happened to use.
func sameProjectPath(left, right string) bool {
	left = normaliseProjectPath(left)
	right = normaliseProjectPath(right)
	if left == "" || right == "" {
		return false
	}
	if caseInsensitiveFilesystem() {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// normaliseProjectPath puts a path into the form comparisons are made in:
// cleaned, with this platform's separator, and without a trailing one.
func normaliseProjectPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	// Both separators appear in recorded paths — an agent may write the one it
	// was given rather than the one the platform uses.
	trimmed = strings.ReplaceAll(trimmed, "\\", string(filepath.Separator))
	trimmed = strings.ReplaceAll(trimmed, "/", string(filepath.Separator))
	return filepath.Clean(trimmed)
}

// caseInsensitiveFilesystem reports whether path comparison should ignore case.
//
// By platform rather than by probing: a probe would need a writable directory
// and would answer for that one filesystem, while this decides how to compare
// two strings that may name directories on different volumes.
func caseInsensitiveFilesystem() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

// claudeProjectDirName is the directory Claude stores a project's transcripts
// in, derived from the project's path.
//
// Claude replaces every character that is not a letter or a digit with a
// hyphen — separators, dots, colons and all. Verified against real directories
// on both platforms:
//
//	/home/izll/App/ventoy-1.0.79  -> -home-izll-App-ventoy-1-0-79
//	c:\Project\.net\muszakidepoRA -> c--Project--net-muszakidepoRA
//
// Note what this means: the encoding is lossy, and cannot be reversed. A
// directory named "asmgr-desktop" and one named "asmgr/desktop" produce the
// same result, so decoding a directory name back into a path — which is what
// this code used to do — invents a path that may never have existed. Encoding
// the path we are looking for and comparing the names is exact.
func claudeProjectDirName(projectPath string) string {
	var builder strings.Builder
	builder.Grow(len(projectPath))
	for _, character := range projectPath {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9':
			builder.WriteRune(character)
		default:
			builder.WriteByte('-')
		}
	}
	return builder.String()
}

// claudeProjectDirMatches reports whether a directory in Claude's projects
// folder belongs to a project, or to something inside it.
//
// Compared as encoded names rather than as paths, because the encoding cannot
// be undone. A descendant's name begins with the project's own followed by a
// hyphen — the separator, encoded — which is how "/repo" is told from
// "/repo2": the latter encodes to "-repo2", which does not start with "-repo-".
func claudeProjectDirMatches(dirName, projectPath string) bool {
	wanted := claudeProjectDirName(projectPath)
	if wanted == "" || dirName == "" {
		return false
	}
	// Case-folded whenever the path looks like a Windows one, rather than
	// asking what this machine runs. Claude records "c:\Project" where Windows
	// reports "C:\Project", and a session file can be read on a different
	// machine than it was written on — an exported session, a shared home
	// directory — so the rule has to follow the DATA, not the reader.
	if caseInsensitiveFilesystem() || looksLikeWindowsPath(projectPath) {
		dirName = strings.ToLower(dirName)
		wanted = strings.ToLower(wanted)
	}
	return dirName == wanted || strings.HasPrefix(dirName, wanted+"-")
}

// looksLikeWindowsPath reports whether a path was written on Windows.
//
// A drive letter followed by a colon, which no Unix path has. Used to decide
// whether case matters, since that is a property of where the path came from
// rather than of the machine reading it.
func looksLikeWindowsPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if len(trimmed) < 2 || trimmed[1] != ':' {
		return false
	}
	letter := trimmed[0]
	return (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')
}
