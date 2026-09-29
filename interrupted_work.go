package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"asmgr-desktop/session"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Picking up where a restart left off.
//
// After a reboot every session reads as stopped, and the user used to start
// each one they had been working in by hand, tab by tab. The sidebar poll now
// records what is running (session.RunningSnapshot); on launch — and on
// switching to a project — the sessions that were running and were lost with
// the multiplexer are offered back, each with only the tabs that were running.
//
// Only the active project's: the poll watches the active project alone, and a
// project's sessions are started in that project. Another project's work is
// offered when that project is opened, which is also when the user is about to
// work in it.

// runningRecordInterval paces the poll's record of what is running. A tab
// started or stopped within this long of a power cut may come back the way it
// was before; the explicit stops below are recorded immediately regardless.
const runningRecordInterval = 5 * time.Second

// serverConnectBudget is how long the launch check waits for a server's
// connection before leaving that server's sessions for another time.
const serverConnectBudget = 10 * time.Second

// Restart behaviour, as stored in session.Settings.RestartReopen.
const (
	restartReopenAsk  = "ask"
	restartReopenAuto = "auto"
	restartReopenOff  = "off"
)

// restartReopenMode reads the stored setting, anything unknown being "ask".
func restartReopenMode(stored string) string {
	switch stored {
	case restartReopenAuto, restartReopenOff:
		return stored
	}
	return restartReopenAsk
}

// storedRestartReopen stores the default as empty, like the other defaults.
func storedRestartReopen(value string) string {
	switch value {
	case restartReopenAuto, restartReopenOff:
		return value
	}
	return ""
}

// ── Explicit stops ──────────────────────────────────────────────────────────

// runningStops remembers, briefly, what the user stopped on purpose.
//
// The poll decides what is running from a load made at the start of its pass;
// a stop that lands while the pass is still going would otherwise be undone by
// the pass writing its older view back. The pass skips anything stopped after
// it began.
type runningStops struct {
	mu sync.Mutex
	at map[string]time.Time
}

func runningStopKey(sessionID, tabID string) string {
	return sessionID + "\x00" + tabID
}

func (s *runningStops) note(sessionID, tabID string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.at == nil {
		s.at = map[string]time.Time{}
	}
	for key, when := range s.at {
		if at.Sub(when) > time.Minute {
			delete(s.at, key)
		}
	}
	s.at[runningStopKey(sessionID, tabID)] = at
}

// since reports whether the session (tabID "") or the tab was stopped after t.
func (s *runningStops) since(sessionID, tabID string, t time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	when, ok := s.at[runningStopKey(sessionID, tabID)]
	return ok && !when.Before(t)
}

// forgetStoppedSession takes a session the user stopped out of the record.
func (a *App) forgetStoppedSession(projectID, sessionID string) {
	a.runningStops.note(sessionID, "", time.Now())
	if err := a.storage.UpdateRunningSnapshot(projectID, func(snapshot *session.RunningSnapshot) bool {
		return snapshot.Forget(sessionID)
	}); err != nil {
		log.Printf("[running] could not forget stopped session %s: %v", sessionID, err)
	}
}

// forgetStoppedTab takes a tab the user stopped out of its session's record.
func (a *App) forgetStoppedTab(projectID, sessionID, tabID string) {
	if tabID == "" {
		return
	}
	a.runningStops.note(sessionID, tabID, time.Now())
	if err := a.storage.UpdateRunningSnapshot(projectID, func(snapshot *session.RunningSnapshot) bool {
		return snapshot.ForgetTab(sessionID, tabID)
	}); err != nil {
		log.Printf("[running] could not forget stopped tab %s/%s: %v", sessionID, tabID, err)
	}
}

// ── The poll's record ───────────────────────────────────────────────────────

// runningProbe is what the record needs to ask of a session, apart so the
// logic can be tested without a multiplexer.
type runningProbe interface {
	liveTabs(ctx context.Context, inst *session.Instance) ([]string, bool)
	identity(ctx context.Context, inst *session.Instance) string
	reachable(inst *session.Instance) bool
}

type multiplexerProbe struct{}

func (multiplexerProbe) liveTabs(ctx context.Context, inst *session.Instance) ([]string, bool) {
	return inst.LiveTabsContext(ctx)
}

func (multiplexerProbe) identity(ctx context.Context, inst *session.Instance) string {
	return inst.MultiplexerIdentityContext(ctx)
}

func (multiplexerProbe) reachable(inst *session.Instance) bool {
	return inst.MachineReachable()
}

// identityCache asks each machine for its multiplexer once per pass.
type identityCache struct {
	probe  runningProbe
	byHost map[string]string
}

