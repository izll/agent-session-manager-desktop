package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// linkPresenceLock points a fake /proc descriptor at a conversation's presence
// lock, the way a live agy holds one open.
func linkPresenceLock(t *testing.T, procRoot, presenceRoot, conversationID, fd string, pid int) {
	t.Helper()
	if err := os.MkdirAll(presenceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(presenceRoot, conversationID+".lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fdDir := filepath.Join(procRoot, fmt.Sprint(pid), "fd")
	if err := os.MkdirAll(fdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(lock, filepath.Join(fdDir, fd)); err != nil {
		t.Fatal(err)
	}
}

// The id is the lock's own name. Measured on a running 1.2.3: the process held
// presence/<id>.lock throughout the conversation.
func TestAntigravityIDComesFromTheOpenPresenceLock(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	presenceRoot := filepath.Join(tmp, "presence")

	writeProcessChildren(t, procRoot, 100, "")
	linkPresenceLock(t, procRoot, presenceRoot, "7c24d7d3-dd20-41eb-ae80-e30a876264fb", "7", 100)

	got := detectAntigravityConversationIDFromProcessTreeContext(
		context.Background(), procRoot, presenceRoot, 100, "")
	if got != "7c24d7d3-dd20-41eb-ae80-e30a876264fb" {
		t.Errorf("conversation id = %q, want the name of the open lock", got)
	}
}

// A lock left behind by a conversation that has ended is not open by anyone.
// Three were on disk when this was measured and only the live one appeared in
// a process's descriptors — which is what makes the lock trustworthy.
func TestAntigravityOrphanedLocksAreNotClaimed(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	presenceRoot := filepath.Join(tmp, "presence")

	writeProcessChildren(t, procRoot, 100, "")
	// Two locks on disk, neither opened by the process.
	for _, id := range []string{"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222"} {
		if err := os.MkdirAll(presenceRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(presenceRoot, id+".lock"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if got := detectAntigravityConversationIDFromProcessTreeContext(
		context.Background(), procRoot, presenceRoot, 100, ""); got != "" {
		t.Errorf("conversation id = %q, want empty: no lock was held open", got)
	}
}

// Two panes, two conversations: each process holds its own lock.
func TestAntigravityPanesAreToldApartByTheirOwnProcess(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	presenceRoot := filepath.Join(tmp, "presence")

	writeProcessChildren(t, procRoot, 100, "")
	writeProcessChildren(t, procRoot, 200, "")
	linkPresenceLock(t, procRoot, presenceRoot, "aaaaaaaa-1111-4111-8111-111111111111", "7", 100)
	linkPresenceLock(t, procRoot, presenceRoot, "bbbbbbbb-2222-4222-8222-222222222222", "7", 200)

	first := detectAntigravityConversationIDFromProcessTreeContext(
		context.Background(), procRoot, presenceRoot, 100, "")
	second := detectAntigravityConversationIDFromProcessTreeContext(
		context.Background(), procRoot, presenceRoot, 200, "")
	if first != "aaaaaaaa-1111-4111-8111-111111111111" ||
		second != "bbbbbbbb-2222-4222-8222-222222222222" {
		t.Errorf("panes were not told apart: %q and %q", first, second)
	}
}

// A child process holding the lock still counts.
func TestAntigravityIDIsFoundOnAChildProcess(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	presenceRoot := filepath.Join(tmp, "presence")

	writeProcessChildren(t, procRoot, 100, "101")
	writeProcessChildren(t, procRoot, 101, "")
	linkPresenceLock(t, procRoot, presenceRoot, "cccccccc-3333-4333-8333-333333333333", "7", 101)

	if got := detectAntigravityConversationIDFromProcessTreeContext(
		context.Background(), procRoot, presenceRoot, 100, ""); got != "cccccccc-3333-4333-8333-333333333333" {
		t.Errorf("a lock held by a child process was missed: %q", got)
	}
}

// A pane that has not started a conversation yet — sitting at the trust prompt
// — genuinely has no id. Guessing one would resume a conversation the user
// never opened here.
func TestAntigravityWithNoConversationYetFindsNothing(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	writeProcessChildren(t, procRoot, 100, "")
	if got := detectAntigravityConversationIDFromProcessTreeContext(
		context.Background(), procRoot, filepath.Join(tmp, "presence"), 100, ""); got != "" {
		t.Errorf("conversation id = %q, want empty", got)
	}
}

// Two locks open at once is ambiguous, and a wrong id is worse than none.
func TestAntigravityTwoOpenLocksAreAmbiguous(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	presenceRoot := filepath.Join(tmp, "presence")

	writeProcessChildren(t, procRoot, 100, "")
	linkPresenceLock(t, procRoot, presenceRoot, "dddddddd-4444-4444-8444-444444444444", "7", 100)
	linkPresenceLock(t, procRoot, presenceRoot, "eeeeeeee-5555-4555-8555-555555555555", "8", 100)

	if got := detectAntigravityConversationIDFromProcessTreeContext(
		context.Background(), procRoot, presenceRoot, 100, ""); got != "" {
		t.Errorf("conversation id = %q, want empty when two locks are open", got)
	}
}

// Only files under the presence directory count. The agent keeps its log and
// crash file open too, and a lock-looking path elsewhere must not be taken.
func TestAntigravityIgnoresLocksOutsideThePresenceDirectory(t *testing.T) {
	tmp := t.TempDir()
	procRoot := filepath.Join(tmp, "proc")
	presenceRoot := filepath.Join(tmp, "presence")
	elsewhere := filepath.Join(tmp, "elsewhere")

	writeProcessChildren(t, procRoot, 100, "")
	linkPresenceLock(t, procRoot, elsewhere, "ffffffff-6666-4666-8666-666666666666", "7", 100)

	if got := detectAntigravityConversationIDFromProcessTreeContext(
		context.Background(), procRoot, presenceRoot, 100, ""); got != "" {
		t.Errorf("a lock outside the presence directory was claimed: %q", got)
	}
}

// A tab records its own id, and one that moved to another conversation with
// /resume is corrected rather than left pointing at the old one.
func TestCaptureAntigravityResumeIDsUpdatesTabs(t *testing.T) {
	inst := &Instance{
		ID:     "s1",
		Agent:  AgentAntigravity,
		Status: StatusRunning,
		Path:   "/repo",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Agent: AgentAntigravity, ResumeSessionID: "stale"},
			{Index: 3, Agent: AgentTerminal},
			{Index: 4, Agent: AgentAntigravity, Stopped: true, ResumeSessionID: "kept"},
		},
	}
	detect := func(_ string, windowIdx int, _ string) string {
		switch windowIdx {
		case 1:
			return "main-conversation"
		case 2:
			return "tab-conversation"
		}
		return ""
	}
	if !inst.captureAntigravityResumeIDsAtMainWindow(detect, 1, true) {
		t.Fatal("nothing was recorded")
	}
	if inst.ResumeSessionID != "main-conversation" {
		t.Errorf("session id = %q", inst.ResumeSessionID)
	}
	if inst.FollowedWindows[0].ResumeSessionID != "tab-conversation" {
		t.Errorf("a stale tab id was not corrected: %q", inst.FollowedWindows[0].ResumeSessionID)
	}
	if inst.FollowedWindows[2].ResumeSessionID != "kept" {
		t.Error("a stopped tab was touched")
	}
}

func TestNeedsAntigravityResumeCapture(t *testing.T) {
	stopped := &Instance{Agent: AgentAntigravity, Status: StatusStopped}
	if stopped.NeedsAntigravityResumeCapture() {
		t.Error("a stopped session was polled")
	}
	running := &Instance{Agent: AgentClaude, Status: StatusRunning,
		FollowedWindows: []FollowedWindow{{Index: 2, Agent: AgentAntigravity}}}
	if !running.NeedsAntigravityResumeCapture() {
		t.Error("an Antigravity tab under another agent's session was skipped")
	}
}
