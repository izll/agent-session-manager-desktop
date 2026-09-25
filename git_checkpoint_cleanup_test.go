package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

func checkpointIDs(checkpoints []Checkpoint) []string {
	ids := []string{}
	for _, c := range checkpoints {
		ids = append(ids, c.ID)
	}
	return ids
}

func listedCheckpoints(t *testing.T, repo string) []Checkpoint {
	t.Helper()
	list, err := listCheckpoints(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	return list.Checkpoints
}

func TestSelectCheckpointsForCleanup(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	at := func(id, kind string, age time.Duration) Checkpoint {
		return Checkpoint{ID: id, Kind: kind, Created: now.Add(-age).Format(time.RFC3339)}
	}
	// Newest first, as the list comes.
	list := []Checkpoint{
		at("a", checkpointKindManual, time.Hour),
		at("b", checkpointKindBeforeRestore, 2*time.Hour),
		at("c", checkpointKindBeforeRestore, 3*checkpointDay),
		at("d", checkpointKindManual, 30*checkpointDay),
		at("e", checkpointKindManual, 29*checkpointDay+23*time.Hour),
		{ID: "undated", Kind: checkpointKindManual, Created: "yesterday-ish"},
		at("f", checkpointKindBeforeRestore, 100*checkpointDay),
	}

	cases := []struct {
		name string
		rule CheckpointCleanupRule
		keep int
		want []string
	}{
		{"older than 30 days", CheckpointCleanupRule{OlderThanDays: 30}, 0, []string{"d", "f"}},
		{"older than 1 day skips a fresh before-restore", CheckpointCleanupRule{OlderThanDays: 1}, 0, []string{"c", "d", "e", "f"}},
		{"before restore past the grace period", CheckpointCleanupRule{BeforeRestoreOnly: true}, 0, []string{"c", "f"}},
		{"before restore and old", CheckpointCleanupRule{OlderThanDays: 30, BeforeRestoreOnly: true}, 0, []string{"f"}},
		{"the newest are kept", CheckpointCleanupRule{OlderThanDays: 1}, 4, []string{"e", "f"}},
		{"no rule takes nothing", CheckpointCleanupRule{}, 0, []string{}},
		{"a negative age takes nothing", CheckpointCleanupRule{OlderThanDays: -5}, 0, []string{}},
	}
	for _, tc := range cases {
		got := checkpointIDs(selectCheckpointsForCleanup(list, now, tc.rule, tc.keep))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: picked %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The age falls back to the id when the date cannot be read, since the id is
// the creation time too.
func TestCheckpointCreatedFallsBackToTheID(t *testing.T) {
	created, ok := checkpointCreated(Checkpoint{ID: "20260101-000000.000000000-2"})
	if !ok || !created.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("created = %v, %v", created, ok)
	}
	if _, ok := checkpointCreated(Checkpoint{ID: "nonsense"}); ok {
		t.Error("an undatable checkpoint was given an age")
	}
}

func TestCleanUpDeletesOnlyWhatWasConfirmedAndStillMatches(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "a\n")
	dashboardGit(t, repo, "add", ".")
	dashboardGit(t, repo, "commit", "-m", "initial")
	first := newCheckpoint(t, repo, "one")
	writeRepoFile(t, repo, "a.txt", "b\n")
	second := newCheckpoint(t, repo, "two")
	writeRepoFile(t, repo, "a.txt", "c\n")
	third := newCheckpoint(t, repo, "three")

	// A checkpoint of another worktree shares the repository's refs; a cleanup
	// here must not reach it even when asked to by name.
	linked := filepath.Join(resolvedTempDir(t), "linked")
	dashboardGit(t, repo, "worktree", "add", "-b", "other", linked)
	foreign := newCheckpoint(t, linked, "elsewhere")

	ctx := context.Background()
	later := time.Now().Add(40 * checkpointDay)
	rule := CheckpointCleanupRule{OlderThanDays: 30}
	plan, err := planCheckpointCleanup(ctx, repo, rule, later)
	if err != nil {
		t.Fatal(err)
	}
	if got := checkpointIDs(plan); !reflect.DeepEqual(got, []string{third.ID, second.ID, first.ID}) {
		t.Fatalf("plan = %v", got)
	}
	if got := len(listedCheckpoints(t, repo)); got != 3 {
		t.Fatalf("planning deleted something: %d left", got)
	}

	confirmed := []CheckpointTarget{
		{ID: first.ID, Hash: first.Hash},
		// Confirmed with a hash the ref no longer holds: left alone.
		{ID: second.ID, Hash: first.Hash},
		{ID: foreign.ID, Hash: foreign.Hash},
	}
	deleted, err := cleanUpCheckpoints(ctx, repo, rule, confirmed, later)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("deleted %d, want 1", deleted)
	}
	if got := checkpointIDs(listedCheckpoints(t, repo)); !reflect.DeepEqual(got, []string{third.ID, second.ID}) {
		t.Errorf("left %v", got)
	}
	if got := checkpointIDs(listedCheckpoints(t, linked)); !reflect.DeepEqual(got, []string{foreign.ID}) {
		t.Errorf("the other worktree's checkpoints changed: %v", got)
	}

	// Confirmed, but the rule no longer takes it (not old enough now).
	deleted, err = cleanUpCheckpoints(ctx, repo, rule, []CheckpointTarget{{ID: third.ID, Hash: third.Hash}}, time.Now())
	if err != nil || deleted != 0 {
		t.Errorf("a checkpoint the rule does not take was deleted: %d, %v", deleted, err)
	}
	if got := len(listedCheckpoints(t, repo)); got != 2 {
		t.Errorf("%d left, want 2", got)
	}
}

