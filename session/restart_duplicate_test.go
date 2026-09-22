package session

import "testing"

// Two records on one index, disagreeing about the agent.
//
// A window whose descriptor outlived it leaves its record behind, and the
// multiplexer later gives the number to a new window — so a session ends up
// with two tabs on one index. Refusing to guess is right when there is nothing
// to go on, but the multiplexer knows which window it actually has, and it is
// named after one of them.
//
// Seen on a real session: a codex record with no window of its own sat on
// index 4 beside "claude tab ezt megtalalod??", which held it. Stopping that
// tab left neither able to start — "window 4 has conflicting duplicate agent
// metadata (codex and claude)".
func TestTheWindowsOwnNameSettlesADuplicateIndex(t *testing.T) {
	windows := []FollowedWindow{
		{Index: 4, Agent: AgentCodex, Name: "codex tab"},
		{Index: 4, Agent: AgentClaude, Name: "claude tab ezt megtalalod??"},
	}

	// With the name, the record that matches the window wins.
	idx, collapse, err := selectFollowedWindowNamed(
		windows, 4, "", "", "claude tab ezt megtalalod??")
	if err != nil {
		t.Fatalf("the name did not settle it: %v", err)
	}
	if idx != 1 {
		t.Errorf("chose record %d (%s), want the claude one", idx, windows[idx].Agent)
	}
	// And the loser is dropped, so the conflict does not come back.
	if !collapse {
		t.Error("the duplicate was left in place")
	}

	// The other way round, to show the name is what decides rather than the
	// order they happen to be in.
	idx, _, err = selectFollowedWindowNamed(windows, 4, "", "", "codex tab")
	if err != nil {
		t.Fatalf("the name did not settle it: %v", err)
	}
	if idx != 0 {
		t.Errorf("chose record %d (%s), want the codex one", idx, windows[idx].Agent)
	}
}

// Without a name there is nothing to go on, and guessing would start the wrong
// agent in a window the user is looking at. Refusing is the safe answer.
func TestADuplicateIndexIsStillRefusedWithNothingToGoOn(t *testing.T) {
	windows := []FollowedWindow{
		{Index: 4, Agent: AgentCodex, Name: "codex tab"},
		{Index: 4, Agent: AgentClaude, Name: "claude tab"},
	}
	if _, _, err := selectFollowedWindowNamed(windows, 4, "", "", ""); err == nil {
		t.Error("two different agents on one index were resolved by guessing")
	}
}

// A name that matches neither record is no help either — and must not be taken
// as a reason to pick one.
func TestAnUnrecognisedWindowNameDoesNotDecide(t *testing.T) {
	windows := []FollowedWindow{
		{Index: 4, Agent: AgentCodex, Name: "codex tab"},
		{Index: 4, Agent: AgentClaude, Name: "claude tab"},
	}
	if _, _, err := selectFollowedWindowNamed(windows, 4, "", "", "something else"); err == nil {
		t.Error("a name matching neither record was used to choose between them")
	}
}
