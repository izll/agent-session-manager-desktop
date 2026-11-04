package session

import (
	"testing"
	"time"
)

func TestTheLatestOutputOfTheTerminalTabsCounts(t *testing.T) {
	now := time.Unix(1000, 0)
	inst := &Instance{Agent: AgentClaude, FollowedWindows: []FollowedWindow{
		{Index: 1, Agent: AgentTerminal}, {Index: 2, Agent: AgentTerminal}, {Index: 3, Agent: AgentClaude},
	}}
	for name, test := range map[string]struct {
		states map[int]TabState
		want   int64
	}{
		"latest of two":   {map[int]TabState{1: {LastOutput: 100}, 2: {LastOutput: 300}}, 300},
		"latest first":    {map[int]TabState{1: {LastOutput: 300}, 2: {LastOutput: 100}}, 300},
		"an agent's":      {map[int]TabState{3: {LastOutput: 900}, 1: {LastOutput: 100}}, 100},
		"only an agent's": {map[int]TabState{3: {LastOutput: 900}}, 0},
		"from the future": {map[int]TabState{1: {LastOutput: 5000}}, 1000},
		"nothing listed":  {nil, 0},
	} {
		got := inst.LastTerminalOutputFrom(test.states, now)
		if test.want == 0 {
			if !got.IsZero() {
				t.Errorf("%s: %v", name, got)
			}
			continue
		}
		if got.Unix() != test.want {
			t.Errorf("%s: %v", name, got.Unix())
		}
	}
}

// Nothing is claimed about a tab from a line that does not say everything.
func TestAnIncompleteListingClaimsNothing(t *testing.T) {
	states := parseTabStates([]byte("1\t100\t55\t0\tbash\t80x24\n" +
		"2\t\t55\t0\tbash\t80x24\n" +
		"3\t0\t55\t0\tbash\t80x24\n" +
		"4\t100\t\t0\tbash\t80x24\n" +
		"x\t100\t55\t0\tbash\t80x24\n" +
		"6\t100\n\n"))
	if len(states) != 1 || states[1].LastOutput != 100 || states[1].Identity == "" {
		t.Fatalf("states = %v", states)
	}
}

func TestWhatElseChangesAReadingShows(t *testing.T) {
	states := parseTabStates([]byte("1\t100\t55\t0\tbash\t80x24\n"))
	if states[1].Identity == parseTabStates([]byte("1\t100\t56\t0\tbash\t80x24\n"))[1].Identity {
		t.Fatal("another process reads as the same one")
	}
	if states[1].Identity == parseTabStates([]byte("1\t100\t55\t1\tbash\t80x24\n"))[1].Identity {
		t.Fatal("a dead pane reads as a live one")
	}
	if states[1].Identity == parseTabStates([]byte("1\t100\t55\t0\tbash\t90x24\n"))[1].Identity {
		t.Fatal("another size reads as the same one")
	}
}
