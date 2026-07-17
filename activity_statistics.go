package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	activityStatsVersion   = 1
	activityStatsRetention = 90
	activityStatsMaxGap    = 5 * time.Second
	activityStatsFlushRate = 30 * time.Second
)

type activityObservation struct {
	SessionID   string
	SessionName string
	WindowIdx   int
	TabName     string
	Agent       string
	Activity    string
}

type activityStatsRecord struct {
	Date            string `json:"date"`
	SessionID       string `json:"sessionId"`
	SessionName     string `json:"sessionName"`
	WindowIdx       int    `json:"windowIdx"`
	TabName         string `json:"tabName"`
	Agent           string `json:"agent"`
	FirstObservedAt string `json:"firstObservedAt"`
	LastObservedAt  string `json:"lastObservedAt"`
	ObservedMs      int64  `json:"observedMs"`
	BusyMs          int64  `json:"busyMs"`
	WaitingMs       int64  `json:"waitingMs"`
	IdleMs          int64  `json:"idleMs"`
	WaitingEvents   int64  `json:"waitingEvents"`
}

type activityStatsFile struct {
	Version   int                             `json:"version"`
	UpdatedAt string                          `json:"updatedAt"`
	Records   map[string]*activityStatsRecord `json:"records"`
}

type activityProjectStore struct {
	path      string
	data      activityStatsFile
	dirty     bool
	lastFlush time.Time
	writer    bool
	lockPath  string
}

type liveActivityState struct {
	activity       string
	candidate      string
	lastObservedAt time.Time
	observation    activityObservation
}

// ActivityStatsRecorder keeps only aggregate durations. No terminal output,
// prompts, status lines, or conversation text are persisted.
type ActivityStatsRecorder struct {
	mu     sync.Mutex
	dir    string
	stores map[string]*activityProjectStore
	live   map[string]*liveActivityState
	closed bool
}

var statsWriterLocks = struct {
	sync.Mutex
	paths map[string]bool
}{paths: make(map[string]bool)}

type ActivityStatsSummary struct {
	ObservedMs    int64   `json:"observedMs"`
	BusyMs        int64   `json:"busyMs"`
	WaitingMs     int64   `json:"waitingMs"`
	IdleMs        int64   `json:"idleMs"`
	WaitingEvents int64   `json:"waitingEvents"`
	BusyPercent   float64 `json:"busyPercent"`
}

type ActivityStatsDay struct {
	Date      string `json:"date"`
	BusyMs    int64  `json:"busyMs"`
	WaitingMs int64  `json:"waitingMs"`
	IdleMs    int64  `json:"idleMs"`
}

type ActivityStatsAgent struct {
	Agent         string  `json:"agent"`
	ObservedMs    int64   `json:"observedMs"`
	BusyMs        int64   `json:"busyMs"`
	WaitingMs     int64   `json:"waitingMs"`
	IdleMs        int64   `json:"idleMs"`
	WaitingEvents int64   `json:"waitingEvents"`
	SharePercent  float64 `json:"sharePercent"`
}

type ActivityStatsSession struct {
	SessionID     string `json:"sessionId"`
	SessionName   string `json:"sessionName"`
	Agents        string `json:"agents"`
	ObservedMs    int64  `json:"observedMs"`
	BusyMs        int64  `json:"busyMs"`
	WaitingMs     int64  `json:"waitingMs"`
	IdleMs        int64  `json:"idleMs"`
	WaitingEvents int64  `json:"waitingEvents"`
}

type ProjectActivityStatistics struct {
	Days          int                    `json:"days"`
	RecordingFrom string                 `json:"recordingFrom"`
	UpdatedAt     string                 `json:"updatedAt"`
	Summary       ActivityStatsSummary   `json:"summary"`
	Series        []ActivityStatsDay     `json:"series"`
	Agents        []ActivityStatsAgent   `json:"agents"`
	Sessions      []ActivityStatsSession `json:"sessions"`
}

func NewActivityStatsRecorder() (*ActivityStatsRecorder, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".config", "agent-session-manager-desktop", "activity-stats")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &ActivityStatsRecorder{
		dir:    dir,
		stores: make(map[string]*activityProjectStore),
		live:   make(map[string]*liveActivityState),
	}, nil
}

