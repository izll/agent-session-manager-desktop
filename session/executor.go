package session

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Where a session's commands run.
//
// A session lives either on this computer or on a server, and the difference
// has to travel with the session rather than with the process: one window
// manages several servers and the local machine at the same time, and the
// sidebar polls all of them together. A global switch — "we are remote now" —
// would be read by the poller and the terminal goroutines at whatever moment
// each happened to look.
//
// So an Instance carries its own executor, and every command it issues goes
// through that. The local implementation is the behaviour this package always
// had; the remote one is installed by the app when it loads a session that
// names a server.

// Executor runs multiplexer commands somewhere.
//
// Two methods rather than one returning a command object: an *exec.Cmd cannot
// cross a network, and the callers only ever want the output or the exit
// status.
type Executor interface {
	// Run executes and reports only whether it succeeded.
	Run(ctx context.Context, args ...string) error
	// Output executes and returns what the command wrote to standard output.
	//
	// Standard output alone, not both streams: a login shell on the server
	// prints its own messages to stderr, and mixing them in corrupts every
	// listing this package parses.
	Output(ctx context.Context, args ...string) ([]byte, error)
	// Describe names where commands go, for logs and error messages. "local",
	// or the server's display name.
	Describe() string
}

// ShellExecutor is an executor that can also run arbitrary commands, not only
// multiplexer ones.
//
// Separate from Executor because the two have different reach: the multiplexer
// commands are a closed set this package builds, while these are git
// invocations and file reads whose arguments come from a working directory on
// the far machine. An executor that cannot do this is still useful for
// sessions; one that can serves the diff and the file browser too.
type ShellExecutor interface {
	Executor
	// RunShell runs one command in a directory and returns its standard
	// output, its standard error and the exit status.
	//
	// The exit status is returned rather than turned into an error because
	// several callers here depend on it: `git ls-files --error-unmatch`
	// failing is how the code learns a file is untracked, which is an answer
	// rather than a fault.
	RunShell(ctx context.Context, dir string, args ...string) (stdout, stderr []byte, exitCode int, err error)
}

// ShellExecutorFor returns an executor that can run commands other than the
// multiplexer's, or nil when this session runs on the local machine.
//
// A nil return is the signal to take the local path, which is what every
// caller does today: the remote branch is an addition, not a replacement.
func ShellExecutorFor(sessionID string) ShellExecutor {
	found, ok := executors.Load(sessionID)
	if !ok {
		return nil
	}
	shell, isShell := found.(ShellExecutor)
	if !isShell {
		return nil
	}
	return shell
}

// shellExec returns this instance's shell executor, or nil for a local
// session.
func (i *Instance) shellExec() ShellExecutor {
	if i.ServerID == "" {
		return nil
	}
	return ShellExecutorFor(i.ID)
}

// localExecutor runs commands on this computer, exactly as before.
type localExecutor struct{}

func (localExecutor) Run(ctx context.Context, args ...string) error {
	return TmuxCommandContext(ctx, args...).Run()
}

func (localExecutor) Output(ctx context.Context, args ...string) ([]byte, error) {
	return TmuxCommandContext(ctx, args...).Output()
}

func (localExecutor) Describe() string { return "local" }

// LocalExecutor is the one every session gets until told otherwise.
var LocalExecutor Executor = localExecutor{}

// executors holds the executor for each session id.
//
// Kept beside the instances rather than inside them because an Instance is
// serialised to disk and read back: a connection cannot be stored, and a field
// that is always nil after a reload is a field that will be used as if it were
// not. Registered by the app when it connects to a server, and cleared when
// that connection ends.
var executors sync.Map // map[string]Executor

// SetExecutor routes one session's commands through an executor.
func SetExecutor(sessionID string, executor Executor) {
	if executor == nil {
		executors.Delete(sessionID)
		return
	}
	executors.Store(sessionID, executor)
}

// ClearExecutor sends a session's commands back to this computer.
//
// Called when a server's connection drops. The session is then unreachable
// rather than local — but its commands failing against a local multiplexer
// that has no such session is a clearer outcome than commands sent into a
// connection that is gone.
func ClearExecutor(sessionID string) {
	executors.Delete(sessionID)
}

// ExecutorFor returns where this session's commands should run.
func ExecutorFor(sessionID string) Executor {
	if found, ok := executors.Load(sessionID); ok {
		if executor, isExecutor := found.(Executor); isExecutor && executor != nil {
			return executor
		}
	}
	return LocalExecutor
}

// exec returns this instance's executor.
func (i *Instance) exec() Executor {
	return ExecutorFor(i.ID)
}

// IsRemote reports whether this session runs on a server.
func (i *Instance) IsRemote() bool {
	return i.ServerID != ""
}

// tmuxRun issues one command for this session.
func (i *Instance) tmuxRun(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()
	return i.exec().Run(ctx, args...)
}

