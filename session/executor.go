package session

import (
	"context"
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