func statsProjectFilename(projectID string) string {
	if projectID == "" {
		return "default.json"
	}
	sum := sha256.Sum256([]byte(projectID))
	return "project-" + hex.EncodeToString(sum[:12]) + ".json"
}

func newActivityProjectStore(path string, now time.Time) *activityProjectStore {
	return &activityProjectStore{
		path: path,
		data: activityStatsFile{
			Version: activityStatsVersion,
			Records: make(map[string]*activityStatsRecord),
		},
		lastFlush: now,
	}
}

func (r *ActivityStatsRecorder) storeLocked(projectID string, now time.Time) *activityProjectStore {
	if store := r.stores[projectID]; store != nil {
		return store
	}

	path := filepath.Join(r.dir, statsProjectFilename(projectID))
	store := newActivityProjectStore(path, now)
	store.writer, store.lockPath = acquireStatsWriter(path)
	r.loadStoreLocked(store, now)
	r.stores[projectID] = store
	r.pruneLocked(store, now)
	return store
}

func (r *ActivityStatsRecorder) loadStoreLocked(store *activityProjectStore, now time.Time) {
	raw, err := os.ReadFile(store.path)
	if err == nil {
		var data activityStatsFile
		if parseErr := json.Unmarshal(raw, &data); parseErr == nil && validStatsFile(data) {
			store.data = data
		} else {
			if !store.writer {
				log.Printf("[statistics] read-only statistics file is invalid: %s", store.path)
				return
			}
			if parseErr == nil {
				parseErr = fmt.Errorf("invalid activity statistics data")
			}
			quarantine := fmt.Sprintf("%s.corrupt-%d", store.path, now.UnixNano())
			if renameErr := os.Rename(store.path, quarantine); renameErr != nil {
				log.Printf("[statistics] corrupt file %s (%v), quarantine failed: %v", store.path, parseErr, renameErr)
			} else {
				log.Printf("[statistics] corrupt file moved to %s: %v", quarantine, parseErr)
			}
		}
	} else if !os.IsNotExist(err) {
		log.Printf("[statistics] failed to read %s: %v", store.path, err)
	}
}