func (c *identityCache) of(ctx context.Context, inst *session.Instance) string {
	if c.byHost == nil {
		c.byHost = map[string]string{}
	}
	if identity, ok := c.byHost[inst.ServerID]; ok {
		return identity
	}
	identity := c.probe.identity(ctx, inst)
	c.byHost[inst.ServerID] = identity
	return identity
}

// recordRunning updates the project's record from one poll's instances, whose
// statuses were read from the multiplexer at passStart.
//
//   - A running session is recorded with the tabs that have a live process.
//   - A recorded session that is no longer running is dropped only when its
//     machine's multiplexer is the very one it ran in: then it ended on its own.
//     A multiplexer gone or replaced — the machine going down, or the server
//     being killed — leaves the record for the next launch to offer. The poll
//     can be the last thing to run during a shutdown, and that must not be
//     what erases the work it is there to save.
//   - A session that no longer exists is dropped.
func recordRunning(ctx context.Context, storage *session.Storage, projectID string,
	instances []*session.Instance, probe runningProbe, stops *runningStops, passStart time.Time) error {
	current, err := storage.LoadRunningSnapshot(projectID)
	if err != nil {
		return err
	}
	identities := identityCache{probe: probe}

	type observed struct {
		record session.RunningRecord
		ok     bool
	}
	seen := map[string]observed{}
	ended := map[string]bool{}
	exists := map[string]bool{}
	for _, inst := range instances {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		exists[inst.ID] = true
		if inst.Status == session.StatusRunning {
			tabs, ok := probe.liveTabs(ctx, inst)
			if !ok {
				continue
			}
			seen[inst.ID] = observed{ok: true, record: session.RunningRecord{
				Tabs:        tabs,
				Multiplexer: identities.of(ctx, inst),
				SeenAt:      passStart,
			}}
			continue
		}
		record, recorded := current.Sessions[inst.ID]
		if !recorded {
			continue
		}
		verdict := session.JudgeInterruption(false, probe.reachable(inst), record.Multiplexer, identities.of(ctx, inst))
		if verdict == session.EndedOnItsOwn {
			ended[inst.ID] = true
		}
	}

	return storage.UpdateRunningSnapshot(projectID, func(snapshot *session.RunningSnapshot) bool {
		changed := false
		for id := range snapshot.Sessions {
			if !exists[id] || ended[id] {
				changed = snapshot.Forget(id) || changed
			}
		}
		for id, observation := range seen {
			if stops.since(id, "", passStart) {
				continue
			}
			record := observation.record
			var tabs []string
			for _, tab := range record.Tabs {
				if !stops.since(id, tab, passStart) {
					tabs = append(tabs, tab)
				}
			}
			record.Tabs = tabs
			changed = snapshot.RecordRunning(id, record) || changed
		}
		return changed
	})
}

// recordRunningFromPoll is the poll's call, paced by runningRecordInterval.
func (a *App) recordRunningFromPoll(ctx context.Context, projectID string, instances []*session.Instance, passStart time.Time) {
	if !a.runningSaves.allow(passStart) {
		return
	}
	if err := recordRunning(ctx, a.storage, projectID, instances, multiplexerProbe{}, &a.runningStops, passStart); err != nil && ctx.Err() == nil {
		log.Printf("[running] could not record running sessions: %v", err)
	}
}

// ── The offer ───────────────────────────────────────────────────────────────

// InterruptedSession is one session the restart interrupted, as the dialog
// lists it.
type InterruptedSession struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Agent string `json:"agent"`
	Color string `json:"color"`
	// ServerID is the machine it runs on; empty for this computer.
	ServerID string `json:"serverId"`
	// Agents are the agents of the tabs that will come back running, main
	// first, each once — the icons in the list.
	Agents []string `json:"agents"`
	// ReopenTabs of TotalTabs come back running; the rest come back parked.
	ReopenTabs int `json:"reopenTabs"`
	TotalTabs  int `json:"totalTabs"`
}

// InterruptedWork is what GetInterruptedWork found.
type InterruptedWork struct {
	ProjectID string `json:"projectId"`
	// Mode is the restart setting: "ask", "auto" or "off".
	Mode     string               `json:"mode"`
	Sessions []InterruptedSession `json:"sessions"`
}

// interruptedSessionInfo describes one session for the dialog.
func interruptedSessionInfo(inst *session.Instance, record session.RunningRecord) InterruptedSession {
	info := InterruptedSession{
		ID: inst.ID, Name: inst.Name, Path: inst.Path, Agent: string(inst.Agent),
		Color: inst.Color, ServerID: inst.ServerID,
		TotalTabs: 1 + len(inst.FollowedWindows),
	}
	listed := map[string]bool{}
	addAgent := func(agent session.AgentType) {
		name := string(agent)
		if name == "" {
			name = string(session.AgentClaude)
		}
		if !listed[name] {
			listed[name] = true
			info.Agents = append(info.Agents, name)
		}
	}
	if record.Has(session.MainTabID) {
		info.ReopenTabs++
		addAgent(inst.Agent)
	}
	for _, window := range inst.FollowedWindows {
		if record.Has(window.ID) {
			info.ReopenTabs++
			addAgent(window.Agent)
		}
	}
	return info
}

