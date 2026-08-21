package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	recoverySchemaVersion = 1
	// A ceiling on the backup directory, not the retention rule.
	//
	// Retention is decided by backupsToKeep, which thins by age; this only
	// stops the directory growing without bound if a clock jump puts many
	// backups outside every band at once. Set well above what the bands can
	// produce (roughly 24 hourly + 14 daily + 12 weekly, plus an hour's worth
	// of unthinned ones).
	backupHardCeiling = 200
	// Days a deleted session or tab stays recoverable when the user hasn't
	// chosen otherwise. Nothing expired at all before this, so the trash — and
	// with it sessions.json, read on every load — grew unbounded.
	defaultTrashRetentionDays = 30
	// A ceiling for what retention alone doesn't cover: deleting a great many
	// sessions in one sitting. The newest are kept, and it applies even when
	// retention is switched off — the file still has to stay loadable.
	trashRetentionCount = 100
)

// trashRetentionDays resolves the configured retention in days, or 0 for
// "keep everything".
//
// Unset (the zero value, and so every existing config) means the default
// rather than "expire immediately", which would bin the trash of everyone
// upgrading. "Keep everything" is stored as a negative, since zero was
// already taken by unset.
func trashRetentionDays(settings *Settings) int {
	switch {
	case settings == nil, settings.TrashRetentionDays == 0:
		return defaultTrashRetentionDays
	case settings.TrashRetentionDays < 0:
		return 0
	default:
		return settings.TrashRetentionDays
	}
}

// pruneTrash drops entries past their retention. It returns the kept entries
// and whether anything was removed.
//
// Called where the trash is already being written rather than on load: expiry
// is not urgent to the millisecond, and rewriting the file during a read would
// turn every load into a write.
func pruneTrash(entries []*TrashEntry, now time.Time, days int) ([]*TrashEntry, bool) {
	kept := make([]*TrashEntry, 0, len(entries))
	if days <= 0 {
		// Retention off: only the count cap below applies.
		kept = append(kept, entries...)
	} else {
		cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
		for _, e := range entries {
			// A zero DeletedAt predates this field; treat it as fresh rather
			// than silently discarding something the user may still want.
			if e.DeletedAt.IsZero() || e.DeletedAt.After(cutoff) {
				kept = append(kept, e)
			}
		}
	}

	// Trim oldest-first once over the cap. Sorted by deletion time rather than
	// insertion order, which restores can disturb.
	if len(kept) > trashRetentionCount {
		sort.SliceStable(kept, func(i, j int) bool {
			return kept[i].DeletedAt.Before(kept[j].DeletedAt)
		})
		kept = kept[len(kept)-trashRetentionCount:]
	}

	return kept, len(kept) != len(entries)
}

type TrashEntry struct {
	ID                string          `json:"id"`
	Kind              string          `json:"kind"`
	DeletedAt         time.Time       `json:"deleted_at"`
	SessionName       string          `json:"session_name"`
	ParentSessionID   string          `json:"parent_session_id,omitempty"`
	ParentSessionName string          `json:"parent_session_name,omitempty"`
	OriginalPosition  int             `json:"original_position,omitempty"`
	OriginalTabOrder  []int           `json:"original_tab_order,omitempty"`
	Session           *Instance       `json:"session,omitempty"`
	Tab               *FollowedWindow `json:"tab,omitempty"`
}

type RestoreResult struct {
	SessionID string `json:"sessionId"`
	WindowIdx int    `json:"windowIdx"`
}

type BackupInfo struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Size      int64     `json:"size"`
}

func (s *Storage) backupScopeLocked() string {
	if s.projectID == "" {
		return "default"
	}
	return filepath.Join("projects", s.projectID)
}

func (s *Storage) backupDirLocked() string {
	return filepath.Join(s.configDir, "backups", s.backupScopeLocked())
}

func sanitizedStorageData(data *StorageData) (*StorageData, []byte, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, nil, err
	}
	var clone StorageData
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, nil, err
	}
	if clone.Settings != nil {
		clone.Settings.AnthropicAPIKey = ""
	}
	pretty, err := json.MarshalIndent(&clone, "", "  ")
	return &clone, pretty, err
}

func (s *Storage) createAutomaticBackupLocked(data *StorageData) error {
	return s.createBackupLocked(data, true)
}