func acquireStatsWriter(path string) (bool, string) {
	lockPath := path + ".writer-lock"
	statsWriterLocks.Lock()
	defer statsWriterLocks.Unlock()
	if statsWriterLocks.paths[lockPath] {
		return false, lockPath
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := os.Mkdir(lockPath, 0700); err == nil {
			_ = os.WriteFile(filepath.Join(lockPath, "pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
			statsWriterLocks.paths[lockPath] = true
			return true, lockPath
		} else if !os.IsExist(err) {
			return false, lockPath
		}
		if !staleStatsWriterLock(lockPath) {
			return false, lockPath
		}
		_ = os.RemoveAll(lockPath)
	}
	return false, lockPath
}

func staleStatsWriterLock(lockPath string) bool {
	raw, err := os.ReadFile(filepath.Join(lockPath, "pid"))
	if err != nil {
		info, statErr := os.Stat(lockPath)
		return statErr == nil && time.Since(info.ModTime()) > 2*time.Minute
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return true
	}
	return !statsProcessRunning(pid)
}

func releaseStatsWriter(store *activityProjectStore) {
	if !store.writer {
		return
	}
	statsWriterLocks.Lock()
	delete(statsWriterLocks.paths, store.lockPath)
	statsWriterLocks.Unlock()
	_ = os.RemoveAll(store.lockPath)
	store.writer = false
}

func validStatsFile(data activityStatsFile) bool {
	if data.Version != activityStatsVersion || data.Records == nil {
		return false
	}
	for _, record := range data.Records {
		if record == nil || record.Date == "" || record.SessionID == "" || record.Agent == "" {
			return false
		}
		if record.ObservedMs < 0 || record.BusyMs < 0 || record.WaitingMs < 0 ||
			record.IdleMs < 0 || record.WaitingEvents < 0 {
			return false
		}
		if record.BusyMs+record.WaitingMs+record.IdleMs != record.ObservedMs {
			return false
		}
	}
	return true
}

func (r *ActivityStatsRecorder) Observe(projectID string, now time.Time, observations []activityObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}

	store := r.storeLocked(projectID, now)
	if !store.writer {
		store.writer, store.lockPath = acquireStatsWriter(store.path)
		if !store.writer {
			return
		}
		// The previous writer may have published newer totals before releasing
		// the lock. Start from its latest atomic snapshot.
		store.data = activityStatsFile{Version: activityStatsVersion, Records: make(map[string]*activityStatsRecord)}
		r.loadStoreLocked(store, now)
	}
	seen := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		if !validActivity(observation.Activity) || observation.SessionID == "" || observation.Agent == "" {
			continue
		}
		key := liveActivityKey(projectID, observation)
		seen[key] = struct{}{}
		state := r.live[key]
		if state == nil {
			r.live[key] = &liveActivityState{
				activity:       observation.Activity,
				lastObservedAt: now,
				observation:    observation,
			}
			continue
		}

		gap := now.Sub(state.lastObservedAt)
		if gap <= 0 || gap > activityStatsMaxGap {
			// Never bridge an app pause, capture stall, or clock jump. The
			// returning sample becomes a fresh, silent baseline.
			state.activity = observation.Activity
			state.candidate = ""
			state.lastObservedAt = now
			state.observation = observation
			continue
		}

		switch {
		case observation.Activity == state.activity:
			r.addDurationLocked(store, state.observation, state.lastObservedAt, now, state.activity)
			state.candidate = ""
		case observation.Activity == state.candidate:
			// The first different sample established the boundary. Attribute
			// the interval between that sample and this confirmation to the
			// new state, then commit it.
			r.addDurationLocked(store, observation, state.lastObservedAt, now, observation.Activity)
			state.activity = observation.Activity
			state.candidate = ""
			if state.activity == "waiting" {
				r.incrementWaitingEventLocked(store, observation, now)
			}
		default:
			// First sample of a potential transition: the preceding interval
			// still belongs to the stable old state.
			r.addDurationLocked(store, state.observation, state.lastObservedAt, now, state.activity)
			state.candidate = observation.Activity
		}
		state.lastObservedAt = now
		state.observation = observation
	}

	// A disappeared tab/session/project must not be bridged when it appears
	// again. Keeping no stale live state also bounds memory usage.
	for key := range r.live {
		if _, ok := seen[key]; !ok {
			delete(r.live, key)
		}
	}

	r.pruneLocked(store, now)
	if store.dirty && now.Sub(store.lastFlush) >= activityStatsFlushRate {
		if err := r.flushLocked(store, now); err != nil {
			// Retry at most once per flush interval on persistent disk errors.
			store.lastFlush = now
			log.Printf("[statistics] failed to flush %s: %v", store.path, err)
		}
	}
}

func validActivity(activity string) bool {
	return activity == "busy" || activity == "waiting" || activity == "idle"
}

func liveActivityKey(projectID string, observation activityObservation) string {
	return strings.Join([]string{
		projectID, observation.SessionID, fmt.Sprintf("%d", observation.WindowIdx),
		observation.Agent,
	}, "\x1f")
}

func statsRecordKey(date string, observation activityObservation) string {
	return strings.Join([]string{
		date, observation.SessionID, fmt.Sprintf("%d", observation.WindowIdx),
		observation.Agent,
	}, "\x1f")
}

func (r *ActivityStatsRecorder) recordLocked(store *activityProjectStore, observation activityObservation, at time.Time) *activityStatsRecord {
	date := at.In(time.Local).Format("2006-01-02")
	key := statsRecordKey(date, observation)
	record := store.data.Records[key]
	if record == nil {
		record = &activityStatsRecord{
			Date:            date,
			SessionID:       observation.SessionID,
			SessionName:     observation.SessionName,
			WindowIdx:       observation.WindowIdx,
			TabName:         observation.TabName,
			Agent:           observation.Agent,
			FirstObservedAt: at.UTC().Format(time.RFC3339Nano),
		}
		store.data.Records[key] = record
	}
	record.SessionName = observation.SessionName
	record.TabName = observation.TabName
	record.LastObservedAt = at.UTC().Format(time.RFC3339Nano)
	return record
}

