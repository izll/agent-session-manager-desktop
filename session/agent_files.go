package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	// exists reports whether a path is there at all.
	exists(path string) bool
	// join builds a path using the separator of the machine being read, which
	// is not necessarily this one's.
	join(parts ...string) string
}

// localFiles reads this computer's disk, which is what every listing did
// before servers existed.
type localFiles struct{}

func (localFiles) home() (string, error) { return os.UserHomeDir() }

func (localFiles) readFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (localFiles) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

func (r remoteFiles) exists(path string) bool {
	_, _, exitCode, err := r.run("", "test", "-e", path)
	return err == nil && exitCode == 0
}

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