// createBackupLocked writes a backup, optionally skipping one that would be
// identical to the newest.
//
// Skipping is right for automatic backups: a save that changed nothing would
// otherwise push real history out of the retention window. It is wrong for one
// the user asked for — pressing "Back up now" and seeing the list unchanged
// reads as the button being broken, whatever the reasoning behind it.
func (s *Storage) createBackupLocked(data *StorageData, skipIfUnchanged bool) error {
	_, raw, err := sanitizedStorageData(data)
	if err != nil {
		return err
	}

	dir := s.backupDirLocked()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	existing, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	existing = backupJSONEntries(existing)
	sort.Slice(existing, func(i, j int) bool { return existing[i].Name() < existing[j].Name() })
	if skipIfUnchanged && len(existing) > 0 {
		latest := existing[len(existing)-1]
		if !latest.IsDir() {
			if previous, readErr := os.ReadFile(filepath.Join(dir, latest.Name())); readErr == nil && string(previous) == string(raw) {
				return nil
			}
		}
	}

	sum := sha256.Sum256(raw)
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(sum[:4]) + ".json"
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return pruneBackupDir(dir)
}

func backupJSONEntries(entries []os.DirEntry) []os.DirEntry {
	files := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, entry)
		}
	}
	return files
}

// pruneBackupDir thins the history rather than truncating it.
//
// Keeping the newest N is the obvious rule and the wrong one: on an active day
// every save makes a backup, so N of them span hours rather than days —
// measured on a real config, 25 backups covering nineteen hours, the last three
// seconds apart. Anything noticed the following morning had already been
// deleted.
//
// backupsToKeep decides what survives: everything from the last hour, one an
// hour for today, one a day for a fortnight, one a week for a quarter. The
// newest is always kept. A hard ceiling still applies underneath, so a clock
// jumping backwards cannot fill the disk.
func pruneBackupDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	files := backupJSONEntries(entries)
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	times := make([]time.Time, len(files))
	for i, file := range files {
		times[i] = backupTime(file.Name())
	}

	keep := backupsToKeep(times, time.Now().UTC())
	for i, file := range files {
		if keep[i] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, file.Name())); err != nil {
			return err
		}
	}

	// A backstop, unchanged in spirit: whatever the thinning decides, the
	// directory never grows past this. Only reachable if the clock misbehaves,
	// since the bands cannot produce more entries than they have buckets.
	remaining := backupJSONEntries(mustReadDir(dir))
	sort.Slice(remaining, func(i, j int) bool { return remaining[i].Name() < remaining[j].Name() })
	for len(remaining) > backupHardCeiling {
		if err := os.Remove(filepath.Join(dir, remaining[0].Name())); err != nil {
			return err
		}
		remaining = remaining[1:]
	}
	return nil
}

// mustReadDir returns nothing rather than an error: the caller is already past
// the point where a missing directory matters, and the ceiling below it is a
// backstop, not the primary rule.
func mustReadDir(dir string) []os.DirEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	return entries
}

// backupTime reads the timestamp a backup's name begins with.
//
// Names are written as "20060102T150405.000000000Z-<hash>.json". A name that
// does not parse gets the zero time, which puts it outside every band and so
// makes it a candidate for deletion — correct for a stray file, and it cannot
// take a real backup with it because those all parse.
func backupTime(name string) time.Time {
	const layout = "20060102T150405.000000000Z"
	if len(name) < len(layout) {
		return time.Time{}
	}
	parsed, err := time.Parse(layout, name[:len(layout)])
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func (s *Storage) createProjectsBackupLocked(data *ProjectsData) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(s.configDir, "backups", "projects-catalog")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	entries = backupJSONEntries(entries)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	if len(entries) > 0 {
		latest := entries[len(entries)-1]
		if !latest.IsDir() {
			if previous, readErr := os.ReadFile(filepath.Join(dir, latest.Name())); readErr == nil && string(previous) == string(raw) {
				return nil
			}
		}
	}
	sum := sha256.Sum256(raw)
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(sum[:4]) + ".json"
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return pruneBackupDir(dir)
}

