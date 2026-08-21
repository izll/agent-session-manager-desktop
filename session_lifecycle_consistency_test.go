package main

import (
	"errors"
	"testing"

	"asmgr-desktop/session"
)

func TestResolveResumeIDValidatesStoredFallback(t *testing.T) {
	exists := func(_ session.AgentType, id string) bool { return id == "valid-id" }

	if got, clear := resolveResumeID(session.AgentClaude, "", "valid-id", exists); got != "valid-id" || clear {
		t.Fatalf("stored valid fallback = (%q, %v), want (valid-id, false)", got, clear)
	}
	if got, clear := resolveResumeID(session.AgentClaude, "", "missing-id", exists); got != "" || !clear {
		t.Fatalf("missing stored fallback = (%q, %v), want empty and clear", got, clear)
	}
	if got, clear := resolveResumeID(session.AgentClaude, "unsafe id;", "valid-id", exists); got != "" || !clear {
		t.Fatalf("unsafe request = (%q, %v), want empty and clear", got, clear)
	}
}

func TestExternalMutationPersistenceFailureRunsRollbackAndJoinsErrors(t *testing.T) {
	persistErr := errors.New("save failed")
	rollbackErr := errors.New("tmux rollback failed")
	rolledBack := false

	err := persistOrRollbackExternalMutation(
		func() error { return persistErr },
		func() error {
			rolledBack = true
			return rollbackErr
		},
	)
	if !rolledBack {
		t.Fatal("external mutation was not rolled back after persistence failed")
	}
	if !errors.Is(err, persistErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("joined error = %v, want persistence and rollback failures", err)
	}
}

func TestExternalMutationSuccessDoesNotRollback(t *testing.T) {
	rolledBack := false
	if err := persistOrRollbackExternalMutation(
		func() error { return nil },
		func() error { rolledBack = true; return nil },
	); err != nil {
		t.Fatal(err)
	}
	if rolledBack {
		t.Fatal("successful persistence unexpectedly rolled back the tmux mutation")
	}
}
