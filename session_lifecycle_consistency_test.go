package main

import (
	"errors"
	"os"
	"strings"
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

func TestForkCreationPathsRollbackExternalProcessesOnPersistenceFailure(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"func (a *App) ForkToNewTab",
		"func (a *App) ForkToNewSession",
	} {
		start := strings.Index(text, marker)
		if start < 0 {
			t.Fatalf("missing %s", marker)
		}
		end := strings.Index(text[start+len(marker):], "\nfunc ")
		body := text[start:]
		if end >= 0 {
			body = text[start : start+len(marker)+end]
		}
		if !strings.Contains(body, "persistOrRollbackExternalMutation(") {
			t.Fatalf("%s persists an external tmux mutation without rollback", marker)
		}
	}
}

func TestStartPathsRollbackExternalProcessesOnPersistenceFailure(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"func (a *App) StartSession(",
		"func (a *App) StartSessionWithResume(",
		"func (a *App) RestartTab(",
		"func (a *App) RestartTabWithResume(",
	} {
		start := strings.Index(text, marker)
		if start < 0 {
			t.Fatalf("missing %s", marker)
		}
		end := strings.Index(text[start+len(marker):], "\nfunc ")
		body := text[start:]
		if end >= 0 {
			body = text[start : start+len(marker)+end]
		}
		if !strings.Contains(body, "persistOrRollbackExternalMutation(") {
			t.Fatalf("%s leaves a live process behind when persistence fails", marker)
		}
		if strings.Contains(marker, "RestartTab") && !strings.Contains(body, "RestopWindow(windowIdx)") {
			t.Fatalf("%s must restore a dead pane without killing its tmux session", marker)
		}
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