// CreateBackup writes a backup because the user asked for one.
//
// Unlike the automatic path it does not skip an unchanged state: the button
// exists to produce a restore point at a moment of the user's choosing — before
// something risky, typically — and one that silently declines to appear is
// worse than a duplicate file.
func (s *Storage) CreateBackup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}
	return s.createBackupLocked(data, false)
}

func (s *Storage) ListBackups() ([]BackupInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.backupDirLocked()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]BackupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, BackupInfo{ID: entry.Name(), CreatedAt: info.ModTime(), Size: info.Size()})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func validBackupID(id string) bool {
	return id != "" && filepath.Base(id) == id && strings.HasSuffix(id, ".json") && !strings.Contains(id, "..")
}

func (s *Storage) RestoreBackup(id string) error {
	if !validBackupID(id) {
		return fmt.Errorf("invalid backup ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := readFileAtMost(filepath.Join(s.backupDirLocked(), id), maxCanonicalStorageBytes)
	if err != nil {
		return err
	}
	var restored StorageData
	if err := json.Unmarshal(raw, &restored); err != nil {
		return fmt.Errorf("invalid backup: %w", err)
	}
	if restored.SchemaVersion > recoverySchemaVersion {
		return fmt.Errorf("backup schema is newer than this application")
	}
	if err := validateRestoredStorage(&restored); err != nil {
		return fmt.Errorf("invalid backup structure: %w", err)
	}
	current, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}
	if err := s.createAutomaticBackupLocked(current); err != nil {
		return fmt.Errorf("failed to create safety backup: %w", err)
	}
	if restored.Settings == nil {
		restored.Settings = &Settings{}
	}
	if current.Settings != nil {
		restored.Settings.AnthropicAPIKey = current.Settings.AnthropicAPIKey
	}
	restored.SchemaVersion = recoverySchemaVersion
	restored.Revision = current.Revision + 1
	if restored.Instances == nil {
		restored.Instances = []*Instance{}
	}
	if restored.Groups == nil {
		restored.Groups = []*Group{}
	}
	if restored.Trash == nil {
		restored.Trash = []*TrashEntry{}
	}
	return s.writeStorageDataLocked(&restored, true)
}

func validateRestoredStorage(data *StorageData) error {
	groupIDs := make(map[string]struct{}, len(data.Groups))
	groupNames := make(map[string]struct{}, len(data.Groups))
	for i, group := range data.Groups {
		if group == nil {
			return fmt.Errorf("group %d is null", i)
		}
		if strings.TrimSpace(group.ID) == "" || strings.TrimSpace(group.Name) == "" {
			return fmt.Errorf("group %d has an empty ID or name", i)
		}
		if _, exists := groupIDs[group.ID]; exists {
			return fmt.Errorf("duplicate group ID %q", group.ID)
		}
		if _, exists := groupNames[group.Name]; exists {
			return fmt.Errorf("duplicate group name %q", group.Name)
		}
		groupIDs[group.ID] = struct{}{}
		groupNames[group.Name] = struct{}{}
	}

	instanceIDs := make(map[string]struct{}, len(data.Instances))
	instanceNames := make(map[string]struct{}, len(data.Instances))
	for i, instance := range data.Instances {
		if err := validateRestoredInstance(instance, fmt.Sprintf("instance %d", i)); err != nil {
			return err
		}
		if _, exists := instanceIDs[instance.ID]; exists {
			return fmt.Errorf("duplicate instance ID %q", instance.ID)
		}
		if _, exists := instanceNames[instance.Name]; exists {
			return fmt.Errorf("duplicate instance name %q", instance.Name)
		}
		if instance.GroupID != "" {
			if _, exists := groupIDs[instance.GroupID]; !exists {
				return fmt.Errorf("instance %q references missing group %q", instance.ID, instance.GroupID)
			}
		}
		instanceIDs[instance.ID] = struct{}{}
		instanceNames[instance.Name] = struct{}{}
	}

	trashIDs := make(map[string]struct{}, len(data.Trash))
	for i, entry := range data.Trash {
		if entry == nil {
			return fmt.Errorf("trash entry %d is null", i)
		}
		if strings.TrimSpace(entry.ID) == "" {
			return fmt.Errorf("trash entry %d has an empty ID", i)
		}
		if _, exists := trashIDs[entry.ID]; exists {
			return fmt.Errorf("duplicate trash ID %q", entry.ID)
		}
		trashIDs[entry.ID] = struct{}{}
		switch entry.Kind {
		case "session":
			if err := validateRestoredInstance(entry.Session, fmt.Sprintf("trash session %q", entry.ID)); err != nil {
				return err
			}
		case "tab":
			if entry.Tab == nil || strings.TrimSpace(entry.ParentSessionID) == "" {
				return fmt.Errorf("trash tab %q has no tab payload or parent", entry.ID)
			}
			if entry.Tab.Index < 0 {
				return fmt.Errorf("trash tab %q has a negative window index", entry.ID)
			}
		default:
			return fmt.Errorf("trash entry %q has unknown kind %q", entry.ID, entry.Kind)
		}
	}
	return nil
}

func validateRestoredInstance(instance *Instance, label string) error {
	if instance == nil {
		return fmt.Errorf("%s is null", label)
	}
	if strings.TrimSpace(instance.ID) == "" || strings.TrimSpace(instance.Name) == "" {
		return fmt.Errorf("%s has an empty ID or name", label)
	}
	windowIndices := make(map[int]struct{}, len(instance.FollowedWindows))
	for _, window := range instance.FollowedWindows {
		if window.Index < 0 {
			return fmt.Errorf("%s has a negative window index", label)
		}
		if _, exists := windowIndices[window.Index]; exists {
			return fmt.Errorf("%s has duplicate window index %d", label, window.Index)
		}
		windowIndices[window.Index] = struct{}{}
	}
	return nil
}

func (s *Storage) ListTrash() ([]*TrashEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return nil, err
	}
	result := append([]*TrashEntry(nil), data.Trash...)
	sort.Slice(result, func(i, j int) bool { return result[i].DeletedAt.After(result[j].DeletedAt) })
	return result, nil
}

