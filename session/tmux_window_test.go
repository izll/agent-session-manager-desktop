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
