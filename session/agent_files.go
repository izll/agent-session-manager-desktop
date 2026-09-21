package session

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Reading an agent's own records — its history file, its stored transcripts —
// on whichever machine the agent runs on.
//
// These live under the user's home directory, not under the project, so the
// session's working directory says nothing about where to find them. A tab
// running on a server has its conversations on that server; listing the local
// machine's would offer the user conversations they cannot resume.
//
// The interface is deliberately tiny — read a file, ask whether one is there —
// so the remote implementation is a couple of shell commands rather than a
// filesystem.

// agentFiles reads an agent's records from one machine.
type agentFiles interface {
	// home is the home directory the agent's records live under.
	home() (string, error)
	// readFile returns a file's contents.
	readFile(path string) ([]byte, error)
	// readHead returns at most the first n bytes of a file.
	//
	// Separate from readFile because several agents keep what this needs — a
	// working directory, a title — at the top of a file whose whole length is
	// beside the point. One store measured 24 GB across 862 files while the
	// field that decides whether a file is relevant at all sat in the first
	// 22 KB of each.
	readHead(path string, n int64) ([]byte, error)
	// exists reports whether a path is there at all.
	exists(path string) bool
	// entries lists a directory: the names directly inside it, and for each
	// whether it is a directory. A path that is not there is not an error —
	// an agent that never ran on this machine has nothing stored.
	entries(dir string) ([]agentDirEntry, error)
	// findFiles returns the paths under a directory whose name ends in suffix,
	// newest first, at most limit of them.
	//
	// Newest first is what makes this affordable: the listings show recent
	// conversations, so the far side can stop early rather than hand back a
	// store that has grown for years.
	findFiles(dir, suffix string, limit int) ([]string, error)
	// findFilesByName is findFiles ordered by path, descending.
	//
	// For a store whose layout encodes the date — a tree of YYYY/MM/DD holding
	// files named after their timestamp — this is the more truthful order.
	// Modification times do not survive a copy: one measured store had been
	// moved, leaving 862 files stamped within the same minute, and taking the
	// newest 200 by mtime found 1 of a project's sessions where the name order
	// found 17.
	findFilesByName(dir, suffix string, limit int) ([]string, error)
	// join builds a path using the separator of the machine being read, which
	// is not necessarily this one's.
	join(parts ...string) string
}

// localFiles reads this computer's disk, which is what every listing did
// before servers existed.
type localFiles struct{}

func (localFiles) home() (string, error) { return os.UserHomeDir() }

func (localFiles) readFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (localFiles) readHead(path string, n int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, n))
}

func (localFiles) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (localFiles) entries(dir string) ([]agentDirEntry, error) {
	found, err := readDirAtMost(dir, maxDiscoveryDirectoryEntries)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	listed := make([]agentDirEntry, 0, len(found))
	for _, entry := range found {
		listed = append(listed, agentDirEntry{Name: entry.Name(), IsDir: entry.IsDir()})
	}
	return listed, nil
}

func (localFiles) findFilesByName(dir, suffix string, limit int) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			return nil
		}
		matches = append(matches, path)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func (localFiles) findFiles(dir, suffix string, limit int) ([]string, error) {
	type found struct {
		path     string
		modified time.Time
	}
	var matches []found
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		matches = append(matches, found{path: path, modified: info.ModTime()})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Slice(matches, func(a, b int) bool {
		return matches[a].modified.After(matches[b].modified)
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		paths = append(paths, match.path)
	}
	return paths, nil
}

func (localFiles) join(parts ...string) string { return filepath.Join(parts...) }

// remoteFiles reads a server's disk over an existing connection.
//
// Always Unix: the servers this connects to run a shell, so the separator is
// '/' regardless of what this computer uses. Using filepath.Join here would
// build backslash paths from a Windows desktop and find nothing.
type remoteFiles struct {
	shell ShellExecutor
}

func (r remoteFiles) join(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			cleaned = append(cleaned, strings.TrimSuffix(part, "/"))
		}
	}
	return strings.Join(cleaned, "/")
}

func (r remoteFiles) run(dir string, args ...string) ([]byte, []byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
	defer cancel()
	return r.shell.RunShell(ctx, dir, args...)
}

func (r remoteFiles) home() (string, error) {
	stdout, stderr, exitCode, err := r.run("", "printenv", "HOME")
	if err != nil {
		return "", err
	}
	home := strings.TrimSpace(string(stdout))
	if exitCode != 0 || home == "" {
		return "", fmt.Errorf("could not find the home directory on %s: %s",
			r.shell.Describe(), strings.TrimSpace(string(stderr)))
	}
	return home, nil
}