func (s *Storage) TrashInstance(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}
	index := -1
	for i, instance := range data.Instances {
		if instance.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("instance not found")
	}
	instance := data.Instances[index]
	originalData, err := cloneStorageData(data)
	if err != nil {
		return fmt.Errorf("failed to snapshot storage before deleting session: %w", err)
	}
	// Include generated Codex IDs in both the automatic pre-delete backup and
	// the trash snapshot.
	instance.CaptureCodexResumeIDs()
	if err := s.createAutomaticBackupLocked(data); err != nil {
		return fmt.Errorf("failed to create pre-delete backup: %w", err)
	}
	// Keep a separate live descriptor for the irreversible tmux operation. The
	// stored/trash copy must already say "stopped" when it is published, while
	// Stop needs the original running status in order to kill the real session.
	liveInstance := *instance
	liveInstance.FollowedWindows = append([]FollowedWindow(nil), instance.FollowedWindows...)
	liveInstance.TabOrder = append([]int(nil), instance.TabOrder...)
	instance.Status = StatusStopped
	instance.MainWindowStopped = false
	data.Trash = append(data.Trash, &TrashEntry{
		ID:               uuid.NewString(),
		Kind:             "session",
		DeletedAt:        time.Now().UTC(),
		SessionName:      instance.Name,
		OriginalPosition: index,
		Session:          instance,
	})
	data.Instances = append(data.Instances[:index], data.Instances[index+1:]...)
	data.Trash, _ = pruneTrash(data.Trash, time.Now().UTC(), trashRetentionDays(data.Settings))
	data.SchemaVersion = recoverySchemaVersion
	data.Revision++
	return s.persistTrashThenApply(data, originalData, liveInstance.Stop)
}

