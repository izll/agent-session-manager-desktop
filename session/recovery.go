package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	recoverySchemaVersion = 1
	backupRetentionCount  = 25
)

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
	if len(existing) > 0 {
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

func pruneBackupDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	files := backupJSONEntries(entries)
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for len(files) > backupRetentionCount {
		if err := os.Remove(filepath.Join(dir, files[0].Name())); err != nil {
			return err
		}
		files = files[1:]
	}
	return nil
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

func (s *Storage) CreateBackup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadStorageDataLocked()
	if err != nil {
		return err
	}
	return s.createAutomaticBackupLocked(data)
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

	raw, err := os.ReadFile(filepath.Join(s.backupDirLocked(), id))
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
	if err := s.createAutomaticBackupLocked(data); err != nil {
		return fmt.Errorf("failed to create pre-delete backup: %w", err)
	}
	instance := data.Instances[index]
	if err := instance.Stop(); err != nil {
		return err
	}
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
	data.SchemaVersion = recoverySchemaVersion
	data.Revision++
	return s.writeStorageDataLocked(data, true)
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
		return fmt.Errorf("tab not found")
	}
	if err := s.createAutomaticBackupLocked(data); err != nil {
		return fmt.Errorf("failed to create pre-delete backup: %w", err)
	}
	if err := parent.DeleteWindow(windowIdx); err != nil {
		return err
	}
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
	data.SchemaVersion = recoverySchemaVersion
	data.Revision++
	return s.writeStorageDataLocked(data, true)
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
			if err := parent.NewWindowWithName(restored.Name, workDir); err != nil {
				return nil, err
			}
			newIndex := parent.GetCurrentWindowIndex()
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
