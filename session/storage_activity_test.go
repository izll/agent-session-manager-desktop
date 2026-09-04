package session

import (
	"testing"
	"time"
)

// The sidebar poll reloads instances from disk every tick, so a LastActiveAt
// set on those is discarded a second later — which is exactly what happened:
// the field was written on every poll and every session on disk still held the
// zero time.
func TestRecordActivityIsPersisted(t *testing.T) {
	t.Parallel()

	storage := newRecoveryTestStorage(t)
	if err := storage.SaveAll([]*Instance{{ID: "s1", Name: "one"}}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	seen := time.Date(2026, 9, 4, 14, 50, 0, 0, time.UTC)
	if err := storage.RecordActivityForProject("", map[string]time.Time{"s1": seen}); err != nil {
		t.Fatal(err)
	}

	instances, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("instances = %d, want 1", len(instances))
	}
	if !instances[0].LastActiveAt.Equal(seen) {
		t.Fatalf("last active = %v, want %v", instances[0].LastActiveAt, seen)
	}
}

// Polls overlap, and a slow one can finish after a faster one that started
// later. The mark must not walk backwards when that happens.
func TestRecordActivityNeverMovesBackwards(t *testing.T) {
	t.Parallel()

	storage := newRecoveryTestStorage(t)
	newer := time.Date(2026, 9, 4, 14, 50, 0, 0, time.UTC)
	older := newer.Add(-10 * time.Minute)
	if err := storage.SaveAll([]*Instance{{ID: "s1", LastActiveAt: newer}}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	if err := storage.RecordActivityForProject("", map[string]time.Time{"s1": older}); err != nil {
		t.Fatal(err)
	}

	instances, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !instances[0].LastActiveAt.Equal(newer) {
		t.Fatalf("a late poll rolled the mark back to %v", instances[0].LastActiveAt)
	}
}

// Recording activity must not become a way for the polling loop to clobber
// edits a user is making at the same time.
func TestRecordActivityLeavesOtherFieldsAlone(t *testing.T) {
	t.Parallel()

	storage := newRecoveryTestStorage(t)
	current := &Instance{
		ID:              "s1",
		Name:            "renamed while polling",
		Notes:           "notes written while polling",
		ResumeSessionID: "conversation-1",
	}
	if err := storage.SaveAll([]*Instance{current}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	seen := time.Date(2026, 9, 4, 14, 50, 0, 0, time.UTC)
	if err := storage.RecordActivityForProject("", map[string]time.Time{"s1": seen}); err != nil {
		t.Fatal(err)
	}

	instances, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	got := instances[0]
	if got.Name != "renamed while polling" || got.Notes != "notes written while polling" ||
		got.ResumeSessionID != "conversation-1" {
		t.Fatalf("the activity write disturbed other fields: %+v", got)
	}
	if !got.LastActiveAt.Equal(seen) {
		t.Fatalf("last active = %v, want %v", got.LastActiveAt, seen)
	}
}

// An unknown ID (a session deleted between the poll starting and finishing)
// must not create anything or fail the write for the others.
func TestRecordActivityIgnoresUnknownSessions(t *testing.T) {
	t.Parallel()

	storage := newRecoveryTestStorage(t)
	if err := storage.SaveAll([]*Instance{{ID: "s1"}}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	seen := time.Date(2026, 9, 4, 14, 50, 0, 0, time.UTC)
	if err := storage.RecordActivityForProject("", map[string]time.Time{
		"s1":      seen,
		"deleted": seen,
	}); err != nil {
		t.Fatal(err)
	}

	instances, err := storage.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("instances = %d, want 1", len(instances))
	}
}