// findInterrupted sorts the recorded sessions into the ones to offer and the
// ones whose record is to be dropped. Instances come in the sidebar's order,
// with statuses already read; reachable and identity answer for their machine.
func findInterrupted(ctx context.Context, instances []*session.Instance, snapshot session.RunningSnapshot, probe runningProbe) (offer []InterruptedSession, drop []string) {
	identities := identityCache{probe: probe}
	exists := map[string]bool{}
	for _, inst := range instances {
		exists[inst.ID] = true
		record, ok := snapshot.Sessions[inst.ID]
		if !ok {
			continue
		}
		alive := inst.Status == session.StatusRunning
		reachable := probe.reachable(inst)
		current := ""
		if !alive && reachable {
			current = identities.of(ctx, inst)
		}
		switch session.JudgeInterruption(alive, reachable, record.Multiplexer, current) {
		case session.Interrupted:
			info := interruptedSessionInfo(inst, record)
			if info.ReopenTabs == 0 {
				// Every tab it had is gone from the session since.
				drop = append(drop, inst.ID)
				continue
			}
			offer = append(offer, info)
		case session.EndedOnItsOwn:
			drop = append(drop, inst.ID)
		}
	}
	for id := range snapshot.Sessions {
		if !exists[id] {
			drop = append(drop, id)
		}
	}
	return offer, drop
}

// connectRecordedServers opens the connections the recorded sessions on
// servers need, waiting at most serverConnectBudget. A server that does not
// answer in time leaves its sessions undecided rather than holding anything
// back.
//
// Called before the project lock is taken — a dial can take the full budget,
// and nothing else in the project should wait for it. The sessions are routed
// over the new connections by the next load, which routes every session it
// reads (session.RouteInstance).
func (a *App) connectRecordedServers(projectID string, only map[string]bool) {
	if a.servers == nil {
		return
	}
	snapshot, err := a.storage.LoadRunningSnapshot(projectID)
	if err != nil || len(snapshot.Sessions) == 0 {
		return
	}
	instances, _, err := a.storage.LoadAllForProject(projectID)
	if err != nil {
		return
	}
	servers := map[string]bool{}
	for _, inst := range instances {
		if _, recorded := snapshot.Sessions[inst.ID]; !recorded || (only != nil && !only[inst.ID]) {
			continue
		}
		if inst.ServerID != "" {
			servers[inst.ServerID] = true
		}
		for _, serverID := range tabServers(inst) {
			servers[serverID] = true
		}
	}
	if len(servers) == 0 {
		return
	}
	var wg sync.WaitGroup
	for serverID := range servers {
		wg.Add(1)
		go func(serverID string) {
			defer wg.Done()
			if _, err := a.connectionFor(serverID); err != nil {
				log.Printf("[running] server %s not reachable for the restart check: %v", serverID, err)
			}
		}(serverID)
	}
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(serverConnectBudget):
	}
}

// GetInterruptedWork lists the active project's sessions that were running
// when the machine (or its multiplexer) went away, with the restart setting.
//
// Empty on an ordinary relaunch, when the sessions are still running; on the
// very first launch, when nothing was ever recorded; and in a second window
// of the app, which may not start anything in a project the first one owns.
// With the setting "off" the record is dropped as well, so turning it back on
// later does not bring up work from long ago.
func (a *App) GetInterruptedWork() (*InterruptedWork, error) {
	if a.storage == nil {
		return &InterruptedWork{Mode: restartReopenAsk, Sessions: []InterruptedSession{}}, nil
	}
	a.connectRecordedServers(a.storage.GetActiveProjectID(), nil)

	done, err := a.beginProjectReadWithSideEffects()
	if err != nil {
		return &InterruptedWork{Mode: restartReopenAsk, Sessions: []InterruptedSession{}}, nil
	}
	defer done()

	// The project cannot change under the read lock held above.
	projectID := a.storage.GetActiveProjectID()
	instances, _, settings, err := a.storage.LoadAllWithSettings()
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = &session.Settings{}
	}
	work := &InterruptedWork{ProjectID: projectID, Mode: restartReopenMode(settings.RestartReopen)}

	snapshot, err := a.storage.LoadRunningSnapshot(projectID)
	if err != nil || len(snapshot.Sessions) == 0 {
		work.Sessions = []InterruptedSession{}
		return work, err
	}

	offer, drop := findInterrupted(context.Background(), instances, snapshot, multiplexerProbe{})
	if work.Mode == restartReopenOff {
		for _, info := range offer {
			drop = append(drop, info.ID)
		}
		offer = nil
	}
	if len(drop) > 0 {
		if err := a.storage.UpdateRunningSnapshot(projectID, func(snapshot *session.RunningSnapshot) bool {
			changed := false
			for _, id := range drop {
				changed = snapshot.Forget(id) || changed
			}
			return changed
		}); err != nil {
			log.Printf("[running] could not drop stale records: %v", err)
		}
	}
	work.Sessions = offer
	if work.Sessions == nil {
		work.Sessions = []InterruptedSession{}
	}
	return work, nil
}

