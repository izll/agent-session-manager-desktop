package session

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
)

// readInstanceSource reads instance.go with its line endings normalised.
//
// Normalised because a Windows checkout hands the file back with CRLF, and
// every search below would miss — a lesson from the release that stopped with
// only the Linux packages built.
func readInstanceSource(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile("instance.go")
	if err != nil {
		t.Fatalf("reading instance.go: %v", err)
	}
	return strings.ReplaceAll(string(source), "\r\n", "\n")
}

// scriptedExecutor answers commands from a table, and records what it was
// asked. Standing in for a server so the lifecycle can be exercised without
// one.
type scriptedExecutor struct {
	mu       sync.Mutex
	asked    [][]string
	answers  map[string]string
	failWith map[string]error
}

func newScriptedExecutor() *scriptedExecutor {
	return &scriptedExecutor{
		answers:  make(map[string]string),
		failWith: make(map[string]error),
	}
}

func (s *scriptedExecutor) Run(ctx context.Context, args ...string) error {
	_, err := s.Output(ctx, args...)
	return err
}

func (s *scriptedExecutor) Output(ctx context.Context, args ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, args)

	key := args[0]
	if err, failing := s.failWith[key]; failing {
		return nil, err
	}
	return []byte(s.answers[key]), nil
}

func (s *scriptedExecutor) Describe() string { return "scripted server" }

func (s *scriptedExecutor) commandsNamed(name string) [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found [][]string
	for _, args := range s.asked {
		if len(args) > 0 && args[0] == name {
			found = append(found, args)
		}
	}
	return found
}

// Starting a session on a server must not consult this computer.
//
// The PATH searched by exec.LookPath is the local one, and a remote session's
// agent lives on the server — so a laptop without claude installed could not
// start a remote Claude session, and a laptop with it would claim a server
// that has none is fine. The connection test answers that question, where the
// answer comes from the right machine.
func TestRemoteStartDoesNotCheckTheLocalPath(t *testing.T) {
	source := readInstanceSource(t)

	start := strings.Index(source, "func (i *Instance) startWithResume(")
	if start < 0 {
		t.Fatal("startWithResume is gone")
	}
	body := source[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}

	if !strings.Contains(body, "exec.LookPath(cmdToCheck)") {
		t.Fatal("the agent check is gone")
	}
	// The guard and the check are one statement; what matters is that the
	// check cannot be reached for a session that runs elsewhere.
	if !strings.Contains(body, `cmdToCheck != "" && !i.IsRemote()`) {
		t.Error("a remote session's agent is looked for in this computer's PATH — " +
			"a laptop without claude could not start a remote Claude session, and " +
			"one with it would call a server that has none fine")
	}
}

// Creating a session on a server goes through the session's own executor.
func TestRemoteStartUsesTheSessionsExecutor(t *testing.T) {
	executor := newScriptedExecutor()
	// has-session must fail first, or the start believes one already exists.
	executor.failWith["has-session"] = context.Canceled

	inst := &Instance{
		ID:       "remote-1",
		Name:     "work",
		Path:     "/srv/project",
		ServerID: "srv1",
		Agent:    AgentTerminal,
		Status:   StatusStopped,
	}
	SetExecutor(inst.ID, executor)
	t.Cleanup(func() { ClearExecutor(inst.ID) })

	// The start will not complete without a live multiplexer answering, but
	// what matters is where it addressed its commands.
	_ = inst.Start()

	created := executor.commandsNamed("new-session")
	if len(created) == 0 {
		t.Fatal("the session was not created through its executor — it would " +
			"have been created on this computer instead")
	}
	if !containsArgument(created[0], "/srv/project") {
		t.Errorf("the session was created in the wrong directory: %v", created[0])
	}
}

// Every session keeps its own destination, including while another is being
// started.
func TestStartingOneSessionDoesNotAffectAnother(t *testing.T) {
	first := newScriptedExecutor()
	second := newScriptedExecutor()
	first.failWith["has-session"] = context.Canceled
	second.failWith["has-session"] = context.Canceled

	instA := &Instance{ID: "a", Name: "a", Path: "/a", ServerID: "s1", Agent: AgentTerminal}
	instB := &Instance{ID: "b", Name: "b", Path: "/b", ServerID: "s2", Agent: AgentTerminal}
	SetExecutor(instA.ID, first)
	SetExecutor(instB.ID, second)
	t.Cleanup(func() {
		ClearExecutor(instA.ID)
		ClearExecutor(instB.ID)
	})

	_ = instA.Start()
	_ = instB.Start()

	for name, executor := range map[string]*scriptedExecutor{"A": first, "B": second} {
		created := executor.commandsNamed("new-session")
		if len(created) == 0 {
			t.Errorf("session %s was not created on its own server", name)
		}
	}

	// Neither may have issued the other's command.
	for _, args := range first.commandsNamed("new-session") {
		if containsArgument(args, "/b") {
			t.Error("session A's executor was asked to create session B")
		}
	}
}

func containsArgument(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