func (r *ActivityStatsRecorder) addDurationLocked(store *activityProjectStore, observation activityObservation, from, to time.Time, activity string) {
	for from.Before(to) {
		localFrom := from.In(time.Local)
		nextDay := time.Date(localFrom.Year(), localFrom.Month(), localFrom.Day()+1, 0, 0, 0, 0, time.Local)
		end := to
		if nextDay.Before(end) {
			end = nextDay
		}
		ms := end.Sub(from).Milliseconds()
		if ms <= 0 {
			break
		}
		record := r.recordLocked(store, observation, from)
		record.LastObservedAt = end.UTC().Format(time.RFC3339Nano)
		record.ObservedMs += ms
		switch activity {
		case "busy":
			record.BusyMs += ms
		case "waiting":
			record.WaitingMs += ms
		default:
			record.IdleMs += ms
		}
		store.dirty = true
		from = end
	}
}

func (r *ActivityStatsRecorder) incrementWaitingEventLocked(store *activityProjectStore, observation activityObservation, at time.Time) {
	r.recordLocked(store, observation, at).WaitingEvents++
	store.dirty = true
}

func (r *ActivityStatsRecorder) pruneLocked(store *activityProjectStore, now time.Time) {
	cutoff := now.In(time.Local).AddDate(0, 0, -(activityStatsRetention - 1)).Format("2006-01-02")
	for key, record := range store.data.Records {
		if record == nil || record.Date < cutoff {
			delete(store.data.Records, key)
			store.dirty = true
		}
	}
}

func (r *ActivityStatsRecorder) flushLocked(store *activityProjectStore, now time.Time) error {
	store.data.UpdatedAt = now.UTC().Format(time.RFC3339)
	raw, err := json.MarshalIndent(store.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", store.path, os.Getpid())
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, store.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	store.dirty = false
	store.lastFlush = now
	return nil
}

func (r *ActivityStatsRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()

	if !r.closed {
		r.closed = true
		// Account for the short final slice after the last valid sample.
		for key, state := range r.live {
			gap := now.Sub(state.lastObservedAt)
			if gap > 0 && gap <= activityStatsMaxGap {
				projectID := strings.SplitN(key, "\x1f", 2)[0]
				store := r.storeLocked(projectID, now)
				if store.writer {
					r.addDurationLocked(store, state.observation, state.lastObservedAt, now, state.activity)
				}
			}
		}
	}

	var firstErr error
	for _, store := range r.stores {
		if !store.writer {
			continue
		}
		if store.dirty {
			if err := r.flushLocked(store, now); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				releaseStatsWriter(store)
				continue
			}
		}
		releaseStatsWriter(store)
	}
	return firstErr
}