func (s *Storage) TrashTab(sessionID string, windowIdx int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}
	var parent *Instance
	for _, instance := range data.Instances {
		if instance.ID == sessionID {
			parent = instance
			break
		}
	}
	if parent == nil {
		return fmt.Errorf("instance not found")
	}
	position := -1
	var snapshot FollowedWindow
	originalTabOrder := append([]int(nil), parent.TabOrder...)
	for i, tab := range parent.FollowedWindows {
		if tab.Index == windowIdx {
			position = i
			snapshot = tab
			break
		}
	}
	if position < 0 {
		// The tab is not in the stored list, but the tab bar shows what tmux
		// actually has — so a window that exists there and not here appears as
		// a perfectly ordinary tab that simply refuses to close.
		//
		// This happens when the two fall out of step: a window outlives the
		// record of it, and from then on nothing can remove it. Kill it and
		// return, with nothing to put in the trash because there is nothing
		// recorded to restore.
		if parent.Status != StatusRunning {
			return fmt.Errorf("tab not found")
		}
		if err := parent.DeleteWindow(windowIdx); err != nil {
			return fmt.Errorf("tab not found in this session, and closing its window failed: %w", err)
		}
		log.Printf("[TrashTab] closed untracked window %d of session %s", windowIdx, parent.ID)
		return nil
	}
	if err := parent.validateWindowDeletion(windowIdx); err != nil {
		return err
	}
	originalData, err := cloneStorageData(data)
	if err != nil {
		return fmt.Errorf("failed to snapshot storage before deleting tab: %w", err)
	}
	// Snapshot the Codex conversation ID while the tab process is still alive,
	// otherwise restoring this trash item would start a different conversation.
	parent.CaptureCodexResumeIDs()
	for _, tab := range parent.FollowedWindows {
		if tab.Index == windowIdx {
			snapshot = tab
			break
		}
	}
	if err := s.createAutomaticBackupLocked(data); err != nil {
		return fmt.Errorf("failed to create pre-delete backup: %w", err)
	}
	parent.removeWindowMetadata(windowIdx)
	data.Trash = append(data.Trash, &TrashEntry{
		ID:                uuid.NewString(),
		Kind:              "tab",
		DeletedAt:         time.Now().UTC(),
		SessionName:       snapshot.Name,
		ParentSessionID:   parent.ID,
		ParentSessionName: parent.Name,
		OriginalPosition:  position,
		OriginalTabOrder:  originalTabOrder,
		Tab:               &snapshot,
	})
	data.Trash, _ = pruneTrash(data.Trash, time.Now().UTC(), trashRetentionDays(data.Settings))
	data.SchemaVersion = recoverySchemaVersion
	data.Revision++
	return s.persistTrashThenApply(data, originalData, func() error {
		return parent.deleteLiveWindow(windowIdx)
	})
}

func cloneStorageData(data *StorageData) (*StorageData, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var clone StorageData
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

// persistTrashThenApply makes the recoverable state durable before an
// irreversible tmux operation. If the external operation fails, restore the
// exact pre-operation metadata; if that rollback also fails, retain both errors
// so callers know recovery needs attention.
func (s *Storage) persistTrashThenApply(data, originalData *StorageData, action func() error) error {
	if err := s.writeStorageDataLocked(data, true); err != nil {
		return err
	}
	if err := action(); err != nil {
		rollbackErr := s.writeStorageDataLocked(originalData, false)
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("failed to roll back trash metadata: %w", rollbackErr))
		}
		return err
	}
	return nil
}

