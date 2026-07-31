package session

import "testing"

func TestTmuxWindowIndexListedRequiresExactIndex(t *testing.T) {
	t.Parallel()

	output := []byte("0\n2\n12\n")
	for _, index := range []int{0, 2, 12} {
		if !tmuxWindowIndexListed(output, index) {
			t.Errorf("tmuxWindowIndexListed() did not find %d", index)
		}
	}
	for _, index := range []int{1, 3, 20} {
		if tmuxWindowIndexListed(output, index) {
			t.Errorf("tmuxWindowIndexListed() falsely found %d", index)
		}
	}
}

func TestParseTmuxWindowIndex(t *testing.T) {
	t.Parallel()

	if got, err := parseTmuxWindowIndex([]byte("7\n")); err != nil || got != 7 {
		t.Fatalf("parseTmuxWindowIndex() = %d, %v; want 7, nil", got, err)
	}
	if _, err := parseTmuxWindowIndex([]byte("")); err == nil {
		t.Fatal("parseTmuxWindowIndex() accepted empty output")
	}
}

func TestDeleteWindowRemovesDuplicateDescriptors(t *testing.T) {
	t.Parallel()

	instance := &Instance{
		Status: StatusStopped,
		FollowedWindows: []FollowedWindow{
			{Index: 1, Name: "terminal", Agent: AgentTerminal},
			{Index: 1, Name: "stale codex alias", Agent: AgentCodex},
			{Index: 2, Name: "keep", Agent: AgentClaude},
		},
	}
	if err := instance.DeleteWindow(1); err != nil {
		t.Fatal(err)
	}
	if len(instance.FollowedWindows) != 1 ||
		instance.FollowedWindows[0].Index != 2 ||
		instance.FollowedWindows[0].Name != "keep" {
		t.Fatalf("unexpected followed windows after cleanup: %#v", instance.FollowedWindows)
	}
}

func TestIdentifyMainWindowIndex(t *testing.T) {
	t.Parallel()

	followed := []FollowedWindow{{Index: 2}, {Index: 5}}
	tests := []struct {
		name   string
		output string
		want   int
		ok     bool
	}{
		{
			name:   "marker survives reordered indices",
			output: "2\t\n5\t\n9\t1\n",
			want:   9,
			ok:     true,
		},
		{
			name:   "legacy session inferred by exclusion",
			output: "2\t\n5\t\n9\t\n",
			want:   9,
			ok:     true,
		},
		{
			name:   "untracked ambiguity fails closed",
			output: "2\t\n5\t\n8\t\n9\t\n",
			ok:     false,
		},
		{
			name:   "multiple markers fail closed",
			output: "2\t1\n5\t\n9\t1\n",
			ok:     false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := identifyMainWindowIndex([]byte(tt.output), followed)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("identifyMainWindowIndex() = %d, %v; want %d, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestSelectFollowedWindowForRestartRepairsTerminalAlias(t *testing.T) {
	t.Parallel()

	windows := []FollowedWindow{
		{Index: 1, Agent: AgentTerminal, Name: "stale terminal"},
		{Index: 1, Agent: AgentCodex, Name: "codex"},
		{Index: 1, Agent: AgentCodex, Name: "duplicate codex"},
	}
	index, collapse, err := selectFollowedWindowForRestart(windows, 1)
	if err != nil {
		t.Fatal(err)
	}
	if index != 1 || !collapse {
		t.Fatalf("selection = %d, %v; want Codex index 1 and collapse", index, collapse)
	}
}

func TestSelectFollowedWindowForRestartRejectsConflictingAgents(t *testing.T) {
	t.Parallel()

	windows := []FollowedWindow{
		{Index: 1, Agent: AgentCodex},
		{Index: 1, Agent: AgentClaude},
	}
	if _, _, err := selectFollowedWindowForRestart(windows, 1); err == nil {
		t.Fatal("conflicting duplicate agents were accepted")
	}
}

// Several marked windows must be refused rather than guessed at.
//
// This is the state that broke tab deletion: getMainWindowIndex backfilled the
// marker on every call, without checking whether one already existed, so the
// marked set grew until it covered every window. Observed on a live session with
// eight windows, all marked. Failing closed is right — killing the wrong window
// takes the agent with it — but nothing was asserting it, so the backfill bug
// was free to accumulate.
func TestIdentifyMainWindowIndexRefusesMultipleMarks(t *testing.T) {
	t.Parallel()

	if _, ok := identifyMainWindowIndex([]byte("6\t1\n7\t1\n8\t1\n"), nil); ok {
		t.Fatal("more than one marked window must not resolve to a main window")
	}
	// One mark among many windows is still unambiguous.
	if got, ok := identifyMainWindowIndex([]byte("6\t\n7\t1\n8\t\n"), nil); !ok || got != 7 {
		t.Fatalf("got (%d, %v), want (7, true)", got, ok)
	}
}

// All windows marked means the marker is meaningless and identification has to
// fall back — but a PARTIAL set of marks must still fail closed.
//
// psmux stores a -w user option globally, so one write tags every window in the
// session, and the value cannot be unset afterwards. Sessions in that state have
// to keep working; a session where only some windows are marked is genuinely
// ambiguous and guessing there could kill the agent's own window.
func TestIdentifyMainWindowIndexHandlesGlobalMarker(t *testing.T) {
	t.Parallel()

	// Every window marked, one followed tab: fall back to the unfollowed one.
	got, ok := identifyMainWindowIndex(
		[]byte("0\t1\n1\t1\n2\t1\n"),
		[]FollowedWindow{{Index: 1}, {Index: 2}},
	)
	if !ok || got != 0 {
		t.Fatalf("all-marked: got (%d, %v), want (0, true)", got, ok)
	}

	// Some marked, some not: ambiguous, refuse.
	if _, ok := identifyMainWindowIndex([]byte("0\t1\n1\t1\n2\t\n"), nil); ok {
		t.Fatal("a partial set of marks must not resolve to a main window")
	}
}
