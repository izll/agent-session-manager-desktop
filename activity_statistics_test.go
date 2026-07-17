package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func newTestStatsRecorder(t *testing.T) *ActivityStatsRecorder {
	t.Helper()
	return &ActivityStatsRecorder{
		dir:    t.TempDir(),
		stores: make(map[string]*activityProjectStore),
		live:   make(map[string]*liveActivityState),
	}
}

func testObservation(activity string) activityObservation {
	return activityObservation{
		SessionID: "session-1", SessionName: "API", WindowIdx: 0,
		TabName: "Codex", Agent: "codex", Activity: activity,
	}
}

func TestActivityStatsConfirmsTransitionsAndAggregates(t *testing.T) {
	r := newTestStatsRecorder(t)
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.Local)

	r.Observe("project-a", start, []activityObservation{testObservation("idle")})
	r.Observe("project-a", start.Add(time.Second), []activityObservation{testObservation("busy")})
	r.Observe("project-a", start.Add(2*time.Second), []activityObservation{testObservation("busy")})
	r.Observe("project-a", start.Add(3*time.Second), []activityObservation{testObservation("busy")})
	r.Observe("project-a", start.Add(4*time.Second), []activityObservation{testObservation("waiting")})
	r.Observe("project-a", start.Add(5*time.Second), []activityObservation{testObservation("waiting")})
	r.Observe("project-a", start.Add(6*time.Second), []activityObservation{testObservation("waiting")})

	stats := r.Statistics("project-a", 7, start.Add(6*time.Second))
	if stats.Summary.ObservedMs != 6000 {
		t.Fatalf("observed = %d, want 6000", stats.Summary.ObservedMs)
	}
	if stats.Summary.IdleMs != 1000 || stats.Summary.BusyMs != 3000 || stats.Summary.WaitingMs != 2000 {
		t.Fatalf("unexpected durations: %+v", stats.Summary)
	}
	if stats.Summary.WaitingEvents != 1 {
		t.Fatalf("waiting events = %d, want 1", stats.Summary.WaitingEvents)
	}
	if len(stats.Agents) != 1 || stats.Agents[0].Agent != "codex" {
		t.Fatalf("unexpected agents: %+v", stats.Agents)
	}
}

func TestActivityStatsResetsTransitionAfterObservationGap(t *testing.T) {
	r := newTestStatsRecorder(t)
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.Local)

	r.Observe("", start, []activityObservation{testObservation("busy")})
	r.Observe("", start.Add(time.Second), []activityObservation{testObservation("waiting")})
	r.Observe("", start.Add(20*time.Second), []activityObservation{testObservation("waiting")})
	r.Observe("", start.Add(21*time.Second), []activityObservation{testObservation("waiting")})

	stats := r.Statistics("", 7, start.Add(21*time.Second))
	if stats.Summary.ObservedMs != 2000 || stats.Summary.BusyMs != 1000 || stats.Summary.WaitingMs != 1000 {
		t.Fatalf("gap handling is wrong: %+v", stats.Summary)
	}
	if stats.Summary.WaitingEvents != 0 {
		t.Fatalf("discontinuous samples created a waiting event: %+v", stats.Summary)
	}
}

func TestActivityStatsTabRenameKeepsContinuousRecord(t *testing.T) {
	r := newTestStatsRecorder(t)
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.Local)
	observation := testObservation("busy")

	r.Observe("project-a", start, []activityObservation{observation})
	observation.TabName = "Renamed Codex"
	r.Observe("project-a", start.Add(time.Second), []activityObservation{observation})
	r.Observe("project-a", start.Add(2*time.Second), []activityObservation{observation})

	stats := r.Statistics("project-a", 7, start.Add(2*time.Second))
	if stats.Summary.ObservedMs != 2000 || stats.Summary.BusyMs != 2000 {
		t.Fatalf("rename broke activity continuity: %+v", stats.Summary)
	}
	if len(stats.Sessions) != 1 {
		t.Fatalf("rename fragmented session statistics: %+v", stats.Sessions)
	}
}