// tmuxRunContext is tmuxRun with a caller-supplied deadline.
func (i *Instance) tmuxRunContext(ctx context.Context, args ...string) error {
	return i.exec().Run(ctx, args...)
}

// tmuxOutput issues one command and returns its output.
func (i *Instance) tmuxOutput(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), TmuxCommandTimeout)
	defer cancel()
	return i.exec().Output(ctx, args...)
}

// tmuxOutputContext is tmuxOutput with a caller-supplied deadline.
func (i *Instance) tmuxOutputContext(ctx context.Context, args ...string) ([]byte, error) {
	return i.exec().Output(ctx, args...)
}

// RouteInstance is called whenever an instance is loaded from storage, so
// whoever owns the connections can point its commands at the right machine.
//
// A hook rather than a direct call: the connections belong to the application
// layer, which knows about SSH and keyrings, and this package must not. Left
// nil, every session runs locally — which is what the app does before it has
// connected to anything.
var RouteInstance func(*Instance)

// routeLoaded applies the hook to instances just read from disk.
func routeLoaded(instances []*Instance) {
	route := RouteInstance
	if route == nil {
		return
	}
	for _, inst := range instances {
		if inst != nil && inst.ServerID != "" {
			route(inst)
		}
	}
}

// gitOutput runs a git command where this session's files are.
//
// Local sessions keep the behaviour this package always had, including the
// environment the caller set up. A session on a server has its repository
// there, so the command goes through the helper instead — and gitEnv is not
// carried across: it exists to stop git reading the developer's own config,
// which is a local concern, and the server's git has its own.
func (i *Instance) gitOutput(args []string, gitEnv []string) ([]byte, error) {
	if shell := i.shellExec(); shell != nil {
		ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
		defer cancel()

		stdout, stderr, exitCode, err := shell.RunShell(ctx, "", append([]string{"git"}, args...)...)
		if err != nil {
			return nil, err
		}
		if exitCode != 0 {
			return stdout, fmt.Errorf("git %s failed on %s: %s",
				args[0], shell.Describe(), strings.TrimSpace(string(stderr)))
		}
		return stdout, nil
	}

	cmd, cancel := GitCommandTimed(args...)
	defer cancel()
	if gitEnv != nil {
		cmd.Env = gitEnv
	}
	return cmd.Output()
}

// Reading files where a session lives.
//
// The file browser, the editor and the diff all read from the working
// directory, which is on the server for a remote session. Each of these takes
// the local path when the session is local, so the ordinary case is untouched.

// RemoteDirEntry is one item in a remote directory listing.
type RemoteDirEntry struct {
	Name  string
	IsDir bool
	Size  int64
}

// readFileWhereSessionLives reads a file from the machine this session runs on.
func (i *Instance) readFileWhereSessionLives(path string, limit int64) ([]byte, error) {
	shell := i.shellExec()
	if shell == nil {
		return readFileAtMost(path, limit)
	}

	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
	defer cancel()

	// head rather than cat: a caller asking for at most N bytes must not have
	// a gigabyte pulled across the network first and trimmed afterwards.
	stdout, stderr, exitCode, err := shell.RunShell(ctx, "",
		"head", "-c", fmt.Sprintf("%d", limit), "--", path)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("could not read %s on %s: %s",
			path, shell.Describe(), strings.TrimSpace(string(stderr)))
	}
	return stdout, nil
}

// listDirectoryWhereSessionLives lists a directory on the machine this session
// runs on.
func (i *Instance) listDirectoryWhereSessionLives(path string) ([]RemoteDirEntry, error) {
	shell := i.shellExec()
	if shell == nil {
		return nil, nil // Caller falls back to the local listing.
	}

	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
	defer cancel()

	// One line per entry, tab-separated, with the type first. Parsed rather
	// than ls -l, whose columns differ between systems and locales.
	script := fmt.Sprintf(
		`find %s -maxdepth 1 -mindepth 1 -printf '%%y\t%%s\t%%f\n' 2>/dev/null`,
		shellQuoteForRemote(path))
	stdout, stderr, exitCode, err := shell.RunShell(ctx, "", "sh", "-c", script)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("could not list %s on %s: %s",
			path, shell.Describe(), strings.TrimSpace(string(stderr)))
	}

	var entries []RemoteDirEntry
	for _, line := range strings.Split(string(stdout), "\n") {
		fields := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 3)
		if len(fields) != 3 {
			continue
		}
		size, _ := strconv.ParseInt(fields[1], 10, 64)
		entries = append(entries, RemoteDirEntry{
			Name:  fields[2],
			IsDir: fields[0] == "d",
			Size:  size,
		})
	}
	return entries, nil
}

// shellQuoteForRemote quotes a path for a command that will be interpreted by
// a shell on the server.
//
// Separate from the quoting the executor does to its arguments: this value
// ends up inside a script passed to sh -c, so it needs quoting of its own.
func shellQuoteForRemote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