// DismissInterruptedWork forgets the given sessions' record: "Not now", or
// the ones left unticked. They are not offered again until they run again.
func (a *App) DismissInterruptedWork(ids []string, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	return a.storage.UpdateRunningSnapshot(expectedProjectID, func(snapshot *session.RunningSnapshot) bool {
		changed := false
		for _, id := range ids {
			changed = snapshot.Forget(id) || changed
		}
		return changed
	})
}

// ── Reopening ──────────────────────────────────────────────────────────────

// ReopenResult is how one session's reopen went.
type ReopenResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// interruptedProgressEvent carries each ReopenResult as it is known.
const interruptedProgressEvent = "interrupted:progress"

// reopenEach reopens the sessions one after another, reporting each as it
// finishes. One failing does not stop the rest: a session whose directory is
// gone should not keep the other nine from coming back. Sequential because
// every start takes the project's mutation lock anyway.
func reopenEach(ctx context.Context, ids []string, reopen func(id string) error, progress func(ReopenResult)) []ReopenResult {
	results := make([]ReopenResult, 0, len(ids))
	for _, id := range ids {
		result := ReopenResult{ID: id, OK: true}
		if err := ctx.Err(); err != nil {
			result = ReopenResult{ID: id, Error: err.Error()}
		} else if err := reopen(id); err != nil {
			result = ReopenResult{ID: id, Error: err.Error()}
		}
		results = append(results, result)
		if progress != nil {
			progress(result)
		}
	}
	return results
}

// ReopenInterruptedSessions brings the chosen sessions back, each with only
// the tabs that were running, resuming each agent's conversation where one is
// known — what starting each by hand would do, without the hand.
//
// Progress is reported per session with interruptedProgressEvent; the results
// come back together at the end as well. A session that failed is dropped
// from the record, so a broken one does not come back on every launch; the
// ones that started are recorded again by the next poll.
func (a *App) ReopenInterruptedSessions(ids []string, expectedProjectID string) ([]ReopenResult, error) {
	snapshot, err := a.storage.LoadRunningSnapshot(expectedProjectID)
	if err != nil {
		return nil, err
	}
	chosen := map[string]bool{}
	for _, id := range ids {
		chosen[id] = true
	}
	a.connectRecordedServers(expectedProjectID, chosen)

	results := reopenEach(a.lifecycleContext(), ids, func(id string) error {
		record, ok := snapshot.Sessions[id]
		if !ok {
			return fmt.Errorf("error.notInterrupted")
		}
		return a.reopenInterruptedSession(id, record, expectedProjectID)
	}, func(result ReopenResult) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, interruptedProgressEvent, result)
		}
	})

	var failed []string
	for _, result := range results {
		if !result.OK {
			failed = append(failed, result.ID)
		}
	}
	if len(failed) > 0 {
		if err := a.storage.UpdateRunningSnapshot(expectedProjectID, func(snapshot *session.RunningSnapshot) bool {
			changed := false
			for _, id := range failed {
				changed = snapshot.Forget(id) || changed
			}
			return changed
		}); err != nil {
			log.Printf("[running] could not drop failed reopens: %v", err)
		}
	}
	return results, nil
}

// reopenInterruptedSession starts one session the way StartSessionWithResume
// does, with the recorded tabs running and the others parked.
func (a *App) reopenInterruptedSession(id string, record session.RunningRecord, expectedProjectID string) error {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return err
	}
	defer done()
	inst, err := a.storage.GetInstance(id)
	if err != nil {
		return err
	}
	inst.UpdateStatus()
	if inst.Status == session.StatusRunning {
		// Started some other way since the dialog opened; nothing to do.
		return nil
	}

	resumeID, clearSaved := resolveResumeID(inst.Agent, "", inst.ResumeSessionID, session.ResumeIDExists)
	if clearSaved {
		inst.ResumeSessionID = ""
	}
	log.Printf("[ReopenInterrupted] id=%s agent=%s resume=%t tabs=%d", id, inst.Agent, resumeID != "", len(record.Tabs))
	if err := inst.StartWithRunningTabs(resumeID, record); err != nil {
		return err
	}
	return persistOrRollbackExternalMutation(
		func() error { return a.storage.UpdateInstance(inst) },
		func() error { return inst.Stop() },
	)
}
