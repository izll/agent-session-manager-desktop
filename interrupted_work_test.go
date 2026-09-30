package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

// fakeProbe answers for sessions without a multiplexer: which tabs are live,
// which machine answers with which server identity, which are unreachable.
type fakeProbe struct {
	live        map[string][]string // session ID → live tabs
	identities  map[string]string   // server ID ("" local) → identity
	unreachable map[string]bool     // server ID → not connected
}

func (p fakeProbe) liveTabs(_ context.Context, inst *session.Instance) ([]string, bool) {
	tabs, ok := p.live[inst.ID]
	return tabs, ok
}

func (p fakeProbe) identity(_ context.Context, inst *session.Instance) string {
	return p.identities[inst.ServerID]
}

func (p fakeProbe) reachable(inst *session.Instance) bool {
	return !p.unreachable[inst.ServerID]
}

func running(id string) *session.Instance {
	return &session.Instance{ID: id, Name: id, Status: session.StatusRunning}
}

func stopped(id string) *session.Instance {
	return &session.Instance{ID: id, Name: id, Status: session.StatusStopped}
}

func testRunningStorage(t *testing.T) *session.Storage {
	t.Helper()
	isolateHomeForTest(t)
	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	return storage
}

func seedRunning(t *testing.T, storage *session.Storage, records map[string]session.RunningRecord) {
	t.Helper()
	if err := storage.UpdateRunningSnapshot("", func(s *session.RunningSnapshot) bool {
		for id, record := range records {
			s.Sessions[id] = record
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
}

func loadRunning(t *testing.T, storage *session.Storage) map[string]session.RunningRecord {
	t.Helper()
	snapshot, err := storage.LoadRunningSnapshot("")
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.Sessions
}

// ── The poll's record ───────────────────────────────────────────────────────

func TestRecordRunningWritesTheLiveTabsOfRunningSessions(t *testing.T) {
	storage := testRunningStorage(t)
	probe := fakeProbe{
		live:       map[string][]string{"a": {session.MainTabID, "t1"}, "b": {"t2"}},
		identities: map[string]string{"": "10 100"},
	}
	start := time.Now()
	if err := recordRunning(context.Background(), storage, "", []*session.Instance{running("a"), running("b"), stopped("c")},
		probe, &runningStops{}, start); err != nil {
		t.Fatal(err)
	}
	got := loadRunning(t, storage)
	if len(got) != 2 {
		t.Fatalf("recorded %v, want a and b", got)
	}
	if !reflect.DeepEqual(got["a"].Tabs, []string{session.MainTabID, "t1"}) || got["a"].Multiplexer != "10 100" {
		t.Errorf("a = %+v", got["a"])
	}
	if !reflect.DeepEqual(got["b"].Tabs, []string{"t2"}) {
		t.Errorf("b = %+v: only its running tab should be recorded", got["b"])
	}
}

// The heart of it: the poll can run once more while the machine shuts down,
// seeing everything gone. That must not erase the record; only a session
// ended while its own multiplexer carries on is dropped.
func TestRecordRunningKeepsWhatAShutdownTookAway(t *testing.T) {
	storage := testRunningStorage(t)
	seedRunning(t, storage, map[string]session.RunningRecord{
		"lost":    {Tabs: []string{session.MainTabID}, Multiplexer: "10 100"},
		"killed":  {Tabs: []string{session.MainTabID}, Multiplexer: "20 200", SeenAt: time.Unix(1, 0)},
		"remote":  {Tabs: []string{session.MainTabID}, Multiplexer: "30 300"},
		"deleted": {Tabs: []string{session.MainTabID}},
	})
	remote := stopped("remote")
	remote.ServerID = "srv"
	killed := stopped("killed")
	killed.ServerID = "other"

	probe := fakeProbe{
		// This computer's server is gone (shutdown); "other" still runs the
		// same server the killed session was in; "srv" is not connected.
		identities:  map[string]string{"": "", "other": "20 200"},
		unreachable: map[string]bool{"srv": true},
	}
	if err := recordRunning(context.Background(), storage, "", []*session.Instance{stopped("lost"), killed, remote},
		probe, &runningStops{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	got := loadRunning(t, storage)
	if _, ok := got["lost"]; !ok {
		t.Error("a session lost with the multiplexer was erased by the poll")
	}
	if _, ok := got["remote"]; !ok {
		t.Error("a session on an unreachable server was erased on a guess")
	}
	if _, ok := got["killed"]; ok {
		t.Error("a session ended alone, its server still the same, is still recorded")
	}
	if _, ok := got["deleted"]; ok {
		t.Error("a session that no longer exists is still recorded")
	}
}

// A stop landing while a pass is out must win over what the pass saw.
func TestRecordRunningDoesNotUndoAStopMadeDuringThePass(t *testing.T) {
	storage := testRunningStorage(t)
	probe := fakeProbe{live: map[string][]string{"a": {session.MainTabID, "t1"}, "b": {session.MainTabID}}}
	stops := &runningStops{}
	passStart := time.Now()
	stops.note("b", "", passStart.Add(time.Millisecond))
	stops.note("a", "t1", passStart.Add(time.Millisecond))

	if err := recordRunning(context.Background(), storage, "", []*session.Instance{running("a"), running("b")},
		probe, stops, passStart); err != nil {
		t.Fatal(err)
	}
	got := loadRunning(t, storage)
	if _, ok := got["b"]; ok {
		t.Error("the session stopped during the pass was written back")
	}
	if !reflect.DeepEqual(got["a"].Tabs, []string{session.MainTabID}) {
		t.Errorf("a = %v: the tab stopped during the pass was written back", got["a"].Tabs)
	}

	// A stop from before the pass is history: the session runs again now.
	later := time.Now().Add(time.Second)
	if err := recordRunning(context.Background(), storage, "", []*session.Instance{running("b")},
		probe, stops, later); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadRunning(t, storage)["b"]; !ok {
		t.Error("a session started again after its stop was not recorded")
	}
}

// ── Explicit stop ───────────────────────────────────────────────────────────

// A failing multiplexer shim: nothing in these tests may reach a real server.
func failingTmux(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell shim")
	}
	previous := session.TmuxBinary()
	t.Cleanup(func() { session.SetTmuxBinary(previous) })
	shim := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	session.SetTmuxBinary(shim)
}

func TestStopSessionTakesItOutOfTheRecord(t *testing.T) {
	failingTmux(t)
	storage := testRunningStorage(t)
	inst := &session.Instance{ID: "s1", Name: "s1", Path: t.TempDir(), Status: session.StatusStopped, Agent: session.AgentTerminal}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	seedRunning(t, storage, map[string]session.RunningRecord{"s1": {Tabs: []string{session.MainTabID}}})

	app := &App{storage: storage, projectLocked: true}
	if err := app.StopSession("s1", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadRunning(t, storage)["s1"]; ok {
		t.Fatal("a session the user stopped would be offered back after a restart")
	}
	if !app.runningStops.since("s1", "", time.Now().Add(-time.Minute)) {
		t.Error("the stop was not noted for a poll pass already out")
	}
}

// StopTab needs a live pane to stop, so its wiring is checked in the source:
// the tab is named before the stop and taken out of the record after it.
func TestStopTabTakesTheTabOutOfTheRecord(t *testing.T) {
	// readTextFile, not os.ReadFile: a Windows checkout has CRLF, and the
	// end of the function is looked up as "\n}\n".
	body := readTextFile(t, "app.go")
	at := strings.Index(body, "func (a *App) StopTab(")
	if at < 0 {
		t.Fatal("StopTab not found")
	}
	body = body[at:]
	body = body[:strings.Index(body, "\n}\n")]
	named := strings.Index(body, "inst.GetFollowedWindow(windowIdx)")
	stop := strings.Index(body, "inst.StopWindow(windowIdx)")
	forget := strings.Index(body, "a.forgetStoppedTab(expectedProjectID, id, tabID)")
	if named < 0 || stop < 0 || forget < 0 || !(named < stop && stop < forget) {
		t.Fatal("StopTab no longer takes the stopped tab out of the running record, " +
			"so a tab the user stopped comes back running after a reboot")
	}
}

// ── What is offered ─────────────────────────────────────────────────────────

func TestFindInterruptedOffersOnlyWhatTheRestartTook(t *testing.T) {
	five := stopped("five")
	five.Agent = session.AgentClaude
	five.FollowedWindows = []session.FollowedWindow{
		{ID: "t1", Agent: session.AgentCodex},
		{ID: "t2", Agent: session.AgentClaude},
		{ID: "t3", Agent: session.AgentTerminal},
		{ID: "t4", Agent: session.AgentGemini},
	}
	snapshot := session.RunningSnapshot{Sessions: map[string]session.RunningRecord{
		// Three of five running: the main agent and two tabs.
		"five":   {Tabs: []string{session.MainTabID, "t1", "t2"}, Multiplexer: "10 100"},
		"alive":  {Tabs: []string{session.MainTabID}, Multiplexer: "10 100"},
		"killed": {Tabs: []string{session.MainTabID}, Multiplexer: "20 200"},
		"remote": {Tabs: []string{session.MainTabID}},
		"closed": {Tabs: []string{"no-such-tab"}},
		"gone":   {Tabs: []string{session.MainTabID}},
	}}
	killed := stopped("killed")
	killed.ServerID = "b"
	remote := stopped("remote")
	remote.ServerID = "down"
	instances := []*session.Instance{five, running("alive"), killed, remote, stopped("closed"), stopped("never")}
	probe := fakeProbe{
		identities:  map[string]string{"": "55 999", "b": "20 200"},
		unreachable: map[string]bool{"down": true},
	}

	offer, drop := findInterrupted(context.Background(), instances, snapshot, probe)
	if len(offer) != 1 || offer[0].ID != "five" {
		t.Fatalf("offered %+v, want only the session the reboot took", offer)
	}
	got := offer[0]
	if got.ReopenTabs != 3 || got.TotalTabs != 5 {
		t.Errorf("tabs = %d of %d, want 3 of 5", got.ReopenTabs, got.TotalTabs)
	}
	if !reflect.DeepEqual(got.Agents, []string{"claude", "codex"}) {
		t.Errorf("agents = %v, want each running agent once, main first", got.Agents)
	}

	dropped := map[string]bool{}
	for _, id := range drop {
		dropped[id] = true
	}
	for _, id := range []string{"killed", "closed", "gone"} {
		if !dropped[id] {
			t.Errorf("%s should have been dropped", id)
		}
	}
	for _, id := range []string{"alive", "remote", "five"} {
		if dropped[id] {
			t.Errorf("%s was dropped", id)
		}
	}
}

func TestGetInterruptedWorkHonoursTheSetting(t *testing.T) {
	failingTmux(t) // no server: every recorded local session was lost with it
	for _, tc := range []struct {
		stored     string
		mode       string
		offered    int
		keepsTrace bool
	}{
		{"", restartReopenAsk, 1, true},
		{restartReopenAuto, restartReopenAuto, 1, true},
		{restartReopenOff, restartReopenOff, 0, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			storage := testRunningStorage(t)
			inst := &session.Instance{ID: "s1", Name: "s1", Path: t.TempDir(), Status: session.StatusRunning, Agent: session.AgentTerminal}
			if err := storage.AddInstance(inst); err != nil {
				t.Fatal(err)
			}
			if err := storage.UpdateSettings(func(s *session.Settings) { s.RestartReopen = tc.stored }); err != nil {
				t.Fatal(err)
			}
			seedRunning(t, storage, map[string]session.RunningRecord{"s1": {Tabs: []string{session.MainTabID}, Multiplexer: "10 100"}})

			app := &App{storage: storage, projectLocked: true}
			work, err := app.GetInterruptedWork()
			if err != nil {
				t.Fatal(err)
			}
			if work.Mode != tc.mode || len(work.Sessions) != tc.offered {
				t.Fatalf("mode %q offered %d, want %q offering %d", work.Mode, len(work.Sessions), tc.mode, tc.offered)
			}
			if _, kept := loadRunning(t, storage)["s1"]; kept != tc.keepsTrace {
				t.Errorf("record kept = %v, want %v", kept, tc.keepsTrace)
			}
		})
	}
}

// The first launch has nothing recorded, and a second window of the app may
// not start anything in a project the first one owns: neither is offered.
func TestGetInterruptedWorkOffersNothingWithoutARecordOrTheLock(t *testing.T) {
	failingTmux(t)
	storage := testRunningStorage(t)
	inst := &session.Instance{ID: "s1", Name: "s1", Path: t.TempDir(), Agent: session.AgentTerminal}
	if err := storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}

	app := &App{storage: storage, projectLocked: true}
	work, err := app.GetInterruptedWork()
	if err != nil || len(work.Sessions) != 0 {
		t.Fatalf("first launch offered %+v (%v)", work, err)
	}

	seedRunning(t, storage, map[string]session.RunningRecord{"s1": {Tabs: []string{session.MainTabID}}})
	readOnly := &App{storage: storage}
	work, err = readOnly.GetInterruptedWork()
	if err != nil || len(work.Sessions) != 0 {
		t.Fatalf("a read-only window offered %+v (%v)", work, err)
	}
}

func TestDismissInterruptedWorkForgetsOnlyThoseSessions(t *testing.T) {
	storage := testRunningStorage(t)
	seedRunning(t, storage, map[string]session.RunningRecord{
		"a": {Tabs: []string{session.MainTabID}},
		"b": {Tabs: []string{session.MainTabID}},
	})
	app := &App{storage: storage, projectLocked: true}
	if err := app.DismissInterruptedWork([]string{"a"}, ""); err != nil {
		t.Fatal(err)
	}
	got := loadRunning(t, storage)
	if _, ok := got["a"]; ok {
		t.Error("\"Not now\" did not stop the offer")
	}
	if _, ok := got["b"]; !ok {
		t.Error("a session not dismissed was forgotten")
	}
}

// ── Reopening ──────────────────────────────────────────────────────────────

func TestReopenEachCarriesOnPastAFailure(t *testing.T) {
	var started, reported []string
	results := reopenEach(context.Background(), []string{"a", "b", "c"}, func(id string) error {
		started = append(started, id)
		if id == "b" {
			return errors.New("directory is gone")
		}
		return nil
	}, func(r ReopenResult) {
		reported = append(reported, r.ID)
	})

	if !reflect.DeepEqual(started, []string{"a", "b", "c"}) {
		t.Errorf("started %v: one failure must not stop the rest", started)
	}
	if !reflect.DeepEqual(reported, started) {
		t.Errorf("progress reported %v, want each as it finished", reported)
	}
	want := []ReopenResult{{ID: "a", OK: true}, {ID: "b", Error: "directory is gone"}, {ID: "c", OK: true}}
	if !reflect.DeepEqual(results, want) {
		t.Errorf("results = %+v, want %+v", results, want)
	}
}

func TestReopenEachStopsStartingWhenTheAppCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var started []string
	results := reopenEach(ctx, []string{"a", "b"}, func(id string) error {
		started = append(started, id)
		cancel()
		return nil
	}, nil)
	if !reflect.DeepEqual(started, []string{"a"}) {
		t.Errorf("started %v after shutdown began", started)
	}
	if results[1].OK || results[1].Error == "" {
		t.Errorf("the session not started reads %+v", results[1])
	}
}

// A session that fails to come back is not offered on every launch after.
func TestReopenInterruptedSessionsDropsTheFailedOnes(t *testing.T) {
	failingTmux(t)
	storage := testRunningStorage(t)
	seedRunning(t, storage, map[string]session.RunningRecord{"missing": {Tabs: []string{session.MainTabID}}})
	app := &App{storage: storage, projectLocked: true}

	results, err := app.ReopenInterruptedSessions([]string{"missing", "unrecorded"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].OK || results[1].OK {
		t.Fatalf("results = %+v", results)
	}
	if results[1].Error != "error.notInterrupted" {
		t.Errorf("an unrecorded session reports %q", results[1].Error)
	}
	if _, ok := loadRunning(t, storage)["missing"]; ok {
		t.Error("a session that could not be reopened stays on offer")
	}
}

// ── The setting ─────────────────────────────────────────────────────────────

func TestRestartReopenSettingRoundTrips(t *testing.T) {
	storage := testRunningStorage(t)
	app := &App{storage: storage, projectLocked: true}

	info, err := app.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if info.RestartReopen != restartReopenAsk {
		t.Fatalf("default = %q, want %q", info.RestartReopen, restartReopenAsk)
	}
	for _, mode := range []string{restartReopenAuto, restartReopenOff, restartReopenAsk} {
		info.RestartReopen = mode
		if err := app.SaveSettings(*info, ""); err != nil {
			t.Fatal(err)
		}
		got, err := app.GetSettings()
		if err != nil {
			t.Fatal(err)
		}
		if got.RestartReopen != mode {
			t.Errorf("saved %q, read back %q", mode, got.RestartReopen)
		}
	}
	info.RestartReopen = "sometimes"
	_ = app.SaveSettings(*info, "")
	if got, _ := app.GetSettings(); got.RestartReopen != restartReopenAsk {
		t.Errorf("an unknown value reads as %q, want the default", got.RestartReopen)
	}
}
