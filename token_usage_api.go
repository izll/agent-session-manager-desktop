package main

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"asmgr-desktop/remote"
	"asmgr-desktop/remote/protocol"
	"asmgr-desktop/session"
	"asmgr-desktop/tokenusage"
	"asmgr-desktop/tokenusage/store"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// How many tokens each session used, on this computer and on the servers the
// app knows.
//
// The account-wide rings say how much of a limit has gone. They cannot say what
// used it, which is the question this answers.

const (
	tokenUsageProgressEvent = "tokenUsage:progress"
	tokenUsageUpdatedEvent  = "tokenUsage:updated"

	// tokenUsageFirstDelay keeps the first scan off the app's busy start. That
	// scan can read gigabytes.
	tokenUsageFirstDelay = 2 * time.Minute
	// tokenUsageInterval keeps the numbers from going stale, and more to the
	// point keeps ahead of Claude Code deleting old transcripts.
	tokenUsageInterval = 30 * time.Minute
	// tokenUsageReadTimeout bounds one batch from a server.
	tokenUsageReadTimeout = 2 * time.Minute
)

// TokenUsageItem is one conversation's use of one model in a period.
type TokenUsageItem struct {
	Machine      string `json:"machine"`
	MachineName  string `json:"machineName"`
	Conversation string `json:"conversation"`
	Agent        string `json:"agent"`
	Cwd          string `json:"cwd"`
	LastUnix     int64  `json:"lastUnix"`
	Model        string `json:"model"`
	Subagent     bool   `json:"subagent"`
	Input        int64  `json:"input"`
	Output       int64  `json:"output"`
	CacheRead    int64  `json:"cacheRead"`
	CacheWrite   int64  `json:"cacheWrite"`
	Responses    int64  `json:"responses"`
	// Empty session means no session of the app could be tied to it, such as an
	// agent started in a terminal elsewhere.
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	SessionID   string `json:"sessionId"`
	SessionName string `json:"sessionName"`
	TabName     string `json:"tabName"`
}

// TokenUsageMachine says how fresh one machine's numbers are.
type TokenUsageMachine struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Local        bool   `json:"local"`
	LastScanUnix int64  `json:"lastScanUnix"`
	Error        string `json:"error"`
}

// TokenUsageReport is what the view shows.
type TokenUsageReport struct {
	Items    []TokenUsageItem    `json:"items"`
	Machines []TokenUsageMachine `json:"machines"`
	Scanning bool                `json:"scanning"`
}