func TestBeforeRestoreCleanupSparesARecentRestore(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "a\n")
	target := newCheckpoint(t, repo, "target")
	writeRepoFile(t, repo, "a.txt", "changed\n")
	result, err := restoreCheckpoint(context.Background(), repo, target.ID, "s")
	if err != nil {
		t.Fatal(err)
	}

	rule := CheckpointCleanupRule{BeforeRestoreOnly: true}
	plan, err := planCheckpointCleanup(context.Background(), repo, rule, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 0 {
		t.Errorf("a restore made minutes ago lost its undo: %v", checkpointIDs(plan))
	}
	plan, _ = planCheckpointCleanup(context.Background(), repo, rule, time.Now().Add(2*checkpointDay))
	if got := checkpointIDs(plan); !reflect.DeepEqual(got, []string{result.Before.ID}) {
		t.Errorf("two days on, plan = %v, want only the before-restore one", got)
	}
}

func TestPruneKeepsTheNewestWhateverTheirAge(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "a\n")
	var made []Checkpoint
	for i := 0; i < checkpointAutoPruneKeepNewest+2; i++ {
		made = append(made, newCheckpoint(t, repo, ""))
	}
	deleted, err := pruneCheckpoints(context.Background(), repo, 30, time.Now().Add(365*checkpointDay))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Errorf("deleted %d, want 2", deleted)
	}
	var want []string
	for i := len(made) - 1; i >= 2; i-- {
		want = append(want, made[i].ID)
	}
	if got := checkpointIDs(listedCheckpoints(t, repo)); !reflect.DeepEqual(got, want) {
		t.Errorf("left %v, want the newest %v", got, want)
	}
}

