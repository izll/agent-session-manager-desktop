package main

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
	"time"
)

// Checkpoint cleanup: checkpoints cost little — git stores only what changed —
// but they are never removed by themselves, and every restore adds one more.
// Over months the list grows past the point of being useful.
//
// Two ways out, sharing one set of rules:
//
//   - By hand, from the dialog: "older than N days", or the automatic
//     "before restore" ones. The dialog asks the backend which checkpoints the
//     rule picks, shows how many, and on confirmation hands the same list
//     back. Only checkpoints that were both confirmed and still match the rule
//     are deleted, each only if its ref still points where it did.
//   - Automatically, if the user opts in: after a checkpoint is taken, the
//     ones older than the configured age go. A checkpoint is the undo, and
//     silently deleting an undo must be something the user asked for, so this
//     is off by default.
//
// Either way only refs under this work tree's own namespace are touched, and
// only refs are deleted: the commits become unreachable, and git's own garbage
// collection takes them in due course. Running gc here would be slow, and
// would take away the grace period in which a deleted ref can still be
// recovered from the object database.

const (
	// A "before restore" checkpoint is the undo of a restore. Within a day of
	// it the restore may still turn out to have been a mistake, so no bulk
	// cleanup takes it — only deleting that one row by hand does.
	checkpointBeforeRestoreGrace = 24 * time.Hour

	// Automatic pruning never leaves a work tree with nothing: however old they
	// are, the newest few stay. Someone coming back to a project after months
	// away still finds where it was left.
	checkpointAutoPruneKeepNewest = 5

	checkpointDay = 24 * time.Hour
)

var checkpointHashPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// CheckpointCleanupRule says which checkpoints a cleanup takes.
type CheckpointCleanupRule struct {
	// OlderThanDays takes the checkpoints at least this many days old. With
	// BeforeRestoreOnly it may be 0: every "before restore" checkpoint past
	// the grace period.
	OlderThanDays int `json:"olderThanDays"`
	// BeforeRestoreOnly limits the cleanup to the checkpoints restores made.
	BeforeRestoreOnly bool `json:"beforeRestoreOnly"`
}

// CheckpointTarget names a checkpoint and the commit its ref held when the
// user was shown it.
type CheckpointTarget struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

func validateCleanupRule(rule CheckpointCleanupRule) error {
	if rule.OlderThanDays < 0 || (rule.OlderThanDays == 0 && !rule.BeforeRestoreOnly) {
		return errors.New("error.checkpointCleanupRule")
	}
	return nil
}

// PlanCheckpointCleanup returns the checkpoints a cleanup rule would delete,
// newest first, without deleting anything.
func (a *App) PlanCheckpointCleanup(sessionID string, windowIdx int, expectedRoot string, rule CheckpointCleanupRule) ([]Checkpoint, error) {
	if err := validateCleanupRule(rule); err != nil {
		return nil, err
	}
	_, root, err := a.checkpointRoot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return nil, err
	}
	checkpointMu.Lock()
	defer checkpointMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), checkpointTimeout)
	defer cancel()
	return planCheckpointCleanup(ctx, root, rule, time.Now())
}

// CleanUpCheckpoints deletes the confirmed checkpoints that still match the
// rule, and returns how many it deleted.
func (a *App) CleanUpCheckpoints(sessionID string, windowIdx int, expectedRoot string, rule CheckpointCleanupRule, confirmed []CheckpointTarget, expectedProjectID string) (int, error) {
	if err := validateCleanupRule(rule); err != nil {
		return 0, err
	}
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return 0, err
	}
	defer done()
	_, root, err := a.checkpointRoot(sessionID, windowIdx, expectedRoot)
	if err != nil {
		return 0, err
	}
	checkpointMu.Lock()
	defer checkpointMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), checkpointTimeout)
	defer cancel()
	return cleanUpCheckpoints(ctx, root, rule, confirmed, time.Now())
}

// checkpointAutoPruneDays is the configured automatic cleanup age, or 0 when
// it is off (the default) or the settings cannot be read.
func (a *App) checkpointAutoPruneDays() int {
	if a.storage == nil {
		return 0
	}
	_, _, settings, err := a.storage.LoadAllWithSettings()
	if err != nil || settings == nil || settings.CheckpointAutoPruneDays <= 0 {
		return 0
	}
	return settings.CheckpointAutoPruneDays
}

// autoPruneCheckpoints runs the automatic cleanup after a checkpoint was
// taken. The caller holds checkpointMu and the project mutation. A failure is
// only logged: the checkpoint the user asked for exists, and a cleanup that
// could not run is no reason to report that it does not.
func autoPruneCheckpoints(ctx context.Context, top string, days int) {
	if days <= 0 {
		return
	}
	if _, err := pruneCheckpoints(ctx, top, days, time.Now()); err != nil {
		log.Printf("checkpoints: automatic cleanup in %s: %v", top, err)
	}
}

func pruneCheckpoints(ctx context.Context, top string, days int, now time.Time) (int, error) {
	namespace, err := checkpointNamespace(ctx, top)
	if err != nil {
		return 0, err
	}
	entries, err := readCheckpointRefs(ctx, top, namespace)
	if err != nil {
		return 0, err
	}
	doomed := selectCheckpointsForCleanup(entryCheckpoints(entries), now,
		CheckpointCleanupRule{OlderThanDays: days}, checkpointAutoPruneKeepNewest)
	return deleteCheckpointRefs(ctx, top, namespace, checkpointTargets(doomed))
}