func (r remoteFiles) readFile(path string) ([]byte, error) {
	// Bounded rather than cat: a transcript is a conversation, but a file in
	// that directory could be anything, and pulling it whole across the
	// network is a cost paid before anyone can decide it was a mistake.
	stdout, stderr, exitCode, err := r.run("",
		"head", "-c", fmt.Sprintf("%d", maxAgentFileBytes), "--", path)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("could not read %s on %s: %s",
			path, r.shell.Describe(), strings.TrimSpace(string(stderr)))
	}
	return stdout, nil
}

func (r remoteFiles) readHead(path string, n int64) ([]byte, error) {
	stdout, stderr, exitCode, err := r.run("",
		"head", "-c", fmt.Sprintf("%d", n), "--", path)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("could not read %s on %s: %s",
			path, r.shell.Describe(), strings.TrimSpace(string(stderr)))
	}
	return stdout, nil
}

func (r remoteFiles) exists(path string) bool {
	_, _, exitCode, err := r.run("", "test", "-e", path)
	return err == nil && exitCode == 0
}

func (r remoteFiles) entries(dir string) ([]agentDirEntry, error) {
	// A trailing slash on the pattern is what marks a directory, which `ls -p`
	// adds. -printf would say so directly but is a GNU extension, and a
	// server may be running BSD or macOS userland.
	stdout, _, exitCode, err := r.run("", "ls", "-1p", "--", dir)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		// Missing directory: nothing stored here, which is not a failure.
		return nil, nil
	}
	var listed []agentDirEntry
	for _, line := range strings.Split(string(stdout), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		isDir := strings.HasSuffix(name, "/")
		listed = append(listed, agentDirEntry{
			Name:  strings.TrimSuffix(name, "/"),
			IsDir: isDir,
		})
		if len(listed) >= maxDiscoveryDirectoryEntries {
			break
		}
	}
	return listed, nil
}