// TokenUsageProgress reports a running scan.
type TokenUsageProgress struct {
	Machine string `json:"machine"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
}

type tokenUsageState struct {
	mu       sync.Mutex
	store    *store.Store
	scanning bool
	stopped  bool
	// ctx ends with the app, and with it the timer and any scan under way.
	ctx    context.Context
	cancel context.CancelFunc
	scans  sync.WaitGroup
}

// tokenUsageContext returns the context everything here runs under. Call with
// the mutex held.
func (a *App) tokenUsageContext() context.Context {
	if a.tokenUsage.ctx == nil {
		a.tokenUsage.ctx, a.tokenUsage.cancel = context.WithCancel(a.lifecycleContext())
	}
	return a.tokenUsage.ctx
}

// tokenUsageStore opens the database on first use. A failure is not remembered,
// so opening the view again tries again.
func (a *App) tokenUsageStore() (*store.Store, error) {
	if a.storage == nil {
		return nil, errors.New("the app could not open its storage")
	}
	a.tokenUsage.mu.Lock()
	defer a.tokenUsage.mu.Unlock()
	if a.tokenUsage.store == nil {
		opened, err := store.Open(a.storage.TokenUsagePath(), time.Local)
		if err != nil {
			return nil, err
		}
		a.tokenUsage.store = opened
	}
	return a.tokenUsage.store, nil
}

// GetTokenUsage returns the last so many days, today included. Zero days means
// everything recorded. It answers from what has been counted so far and never
// waits for a scan.
func (a *App) GetTokenUsage(days int) (*TokenUsageReport, error) {
	usage, err := a.tokenUsageStore()
	if err != nil {
		return nil, err
	}
	ctx := a.lifecycleContext()

	owned := a.rememberTokenUsageOwners(ctx, usage)

	fromDay := 0
	if days > 0 {
		fromDay = usage.DayOf(time.Now().AddDate(0, 0, -(days - 1)))
	}
	items, err := usage.Items(ctx, fromDay)
	if err != nil {
		return nil, err
	}

	machines, err := a.tokenUsageMachines(ctx, usage)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, machine := range machines {
		names[machine.ID] = machine.Name
	}

	report := &TokenUsageReport{
		Items:    make([]TokenUsageItem, 0, len(items)),
		Machines: machines,
		Scanning: a.tokenUsageScanning(),
	}
	for _, item := range items {
		entry := TokenUsageItem{
			Machine:      item.Machine,
			MachineName:  names[item.Machine],
			Conversation: item.Conversation,
			Agent:        item.Agent,
			Cwd:          item.Cwd,
			LastUnix:     item.LastUnix,
			Model:        item.Model,
			Subagent:     item.Subagent,
			Input:        item.Input,
			Output:       item.Output,
			CacheRead:    item.CacheRead,
			CacheWrite:   item.CacheWrite,
			Responses:    item.Responses,
		}
		if entry.MachineName == "" && item.Machine != store.LocalMachine {
			// A server since removed from the list: its numbers still happened.
			entry.MachineName = item.Machine
		}
		owner := item.Owner
		if owner == nil {
			owner = owned.ownerOfDirectory(item.Machine, item.Cwd)
		}
		if owner != nil {
			entry.ProjectID = owner.ProjectID
			entry.ProjectName = owner.ProjectName
			entry.SessionID = owner.SessionID
			entry.SessionName = owner.SessionName
			entry.TabName = owner.TabName
		}
		report.Items = append(report.Items, entry)
	}
	return report, nil
}

// rememberTokenUsageOwners records which session holds which conversation right
// now. Also done on the timer: a session holds only its current conversation, so
// one replaced while this view is closed would otherwise never be tied to it.
func (a *App) rememberTokenUsageOwners(ctx context.Context, usage *store.Store) *tokenUsageOwners {
	owned := a.collectTokenUsageOwners()
	if err := usage.RememberOwners(ctx, owned.owners); err != nil {
		log.Printf("[tokens] could not record which session holds what: %v", err)
	}
	return owned
}

// RefreshTokenUsage starts a scan of every machine, connecting to servers that
// are not connected yet. It returns at once; the view listens for the events.
func (a *App) RefreshTokenUsage() {
	a.startTokenUsageScan(true)
}

func (a *App) tokenUsageScanning() bool {
	a.tokenUsage.mu.Lock()
	defer a.tokenUsage.mu.Unlock()
	return a.tokenUsage.scanning
}

// startTokenUsageWatcher scans on a timer. Only servers already connected are
// read: a background timer has no business dialling a machine nobody asked for.
func (a *App) startTokenUsageWatcher() {
	a.tokenUsage.mu.Lock()
	ctx := a.tokenUsageContext()
	a.tokenUsage.scans.Add(1)
	a.tokenUsage.mu.Unlock()

	go func() {
		defer a.tokenUsage.scans.Done()
		timer := time.NewTimer(tokenUsageFirstDelay)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			a.startTokenUsageScan(false)
			timer.Reset(tokenUsageInterval)
		}
	}()
}

// stopTokenUsageWait bounds shutdown. A helper being installed on a server does
// not stop for this, and the app should not sit open waiting for it: everything
// counted is already committed, and the rest is counted next time.
const stopTokenUsageWait = 3 * time.Second

// stopTokenUsage ends any scan and closes the database, at shutdown.
func (a *App) stopTokenUsage() {
	a.tokenUsage.mu.Lock()
	a.tokenUsage.stopped = true
	cancel := a.tokenUsage.cancel
	opened := a.tokenUsage.store
	a.tokenUsage.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	finished := make(chan struct{})
	go func() {
		a.tokenUsage.scans.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		if opened != nil {
			_ = opened.Close()
		}
	case <-time.After(stopTokenUsageWait):
		log.Printf("[tokens] a scan was still running at shutdown; leaving it")
	}
}

// startTokenUsageScan runs one scan unless one is already running, in which
// case that one's result is as good.
func (a *App) startTokenUsageScan(connect bool) {
	a.tokenUsage.mu.Lock()
	defer a.tokenUsage.mu.Unlock()
	if a.tokenUsage.scanning || a.tokenUsage.stopped {
		return
	}
	a.tokenUsage.scanning = true
	ctx := a.tokenUsageContext()
	a.tokenUsage.scans.Add(1)

	go func() {
		defer a.tokenUsage.scans.Done()
		a.scanTokenUsage(ctx, connect)
		a.tokenUsage.mu.Lock()
		a.tokenUsage.scanning = false
		a.tokenUsage.mu.Unlock()
		a.emitTokenUsage(tokenUsageUpdatedEvent, nil)
	}()
}

// scanTokenUsage reads each machine in turn. One that fails is noted and the
// others still go ahead.
func (a *App) scanTokenUsage(ctx context.Context, connect bool) {
	usage, err := a.tokenUsageStore()
	if err != nil {
		log.Printf("[tokens] could not open the usage database: %v", err)
		return
	}
	a.rememberTokenUsageOwners(ctx, usage)

	roots, err := tokenusage.DefaultRoots()
	if err == nil {
		a.scanTokenUsageMachine(ctx, usage, store.LocalMachine, store.LocalSource{Roots: roots})
	} else {
		a.recordTokenUsageScan(ctx, usage, store.LocalMachine, err)
	}

	list, err := a.storage.LoadServers()
	if err != nil {
		log.Printf("[tokens] could not read the server list: %v", err)
		return
	}
	scanned := map[string]bool{}
	for index := range list.Servers {
		if ctx.Err() != nil {
			return
		}
		server := &list.Servers[index]
		machine := usageServerKey(server)
		// Two entries for one host are one set of logs.
		if scanned[machine] {
			continue
		}
		var connection *serverConnection
		if connect {
			connection, err = a.connectionFor(server.ID)
			if err != nil {
				a.recordTokenUsageScan(ctx, usage, machine, err)
				continue
			}
		} else if connection = a.servers.get(server.ID); connection == nil {
			continue
		}
		scanned[machine] = true
		a.scanTokenUsageMachine(ctx, usage, machine, helperUsageSource{helper: connection.helper})
	}
}

func (a *App) scanTokenUsageMachine(ctx context.Context, usage *store.Store, machine string, source store.Source) {
	err := usage.Sync(ctx, machine, source, func(done, total int) {
		a.emitTokenUsage(tokenUsageProgressEvent, TokenUsageProgress{Machine: machine, Done: done, Total: total})
	})
	a.recordTokenUsageScan(ctx, usage, machine, err)
}

func (a *App) recordTokenUsageScan(ctx context.Context, usage *store.Store, machine string, scanErr error) {
	// Being shut down is no fault of the machine's.
	if ctx.Err() != nil {
		return
	}
	if scanErr != nil {
		log.Printf("[tokens] %s: %v", machine, scanErr)
	}
	if err := usage.RecordScan(ctx, machine, time.Now(), scanErr); err != nil {
		log.Printf("[tokens] could not record the scan of %s: %v", machine, err)
	}
}

// emitTokenUsage is a variable so tests, which have no Wails runtime, can
// replace it.
var emitTokenUsageEvent = func(ctx context.Context, name string, data any) {
	if data == nil {
		runtime.EventsEmit(ctx, name)
		return
	}
	runtime.EventsEmit(ctx, name, data)
}

func (a *App) emitTokenUsage(name string, data any) {
	if a.ctx != nil {
		emitTokenUsageEvent(a.ctx, name, data)
	}
}

// helperUsageSource reads a server's logs through its helper.
type helperUsageSource struct {
	helper *remote.Helper
}

func (h helperUsageSource) List(ctx context.Context) ([]tokenusage.FileInfo, error) {
	callCtx, cancel := context.WithTimeout(ctx, tokenUsageReadTimeout)
	defer cancel()
	var result protocol.UsageFilesResult
	if err := h.helper.Call(callCtx, protocol.MethodUsageFiles, struct{}{}, &result); err != nil {
		return nil, err
	}
	return result.Files, nil
}

func (h helperUsageSource) Read(ctx context.Context, requests []tokenusage.ReadRequest) ([]tokenusage.FileResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, tokenUsageReadTimeout)
	defer cancel()
	var result protocol.UsageReadResult
	if err := h.helper.Call(callCtx, protocol.MethodUsageRead, protocol.UsageReadParams{Requests: requests}, &result); err != nil {
		return nil, err
	}
	return result.Results, nil
}

func (a *App) tokenUsageMachines(ctx context.Context, usage *store.Store) ([]TokenUsageMachine, error) {
	scans, err := usage.Scans(ctx)
	if err != nil {
		return nil, err
	}
	local := scans[store.LocalMachine]
	// Named by the frontend: "this computer" is a translated string.
	machines := []TokenUsageMachine{{ID: store.LocalMachine, Local: true, LastScanUnix: local.LastUnix, Error: local.Error}}
	list, err := a.storage.LoadServers()
	if err != nil {
		return machines, nil
	}
	listed := map[string]bool{}
	for index := range list.Servers {
		server := &list.Servers[index]
		machine := usageServerKey(server)
		if listed[machine] {
			continue
		}
		listed[machine] = true
		scan := scans[machine]
		machines = append(machines, TokenUsageMachine{
			ID: machine, Name: server.DisplayName(), LastScanUnix: scan.LastUnix, Error: scan.Error,
		})
	}
	return machines, nil
}

// usageServerKey names a server's logs by where they are, not by the server's id
// in the list. Taking a server out and adding it again gives it a new id, and
// its logs, read again from the top under that id, would be counted twice.
func usageServerKey(server *session.Server) string {
	key := strings.ToLower(server.User + "@" + server.Host)
	if server.Port != 0 && server.Port != 22 {
		key += ":" + strconv.Itoa(server.Port)
	}
	return key
}

// tokenUsageOwners is every session of every project, as of now.
type tokenUsageOwners struct {
	// owners ties a conversation to its session, from the ids sessions hold.
	owners []store.Owner
	// directories lists who works where, for a conversation no session names.
	directories map[string][]store.Owner
}

// ownerOfDirectory ties a conversation to a session by where it ran, when only
// one session works there. A session holds only its current conversation, so
// this is what finds the ones before it.
//
// Exact match only. A session in the home directory would otherwise claim every
// conversation on the machine. Two sessions sharing a directory cannot be told
// apart, and a guess would be wrong half the time, so neither gets it.
func (o *tokenUsageOwners) ownerOfDirectory(machine, cwd string) *store.Owner {
	candidates := o.directories[directoryKey(machine, normaliseUsageDirectory(cwd))]
	if len(candidates) == 0 {
		return nil
	}
	owner := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.SessionID != owner.SessionID {
			return nil
		}
		// The session is known; which of its tabs is not, and naming one would
		// be a guess.
		if candidate.TabName != owner.TabName {
			owner.TabName = ""
		}
	}
	return &owner
}

func directoryKey(machine, directory string) string {
	return machine + "\x00" + directory
}

// normaliseUsageDirectory maps a Claude worktree back to its repository: an
// agent working in <repo>/.claude/worktrees/<name> is working for <repo>.
func normaliseUsageDirectory(directory string) string {
	if directory == "" {
		return ""
	}
	if at := strings.Index(filepath.ToSlash(directory), "/.claude/worktrees/"); at >= 0 {
		directory = directory[:at]
	}
	return filepath.Clean(directory)
}

// collectTokenUsageOwners walks every project. A project that cannot be read is
// skipped: one unusable file must not empty the view.
func (a *App) collectTokenUsageOwners() *tokenUsageOwners {
	collected := &tokenUsageOwners{directories: map[string][]store.Owner{}}

	machineOf := map[string]string{"": store.LocalMachine}
	if list, err := a.storage.LoadServers(); err == nil {
		for index := range list.Servers {
			machineOf[list.Servers[index].ID] = usageServerKey(&list.Servers[index])
		}
	}
	usageMachine := func(serverID string) string {
		if machine, known := machineOf[serverID]; known {
			return machine
		}
		return serverID
	}

	type projectRef struct{ id, name string }
	// Name left empty: the default project's label is translated in the
	// frontend, and its id is empty because it is not in projects.json.
	refs := []projectRef{{}}
	if projects, err := a.storage.LoadProjects(); err == nil {
		for _, project := range projects.Projects {
			if project != nil {
				refs = append(refs, projectRef{id: project.ID, name: project.Name})
			}
		}
	}

	for _, project := range refs {
		instances, _, err := a.storage.LoadAllForProject(project.id)
		if err != nil {
			continue
		}
		for _, instance := range instances {
			if instance == nil {
				continue
			}
			owner := store.Owner{
				ProjectID: project.id, ProjectName: project.name,
				SessionID: instance.ID, SessionName: instance.Name,
			}
			collected.add(owner, instance.ResumeSessionID, usageMachine(instance.ServerID), instance.Path)
			for _, window := range instance.FollowedWindows {
				tab := owner
				tab.TabName = window.Name
				machine := usageMachine(window.RunsOn(instance.ServerID))
				directory := window.WorkDir
				if directory == "" {
					directory = instance.Path
				}
				collected.add(tab, window.ResumeSessionID, machine, directory)
				if window.WorktreeDir != "" {
					collected.add(tab, "", machine, window.WorktreeDir)
				}
			}
		}
	}
	return collected
}

func (o *tokenUsageOwners) add(owner store.Owner, conversation, machine, directory string) {
	if conversation != "" {
		named := owner
		named.Conversation = conversation
		o.owners = append(o.owners, named)
	}
	if directory != "" {
		key := directoryKey(machine, normaliseUsageDirectory(directory))
		o.directories[key] = append(o.directories[key], owner)
	}
}