func TestActivityStatsKeepsProjectFilesSeparate(t *testing.T) {
	r := newTestStatsRecorder(t)
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.Local)
	observation := testObservation("busy")

	r.Observe("one", start, []activityObservation{observation})
	r.Observe("one", start.Add(time.Second), []activityObservation{observation})
	r.Observe("two", start.Add(2*time.Second), []activityObservation{observation})
	r.Observe("two", start.Add(3*time.Second), []activityObservation{observation})
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	onePath := filepath.Join(r.dir, statsProjectFilename("one"))
	twoPath := filepath.Join(r.dir, statsProjectFilename("two"))
	if onePath == twoPath {
		t.Fatal("projects share a statistics path")
	}
	if _, err := os.Stat(onePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(twoPath); err != nil {
		t.Fatal(err)
	}
}

func TestStatsWriterLockKeepsCurrentProcessOwner(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "stats.writer-lock")
	if err := os.Mkdir(lockPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockPath, "pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	if staleStatsWriterLock(lockPath) {
		t.Fatal("current process writer lock was considered stale")
	}
}

func TestActivityStatsQuarantinesCorruptProjectFile(t *testing.T) {
	r := newTestStatsRecorder(t)
	path := filepath.Join(r.dir, statsProjectFilename("broken"))
	if err := os.WriteFile(path, []byte(`{"records":{"bad":null}}`), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.Local)
	r.Observe("broken", now, []activityObservation{testObservation("idle")})
	r.Observe("broken", now.Add(time.Second), []activityObservation{testObservation("idle")})
	stats := r.Statistics("broken", 7, now.Add(time.Second))
	if stats.Summary.ObservedMs != 1000 {
		t.Fatalf("recorder did not recover: %+v", stats.Summary)
	}
	matches, err := filepath.Glob(path + ".corrupt-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("corrupt file was not quarantined: %v, %v", matches, err)
	}
}

func TestActivityStatsCloseReleasesWriterAfterFlushError(t *testing.T) {
	r := newTestStatsRecorder(t)
	start := time.Now()
	r.Observe("project-a", start, []activityObservation{testObservation("busy")})
	r.Observe("project-a", start.Add(time.Second), []activityObservation{testObservation("busy")})

	store := r.stores["project-a"]
	lockPath := store.lockPath
	store.path = t.TempDir()
	if err := r.Close(); err == nil {
		t.Fatal("expected flush error")
	}
	if store.writer {
		t.Fatal("writer ownership was retained after failed close")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("writer lock still exists after failed close: %v", err)
	}
}

func TestActivityStatsCloseRejectsLateObservations(t *testing.T) {
	r := newTestStatsRecorder(t)
	start := time.Now().Add(-time.Second)
	r.Observe("", start, []activityObservation{testObservation("busy")})
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r.Observe("", time.Now(), []activityObservation{testObservation("busy")})

	loaded := newTestStatsRecorder(t)
	loaded.dir = r.dir
	stats := loaded.Statistics("", 7, time.Now())
	if stats.Summary.ObservedMs <= 0 || stats.Summary.ObservedMs > 5000 {
		t.Fatalf("unexpected final slice: %+v", stats.Summary)
	}
}

func TestActivityStatsSecondRecorderIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	first := &ActivityStatsRecorder{dir: dir, stores: make(map[string]*activityProjectStore), live: make(map[string]*liveActivityState)}
	second := &ActivityStatsRecorder{dir: dir, stores: make(map[string]*activityProjectStore), live: make(map[string]*liveActivityState)}
	start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.Local)

	first.Observe("same", start, []activityObservation{testObservation("busy")})
	first.Observe("same", start.Add(time.Second), []activityObservation{testObservation("busy")})
	second.Observe("same", start, []activityObservation{testObservation("waiting")})
	second.Observe("same", start.Add(time.Second), []activityObservation{testObservation("waiting")})
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	stats := second.Statistics("same", 7, start.Add(time.Second))
	if stats.Summary.BusyMs != 1000 || stats.Summary.WaitingMs != 0 {
		t.Fatalf("read-only recorder overwrote writer data: %+v", stats.Summary)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}