func planCheckpointCleanup(ctx context.Context, top string, rule CheckpointCleanupRule, now time.Time) ([]Checkpoint, error) {
	namespace, err := checkpointNamespace(ctx, top)
	if err != nil {
		return nil, err
	}
	entries, err := readCheckpointRefs(ctx, top, namespace)
	if err != nil {
		return nil, err
	}
	planned := selectCheckpointsForCleanup(entryCheckpoints(entries), now, rule, 0)
	if planned == nil {
		planned = []Checkpoint{}
	}
	return planned, nil
}

// cleanUpCheckpoints deletes what the user confirmed, but only where the rule
// still agrees: the list is re-read, so a checkpoint that changed or stopped
// matching between the question and the answer is left alone.
func cleanUpCheckpoints(ctx context.Context, top string, rule CheckpointCleanupRule, confirmed []CheckpointTarget, now time.Time) (int, error) {
	if len(confirmed) == 0 {
		return 0, nil
	}
	namespace, err := checkpointNamespace(ctx, top)
	if err != nil {
		return 0, err
	}
	entries, err := readCheckpointRefs(ctx, top, namespace)
	if err != nil {
		return 0, err
	}
	wanted := map[CheckpointTarget]bool{}
	for _, target := range confirmed {
		wanted[target] = true
	}
	var targets []CheckpointTarget
	for _, checkpoint := range selectCheckpointsForCleanup(entryCheckpoints(entries), now, rule, 0) {
		target := CheckpointTarget{ID: checkpoint.ID, Hash: checkpoint.Hash}
		if wanted[target] {
			targets = append(targets, target)
		}
	}
	return deleteCheckpointRefs(ctx, top, namespace, targets)
}

func entryCheckpoints(entries []checkpointRefEntry) []Checkpoint {
	checkpoints := make([]Checkpoint, len(entries))
	for i, e := range entries {
		checkpoints[i] = e.checkpoint
	}
	return checkpoints
}

func checkpointTargets(checkpoints []Checkpoint) []CheckpointTarget {
	targets := make([]CheckpointTarget, len(checkpoints))
	for i, c := range checkpoints {
		targets[i] = CheckpointTarget{ID: c.ID, Hash: c.Hash}
	}
	return targets
}

// checkpointCreated is when a checkpoint was taken: its committer date, or
// failing that the time its id spells.
func checkpointCreated(c Checkpoint) (time.Time, bool) {
	if created, err := time.Parse(time.RFC3339, c.Created); err == nil {
		return created, true
	}
	// The id may carry a "-N" suffix after the timestamp; the time is the
	// fixed-width part before it.
	if len(c.ID) >= len(checkpointIDLayout) {
		if created, err := time.ParseInLocation(checkpointIDLayout, c.ID[:len(checkpointIDLayout)], time.UTC); err == nil {
			return created, true
		}
	}
	return time.Time{}, false
}

// selectCheckpointsForCleanup picks, from checkpoints listed newest first,
// those a rule takes.
//
//   - The first keepNewest are never taken, whatever their age.
//   - A checkpoint whose age cannot be told is never taken.
//   - A "before restore" checkpoint younger than the grace period is never
//     taken: it is the way back from a restore that may have been a mistake.
func selectCheckpointsForCleanup(checkpoints []Checkpoint, now time.Time, rule CheckpointCleanupRule, keepNewest int) []Checkpoint {
	if validateCleanupRule(rule) != nil {
		return nil
	}
	var picked []Checkpoint
	for i, c := range checkpoints {
		if i < keepNewest {
			continue
		}
		created, ok := checkpointCreated(c)
		if !ok {
			continue
		}
		age := now.Sub(created)
		beforeRestore := c.Kind == checkpointKindBeforeRestore
		if beforeRestore && age < checkpointBeforeRestoreGrace {
			continue
		}
		if rule.BeforeRestoreOnly && !beforeRestore {
			continue
		}
		if rule.OlderThanDays > 0 && age < time.Duration(rule.OlderThanDays)*checkpointDay {
			continue
		}
		picked = append(picked, c)
	}
	return picked
}

// deleteCheckpointRefs deletes checkpoint refs of one namespace, each only if
// it still holds the commit it held when it was listed, so a ref replaced in
// the meantime survives. It returns how many were deleted.
//
// One `update-ref --stdin` transaction does them all. A transaction fails as a
// whole, though, so if any one ref had changed, they are tried one at a time
// and the rest still go.
func deleteCheckpointRefs(ctx context.Context, top, namespace string, targets []CheckpointTarget) (int, error) {
	var valid []CheckpointTarget
	for _, target := range targets {
		if validateCheckpointID(target.ID) != nil || !checkpointHashPattern.MatchString(target.Hash) {
			continue
		}
		valid = append(valid, target)
	}
	if len(valid) == 0 {
		return 0, nil
	}
	var input strings.Builder
	for _, target := range valid {
		input.WriteString("delete " + namespace + target.ID + " " + target.Hash + "\n")
	}
	if _, err := checkpointGit(ctx, top, nil, []byte(input.String()), "update-ref", "--stdin"); err == nil {
		return len(valid), nil
	} else if ctx.Err() != nil {
		return 0, err
	}
	deleted := 0
	var firstErr error
	for _, target := range valid {
		if _, err := checkpointGit(ctx, top, nil, nil, "update-ref", "-d", namespace+target.ID, target.Hash); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if ctx.Err() != nil {
				break
			}
			continue
		}
		deleted++
	}
	// A ref that changed is skipped by design; only a cleanup that got nowhere
	// at all is an error worth showing.
	if deleted == 0 && firstErr != nil {
		return 0, firstErr
	}
	return deleted, nil
}