// A ref moved since it was listed survives, and the rest of the batch is
// still deleted.
func TestDeleteCheckpointRefsSkipsARefThatChanged(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "a\n")
	first := newCheckpoint(t, repo, "one")
	writeRepoFile(t, repo, "a.txt", "b\n")
	second := newCheckpoint(t, repo, "two")
	namespace, err := checkpointNamespace(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	dashboardGit(t, repo, "update-ref", namespace+first.ID, second.Hash, first.Hash)

	deleted, err := deleteCheckpointRefs(context.Background(), repo, namespace, []CheckpointTarget{
		{ID: first.ID, Hash: first.Hash},
		{ID: second.ID, Hash: second.Hash},
		{ID: "../../heads/main", Hash: second.Hash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("deleted %d, want 1", deleted)
	}
	out := dashboardGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/asmgr/")
	if !strings.Contains(out, first.ID) || strings.Contains(out, second.ID) {
		t.Errorf("refs left: %q", out)
	}
}

func TestAutoPruneRunsOnCreateOnlyWhenEnabled(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "a\n")
	app := checkpointTestApp(t, &session.Instance{ID: "cp", Name: "cp", Path: repo, Status: session.StatusStopped})
	project := app.storage.GetActiveProjectID()

	// Old checkpoints: the age is the committer date.
	t.Setenv("GIT_COMMITTER_DATE", "2020-01-01T00:00:00Z")
	for i := 0; i < checkpointAutoPruneKeepNewest+1; i++ {
		newCheckpoint(t, repo, "old")
	}
	os.Unsetenv("GIT_COMMITTER_DATE")

	if _, err := app.CreateCheckpoint("cp", -1, repo, "off", project); err != nil {
		t.Fatal(err)
	}
	if got := len(listedCheckpoints(t, repo)); got != checkpointAutoPruneKeepNewest+2 {
		t.Fatalf("with the setting off, %d left; nothing should have gone", got)
	}

	if err := app.storage.UpdateSettings(func(s *session.Settings) { s.CheckpointAutoPruneDays = 30 }); err != nil {
		t.Fatal(err)
	}
	if info, err := app.GetSettings(); err != nil || info.CheckpointAutoPruneDays != 30 {
		t.Fatalf("the setting does not reach the interface: %+v, %v", info, err)
	}
	created, err := app.CreateCheckpoint("cp", -1, repo, "on", project)
	if err != nil {
		t.Fatal(err)
	}
	left := listedCheckpoints(t, repo)
	if len(left) != checkpointAutoPruneKeepNewest {
		t.Fatalf("with the setting on, %d left, want %d", len(left), checkpointAutoPruneKeepNewest)
	}
	if left[0].ID != created.ID || left[1].Label != "off" {
		t.Errorf("the newest were not the ones kept: %+v", left[:2])
	}
}

func TestCleanUpAPIGuards(t *testing.T) {
	repo := checkpointTestRepo(t)
	writeRepoFile(t, repo, "a.txt", "a\n")
	other := checkpointTestRepo(t)
	app := checkpointTestApp(t, &session.Instance{ID: "cp", Name: "cp", Path: repo, Status: session.StatusStopped})
	project := app.storage.GetActiveProjectID()
	created, err := app.CreateCheckpoint("cp", -1, repo, "x", project)
	if err != nil {
		t.Fatal(err)
	}
	rule := CheckpointCleanupRule{OlderThanDays: 7}
	targets := []CheckpointTarget{{ID: created.ID, Hash: created.Hash}}

	if _, err := app.PlanCheckpointCleanup("cp", -1, other, rule); err == nil {
		t.Error("planning accepted a root that is not the tab's")
	}
	if _, err := app.PlanCheckpointCleanup("cp", -1, repo, CheckpointCleanupRule{}); err == nil {
		t.Error("planning accepted an empty rule")
	}
	if _, err := app.CleanUpCheckpoints("cp", -1, other, rule, targets, project); err == nil {
		t.Error("cleaning up accepted a root that is not the tab's")
	}
	if _, err := app.CleanUpCheckpoints("cp", -1, repo, rule, targets, "another-project"); err == nil {
		t.Error("cleaning up ignored the expected project")
	}
	plan, err := app.PlanCheckpointCleanup("cp", -1, repo, rule)
	if err != nil || plan == nil || len(plan) != 0 {
		t.Errorf("a fresh checkpoint was planned for cleanup: %v, %v", plan, err)
	}
	if deleted, err := app.CleanUpCheckpoints("cp", -1, repo, rule, targets, project); err != nil || deleted != 0 {
		t.Errorf("a fresh checkpoint was cleaned up: %d, %v", deleted, err)
	}
}