func (r *ActivityStatsRecorder) Statistics(projectID string, days int, now time.Time) ProjectActivityStatistics {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch days {
	case 7, 30, 90:
	default:
		days = 7
	}
	store := r.storeLocked(projectID, now)
	if !store.writer {
		// A different application instance owns this project's writer lock.
		// Reload its most recent atomic snapshot for a read-only dashboard.
		store.data = activityStatsFile{Version: activityStatsVersion, Records: make(map[string]*activityStatsRecord)}
		r.loadStoreLocked(store, now)
	}
	r.pruneLocked(store, now)
	start := time.Date(now.In(time.Local).Year(), now.In(time.Local).Month(), now.In(time.Local).Day(), 0, 0, 0, 0, time.Local).
		AddDate(0, 0, -(days - 1))

	result := ProjectActivityStatistics{
		Days:      days,
		UpdatedAt: store.data.UpdatedAt,
		Series:    make([]ActivityStatsDay, days),
	}
	dayIndex := make(map[string]int, days)
	for i := 0; i < days; i++ {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		result.Series[i].Date = date
		dayIndex[date] = i
	}

	agentMap := make(map[string]*ActivityStatsAgent)
	type sessionAggregate struct {
		stats        ActivityStatsSession
		agents       map[string]struct{}
		lastObserved time.Time
	}
	sessionMap := make(map[string]*sessionAggregate)
	var firstObserved time.Time

	for _, record := range store.data.Records {
		if parsed := parseRecordStart(record); !parsed.IsZero() && (firstObserved.IsZero() || parsed.Before(firstObserved)) {
			firstObserved = parsed
		}
		index, ok := dayIndex[record.Date]
		if !ok {
			continue
		}
		result.Summary.ObservedMs += record.ObservedMs
		result.Summary.BusyMs += record.BusyMs
		result.Summary.WaitingMs += record.WaitingMs
		result.Summary.IdleMs += record.IdleMs
		result.Summary.WaitingEvents += record.WaitingEvents

		result.Series[index].BusyMs += record.BusyMs
		result.Series[index].WaitingMs += record.WaitingMs
		result.Series[index].IdleMs += record.IdleMs

		agent := agentMap[record.Agent]
		if agent == nil {
			agent = &ActivityStatsAgent{Agent: record.Agent}
			agentMap[record.Agent] = agent
		}
		agent.ObservedMs += record.ObservedMs
		agent.BusyMs += record.BusyMs
		agent.WaitingMs += record.WaitingMs
		agent.IdleMs += record.IdleMs
		agent.WaitingEvents += record.WaitingEvents

		session := sessionMap[record.SessionID]
		if session == nil {
			session = &sessionAggregate{
				stats:  ActivityStatsSession{SessionID: record.SessionID},
				agents: make(map[string]struct{}),
			}
			sessionMap[record.SessionID] = session
		}
		recordLast := parseRecordLast(record)
		if session.lastObserved.IsZero() || recordLast.After(session.lastObserved) {
			session.stats.SessionName = record.SessionName
			session.lastObserved = recordLast
		}
		session.stats.ObservedMs += record.ObservedMs
		session.stats.BusyMs += record.BusyMs
		session.stats.WaitingMs += record.WaitingMs
		session.stats.IdleMs += record.IdleMs
		session.stats.WaitingEvents += record.WaitingEvents
		session.agents[record.Agent] = struct{}{}
	}

	if !firstObserved.IsZero() {
		result.RecordingFrom = firstObserved.UTC().Format(time.RFC3339)
	}
	if result.Summary.ObservedMs > 0 {
		result.Summary.BusyPercent = float64(result.Summary.BusyMs) * 100 / float64(result.Summary.ObservedMs)
	}
	for _, agent := range agentMap {
		if result.Summary.BusyMs > 0 {
			agent.SharePercent = float64(agent.BusyMs) * 100 / float64(result.Summary.BusyMs)
		}
		result.Agents = append(result.Agents, *agent)
	}
	sort.Slice(result.Agents, func(i, j int) bool {
		return result.Agents[i].BusyMs > result.Agents[j].BusyMs
	})

	for _, session := range sessionMap {
		agents := make([]string, 0, len(session.agents))
		for agent := range session.agents {
			agents = append(agents, agent)
		}
		sort.Strings(agents)
		session.stats.Agents = strings.Join(agents, ", ")
		result.Sessions = append(result.Sessions, session.stats)
	}
	sort.Slice(result.Sessions, func(i, j int) bool {
		if result.Sessions[i].BusyMs == result.Sessions[j].BusyMs {
			return result.Sessions[i].ObservedMs > result.Sessions[j].ObservedMs
		}
		return result.Sessions[i].BusyMs > result.Sessions[j].BusyMs
	})
	if len(result.Sessions) > 10 {
		result.Sessions = result.Sessions[:10]
	}
	return result
}

func parseRecordStart(record *activityStatsRecord) time.Time {
	if record == nil {
		return time.Time{}
	}
	if record.FirstObservedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, record.FirstObservedAt); err == nil {
			return parsed
		}
	}
	parsed, _ := time.ParseInLocation("2006-01-02", record.Date, time.Local)
	return parsed
}

func parseRecordLast(record *activityStatsRecord) time.Time {
	if record == nil {
		return time.Time{}
	}
	if record.LastObservedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, record.LastObservedAt); err == nil {
			return parsed
		}
	}
	return parseRecordStart(record)
}

// GetProjectActivityStatistics returns locally observed agent activity for an
// explicit project. The metric is UI status time, not CPU time or billing.
func (a *App) GetProjectActivityStatistics(projectID string, days int) ProjectActivityStatistics {
	if a.activityStats == nil {
		return ProjectActivityStatistics{Days: days}
	}
	return a.activityStats.Statistics(projectID, days, time.Now())
}