func (r remoteFiles) findFilesByName(dir, suffix string, limit int) ([]string, error) {
	// Plain lexical order, which for a date-stamped layout is chronological.
	// No -printf here, so this needs nothing beyond POSIX find.
	script := fmt.Sprintf(`find %s -type f -name %s 2>/dev/null | sort -r | head -n %d`,
		shellQuoteForServer(dir), shellQuoteForServer("*"+suffix), limit)
	stdout, _, exitCode, err := r.run("", "sh", "-c", script)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, nil
	}
	var paths []string
	for _, line := range strings.Split(string(stdout), "\n") {
		if path := strings.TrimSpace(line); path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func (r remoteFiles) findFiles(dir, suffix string, limit int) ([]string, error) {
	// Sorted and cut on the server, so a store that has grown for years does
	// not travel over SSH only to be thrown away here. One measured store held
	// 862 files across 24 GB; this brings back the newest `limit` paths.
	//
	// Tab-separated, because a path may contain spaces. The mtime leads so
	// sort can order by it, and cut drops it again.
	//
	// -printf is a GNU extension: a BSD or macOS server has find(1) without
	// it. There the first pipeline yields nothing, and the second runs — an
	// unordered listing, which is worse than sorted but far better than none.
	// Tested against both shapes rather than assumed.
	quotedDir := shellQuoteForServer(dir)
	quotedPattern := shellQuoteForServer("*" + suffix)
	script := fmt.Sprintf(
		`found=$(find %s -type f -name %s -printf '%%T@\t%%p\n' 2>/dev/null `+
			`| sort -rn | head -n %d | cut -f2-); `+
			`if [ -n "$found" ]; then printf '%%s\n' "$found"; `+
			`else find %s -type f -name %s 2>/dev/null | head -n %d; fi`,
		quotedDir, quotedPattern, limit, quotedDir, quotedPattern, limit)

	stdout, _, exitCode, err := r.run("", "sh", "-c", script)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, nil
	}
	var paths []string
	for _, line := range strings.Split(string(stdout), "\n") {
		if path := strings.TrimSpace(line); path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// agentDirEntry is one name in a directory.
type agentDirEntry struct {
	Name  string
	IsDir bool
}

// shellQuoteForServer wraps a value so the server's shell treats it as one
// word, whatever it contains. A single quote inside would otherwise end the
// quoting and hand the rest to the shell.
func shellQuoteForServer(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// agentSessionScanLimit caps how many stored records a listing reads.
//
// The dialog offers recent conversations, so reading the whole store to show
// the newest handful is work nobody asked for — and one measured store held
// 862 files across 24 GB. The same figure the Antigravity listing uses.
const agentSessionScanLimit = 200

// maxAgentFileBytes caps a single remote read.
//
// A transcript of a long conversation runs to a few megabytes; this leaves
// room for that while refusing to stream something unbounded over SSH.
const maxAgentFileBytes = 64 << 20

// FilesOn returns the reader for the machine a session's tab would run on:
// this computer when serverID is empty, the server's connection otherwise.
//
// Routes are held per session and server, because a connection belongs to the
// session that opened it — so both are needed to find one.
//
// A server with no live connection is an error rather than a quiet fall back
// to the local disk. Answering with this machine's conversations would offer
// the user conversations the tab could never resume.
func FilesOn(sessionID, serverID string) (agentFiles, error) {
	if serverID == "" {
		return localFiles{}, nil
	}
	found, ok := executors.Load(tabExecutorKey(sessionID, serverID))
	if !ok {
		// The session's own machine, for a tab that stays on it.
		found, ok = executors.Load(sessionID)
	}
	if !ok {
		return nil, fmt.Errorf("error.serverNotConnected")
	}
	shell, isShell := found.(ShellExecutor)
	if !isShell {
		return nil, fmt.Errorf("error.serverNotConnected")
	}
	return remoteFiles{shell: shell}, nil
}

// ListClaudeSessionsOn lists Claude's conversations for a project directory on
// a given machine.
//
// The session and server together name the connection to read through, the
// same pair that routes a tab's commands there.
func ListClaudeSessionsOn(sessionID, serverID, projectPath string) ([]AgentSession, error) {
	files, err := FilesOn(sessionID, serverID)
	if err != nil {
		return nil, err
	}
	return listAgentSessionsByHistoryFrom(files, projectPath)
}

// ListAgentSessionsOn lists one agent's conversations on a given machine.
//
// serverID empty reads this computer, which is what the per-agent functions
// have always done. Agents this cannot read remotely answer with an empty
// list rather than the local machine's records: a list that looks right and
// resumes nothing is worse than no list at all.
func ListAgentSessionsOn(sessionID, serverID string, agent AgentType, projectPath string) ([]AgentSession, error) {
	if serverID == "" {
		return ListAgentSessionsFor(agent, projectPath)
	}

	files, err := FilesOn(sessionID, serverID)
	if err != nil {
		return nil, err
	}

	switch agent {
	case AgentClaude:
		return listAgentSessionsByHistoryFrom(files, projectPath)
	case AgentCodex:
		return listCodexSessionsFrom(files, projectPath)
	case AgentCursor:
		return listCursorSessionsFrom(files, projectPath)
	case AgentOpenCode:
		return listOpenCodeSessionsFrom(files, projectPath)
	case AgentGemini:
		remote, isRemote := files.(remoteFiles)
		if !isRemote {
			return []AgentSession{}, nil
		}
		return ListGeminiSessionsVia(remote.shell, projectPath)
	default:
		// Antigravity keeps its conversations in a SQLite database, read here
		// through a Go driver that needs the file itself; copying a database
		// across to list it is not worth what it buys. Amazon Q records no
		// conversation list at all — it resumes the last one for a directory,
		// which needs no reading.
		return []AgentSession{}, nil
	}
}

// ListAgentSessionsFor is the local listing for one agent.
func ListAgentSessionsFor(agent AgentType, projectPath string) ([]AgentSession, error) {
	switch agent {
	case AgentClaude:
		return ListAgentSessionsByHistory(projectPath)
	case AgentGemini:
		return ListGeminiSessions(projectPath)
	case AgentCodex:
		return ListCodexSessions(projectPath)
	case AgentCursor:
		return ListCursorSessions(projectPath)
	case AgentAntigravity:
		return ListAntigravitySessions(projectPath)
	case AgentOpenCode:
		return ListOpenCodeSessions(projectPath)
	case AgentAmazonQ:
		return ListAmazonQSessions(projectPath)
	default:
		return nil, nil
	}
}

// cleanServerPath tidies a path using Unix rules, whatever this computer uses.
//
// path.Clean rather than filepath.Clean: on Windows the latter rewrites "/" to
// "\", which would be the wrong shape for a server and — where the result is
// hashed, as Cursor's project directory is — would name a directory that
// cannot exist there.
func cleanServerPath(value string) string {
	if value == "" {
		return ""
	}
	cleaned := path.Clean(value)
	return cleaned
}