func (s *Storage) RestoreTrashItem(id string) (*RestoreResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return nil, err
	}
	trashIndex := -1
	var entry *TrashEntry
	for i, item := range data.Trash {
		if item.ID == id {
			trashIndex, entry = i, item
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("trash item not found")
	}

	result := &RestoreResult{WindowIdx: 0}
	switch entry.Kind {
	case "session":
		if entry.Session == nil {
			return nil, fmt.Errorf("trash session payload is missing")
		}
		for _, instance := range data.Instances {
			if instance.ID == entry.Session.ID || instance.Name == entry.Session.Name {
				return nil, fmt.Errorf("a session with the same ID or name already exists")
			}
		}
		entry.Session.Status = StatusStopped
		entry.Session.MainWindowStopped = false
		if entry.Session.GroupID != "" && !groupExists(data.Groups, entry.Session.GroupID) {
			entry.Session.GroupID = ""
		}
		data.Instances = insertInstance(data.Instances, entry.Session, entry.OriginalPosition)
		result.SessionID = entry.Session.ID
	case "tab":
		if entry.Tab == nil {
			return nil, fmt.Errorf("trash tab payload is missing")
		}
		var parent *Instance
		for _, instance := range data.Instances {
			if instance.ID == entry.ParentSessionID {
				parent = instance
				break
			}
		}
		if parent == nil {
			return nil, fmt.Errorf("parent session no longer exists")
		}
		restored := *entry.Tab
		running := parent.Status == StatusRunning && parent.IsAlive()
		if running {
			workDir := restored.WorkDir
			newIndex, err := parent.NewWindowWithName(restored.Name, workDir)
			if err != nil {
				return nil, err
			}
			// NewWindowWithName added a terminal descriptor. Replace it with
			// the complete trashed metadata before turning the pane into a safe
			// stopped placeholder.
			parent.FollowedWindows = parent.FollowedWindows[:len(parent.FollowedWindows)-1]
			restored.Index = newIndex
			restored.Stopped = false
			insertFollowedWindow(parent, restored, entry.OriginalPosition)
			if err := parent.StopWindow(newIndex); err != nil {
				_ = parent.DeleteWindow(newIndex)
				return nil, err
			}
		} else {
			parent.Status = StatusStopped
			restored.Index = nextStoredWindowIndex(parent)
			restored.Stopped = true
			insertFollowedWindow(parent, restored, entry.OriginalPosition)
		}
		parent.TabOrder = restoreTabOrder(entry.OriginalTabOrder, entry.Tab.Index, restored.Index, parent)
		result.SessionID = parent.ID
		result.WindowIdx = restored.Index
	default:
		return nil, fmt.Errorf("unknown trash item kind")
	}

	data.Trash = append(data.Trash[:trashIndex], data.Trash[trashIndex+1:]...)
	data.SchemaVersion = recoverySchemaVersion
	data.Revision++
	if err := s.writeStorageDataLocked(data, true); err != nil {
		if entry.Kind == "tab" && result.WindowIdx > 0 {
			for _, instance := range data.Instances {
				if instance.ID == result.SessionID && instance.Status == StatusRunning {
					_ = instance.DeleteWindow(result.WindowIdx)
					break
				}
			}
		}
		return nil, err
	}
	return result, nil
}

func groupExists(groups []*Group, id string) bool {
	for _, group := range groups {
		if group.ID == id {
			return true
		}
	}
	return false
}

func insertInstance(instances []*Instance, instance *Instance, position int) []*Instance {
	if position < 0 || position >= len(instances) {
		return append(instances, instance)
	}
	instances = append(instances, nil)
	copy(instances[position+1:], instances[position:])
	instances[position] = instance
	return instances
}

func restoreTabOrder(saved []int, oldIndex, newIndex int, instance *Instance) []int {
	if len(saved) == 0 {
		return nil
	}
	valid := map[int]bool{0: true}
	for _, tab := range instance.FollowedWindows {
		valid[tab.Index] = true
	}
	result := make([]int, 0, len(valid))
	seen := make(map[int]bool, len(valid))
	appendIndex := func(index int) {
		if valid[index] && !seen[index] {
			result = append(result, index)
			seen[index] = true
		}
	}
	for _, index := range saved {
		if index == oldIndex {
			index = newIndex
		}
		appendIndex(index)
	}
	appendIndex(0)
	for _, tab := range instance.FollowedWindows {
		appendIndex(tab.Index)
	}
	return result
}

func nextStoredWindowIndex(instance *Instance) int {
	next := 1
	for _, tab := range instance.FollowedWindows {
		if tab.Index >= next {
			next = tab.Index + 1
		}
	}
	return next
}

func insertFollowedWindow(instance *Instance, tab FollowedWindow, position int) {
	if position < 0 || position >= len(instance.FollowedWindows) {
		instance.FollowedWindows = append(instance.FollowedWindows, tab)
		return
	}
	instance.FollowedWindows = append(instance.FollowedWindows, FollowedWindow{})
	copy(instance.FollowedWindows[position+1:], instance.FollowedWindows[position:])
	instance.FollowedWindows[position] = tab
}

func (s *Storage) PermanentlyDeleteTrashItem(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}
	for i, item := range data.Trash {
		if item.ID == id {
			data.Trash = append(data.Trash[:i], data.Trash[i+1:]...)
			data.Revision++
			return s.writeStorageDataLocked(data, true)
		}
	}
	return fmt.Errorf("trash item not found")
}

func (s *Storage) EmptyTrash() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}
	if len(data.Trash) == 0 {
		return nil
	}
	data.Trash = []*TrashEntry{}
	data.Revision++
	return s.writeStorageDataLocked(data, true)
}
