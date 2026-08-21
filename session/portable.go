package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Exporting sessions to a file and reading them back, so a set-up can be moved
// to another machine or kept as a snapshot.
//
// Runtime state is deliberately left out: tmux window indices, the running
// status, the git commit a diff was based on and resume IDs all describe THIS
// machine at THIS moment. Carrying them over would produce sessions that look
// live but point at nothing.

// PortableFormat identifies the file layout, so a future change can be
// detected rather than mis-parsed.
const PortableFormat = "asmgr-sessions"

// PortableVersion is the current layout revision.
const PortableVersion = 1

const (
	portableFileLimit      = 16 << 20
	portableSessionLimit   = 1000
	portableGroupLimit     = 1000
	portableTabsPerSession = 256
)

// PortableTab is one tab of an exported session.
type PortableTab struct {
	Name            string    `json:"name"`
	Agent           AgentType `json:"agent,omitempty"`
	CustomCommand   string    `json:"custom_command,omitempty"`
	AutoYes         bool      `json:"auto_yes,omitempty"`
	ExtraArgs       string    `json:"extra_args,omitempty"`
	Notes           string    `json:"notes,omitempty"`
	WorkDir         string    `json:"work_dir,omitempty"`
	TerminalTheme   string    `json:"terminal_theme,omitempty"`
	TextColor       string    `json:"text_color,omitempty"`
	BackgroundColor string    `json:"background_color,omitempty"`
	HideStatusLine  bool      `json:"hide_status_line,omitempty"`
}

// PortableSession is one exported session: its configuration, without any
// machine- or run-specific state.
type PortableSession struct {
	Name               string        `json:"name"`
	Path               string        `json:"path"`
	Agent              AgentType     `json:"agent,omitempty"`
	CustomCommand      string        `json:"custom_command,omitempty"`
	ExtraArgs          string        `json:"extra_args,omitempty"`
	AutoYes            bool          `json:"auto_yes,omitempty"`
	Notes              string        `json:"notes,omitempty"`
	Color              string        `json:"color,omitempty"`
	BgColor            string        `json:"bg_color,omitempty"`
	FullRowColor       bool          `json:"full_row_color,omitempty"`
	Favorite           bool          `json:"favorite,omitempty"`
	HideStatusLine     bool          `json:"hide_status_line,omitempty"`
	TerminalTheme      string        `json:"terminal_theme,omitempty"`
	TabTextColor       string        `json:"tab_text_color,omitempty"`
	TabBackgroundColor string        `json:"tab_background_color,omitempty"`
	MainWindowName     string        `json:"main_window_name,omitempty"`
	GroupName          string        `json:"group_name,omitempty"` // by name, not ID: IDs are per-install
	Tabs               []PortableTab `json:"tabs,omitempty"`
}

// PortableGroup is an exported group. Colours travel; the ID does not.
type PortableGroup struct {
	Name         string `json:"name"`
	Color        string `json:"color,omitempty"`
	BgColor      string `json:"bg_color,omitempty"`
	FullRowColor bool   `json:"full_row_color,omitempty"`
}

// PortableBundle is the whole exported file.
type PortableBundle struct {
	Format     string            `json:"format"`
	Version    int               `json:"version"`
	ExportedAt time.Time         `json:"exported_at"`
	AppVersion string            `json:"app_version,omitempty"`
	Groups     []PortableGroup   `json:"groups,omitempty"`
	Sessions   []PortableSession `json:"sessions"`
}

// ToPortable converts live instances into the exportable form, resolving group
// IDs to names so the file makes sense on another install.
func ToPortable(instances []*Instance, groups []*Group, appVersion string) *PortableBundle {
	groupNameByID := make(map[string]string, len(groups))
	for _, g := range groups {
		groupNameByID[g.ID] = g.Name
	}

	usedGroups := map[string]bool{}
	out := make([]PortableSession, 0, len(instances))
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		ps := PortableSession{
			Name:               inst.Name,
			Path:               inst.Path,
			Agent:              inst.Agent,
			CustomCommand:      inst.CustomCommand,
			ExtraArgs:          inst.ExtraArgs,
			AutoYes:            inst.AutoYes,
			Notes:              inst.Notes,
			Color:              inst.Color,
			BgColor:            inst.BgColor,
			FullRowColor:       inst.FullRowColor,
			Favorite:           inst.Favorite,
			HideStatusLine:     inst.HideStatusLine,
			TerminalTheme:      inst.TerminalTheme,
			TabTextColor:       inst.TabTextColor,
			TabBackgroundColor: inst.TabBackgroundColor,
			MainWindowName:     inst.MainWindowName,
		}
		if name, ok := groupNameByID[inst.GroupID]; ok && name != "" {
			ps.GroupName = name
			usedGroups[inst.GroupID] = true
		}
		for _, w := range inst.FollowedWindows {
			ps.Tabs = append(ps.Tabs, PortableTab{
				Name:            w.Name,
				Agent:           w.Agent,
				CustomCommand:   w.CustomCommand,
				AutoYes:         w.AutoYes,
				ExtraArgs:       w.ExtraArgs,
				Notes:           w.Notes,
				WorkDir:         w.WorkDir,
				TerminalTheme:   w.TerminalTheme,
				TextColor:       w.TextColor,
				BackgroundColor: w.BackgroundColor,
				HideStatusLine:  w.HideStatusLine,
			})
		}
		out = append(out, ps)
	}

	bundle := &PortableBundle{
		Format:     PortableFormat,
		Version:    PortableVersion,
		ExportedAt: time.Now(),
		AppVersion: appVersion,
		Sessions:   out,
	}
	for _, g := range groups {
		if usedGroups[g.ID] {
			bundle.Groups = append(bundle.Groups, PortableGroup{
				Name:         g.Name,
				Color:        g.Color,
				BgColor:      g.BgColor,
				FullRowColor: g.FullRowColor,
			})
		}
	}
	return bundle
}

// WritePortable writes the bundle as indented JSON — it is meant to be
// readable and diffable, not compact.
func WritePortable(w io.Writer, bundle *PortableBundle) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(bundle)
}

// ReadPortable parses an exported file, rejecting anything that isn't one.
func ReadPortable(r io.Reader) (*PortableBundle, error) {
	data, err := io.ReadAll(io.LimitReader(r, portableFileLimit+1))
	if err != nil {
		return nil, fmt.Errorf("this is not a readable session file: %w", err)
	}
	if len(data) > portableFileLimit {
		return nil, fmt.Errorf("this session file exceeds %d bytes", portableFileLimit)
	}
	// Count collection elements before decoding them into the comparatively
	// large Go structs. A small JSON file such as [{},{},...] otherwise has a
	// large memory-amplification factor and can exhaust the process before a
	// post-decode length check gets a chance to reject it.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("this is not a readable session file: %w", err)
	}
	if err := validatePortableCollectionSizes(raw); err != nil {
		return nil, err
	}

	var bundle PortableBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("this is not a readable session file: %w", err)
	}
	if bundle.Format != PortableFormat {
		return nil, fmt.Errorf("this file is not a session export")
	}
	if bundle.Version > PortableVersion {
		return nil, fmt.Errorf("this file was written by a newer version of the app (format %d); update to read it", bundle.Version)
	}
	if len(bundle.Sessions) == 0 {
		return nil, fmt.Errorf("the file contains no sessions")
	}
	return &bundle, nil
}

func validatePortableCollectionSizes(raw map[string]json.RawMessage) error {
	sessions, err := portableJSONArrayItems(raw["sessions"], portableSessionLimit, "sessions")
	if err != nil {
		return err
	}
	if len(raw["groups"]) > 0 {
		if _, err := portableJSONArrayItems(raw["groups"], portableGroupLimit, "groups"); err != nil {
			return err
		}
	}
	for _, item := range sessions {
		var shape struct {
			Tabs json.RawMessage `json:"tabs"`
		}
		if err := json.Unmarshal(item, &shape); err != nil {
			return fmt.Errorf("this session file contains an invalid session: %w", err)
		}
		if len(shape.Tabs) > 0 {
			if _, err := portableJSONArrayItems(shape.Tabs, portableTabsPerSession, "tabs per session"); err != nil {
				return err
			}
		}
	}
	return nil
}

func portableJSONArrayItems(raw json.RawMessage, limit int, label string) ([]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("this session file has invalid %s: %w", label, err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '[' {
		return nil, fmt.Errorf("this session file has invalid %s", label)
	}
	items := make([]json.RawMessage, 0, min(limit, 16))
	for decoder.More() {
		if len(items) >= limit {
			return nil, fmt.Errorf("this session file contains too many %s (maximum %d)", label, limit)
		}
		var item json.RawMessage
		if err := decoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("this session file has invalid %s: %w", label, err)
		}
		items = append(items, item)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("this session file has invalid %s: %w", label, err)
	}
	return items, nil
}

// PathExists reports whether a session's directory is present on this machine.
// Paths are absolute and machine-specific, so an import from elsewhere will
// often point at directories that don't exist here.
func PathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// FromPortable turns an exported session back into an Instance ready to be
// stored. It gets a fresh ID and starts stopped: nothing about the exporting
// machine's runtime state carries over.
func (p PortableSession) FromPortable(groupID string) *Instance {
	now := time.Now()
	inst := &Instance{
		ID:                 generateID(p.Name, p.Agent),
		Name:               p.Name,
		Path:               p.Path,
		Status:             StatusStopped,
		CreatedAt:          now,
		UpdatedAt:          now,
		Agent:              p.Agent,
		CustomCommand:      p.CustomCommand,
		ExtraArgs:          p.ExtraArgs,
		AutoYes:            p.AutoYes,
		Notes:              p.Notes,
		Color:              p.Color,
		BgColor:            p.BgColor,
		FullRowColor:       p.FullRowColor,
		Favorite:           p.Favorite,
		HideStatusLine:     p.HideStatusLine,
		TerminalTheme:      p.TerminalTheme,
		TabTextColor:       p.TabTextColor,
		TabBackgroundColor: p.TabBackgroundColor,
		MainWindowName:     p.MainWindowName,
		GroupID:            groupID,
	}
	// Tab indices are assigned fresh: the exporting machine's tmux window
	// numbers mean nothing here.
	for i, t := range p.Tabs {
		inst.FollowedWindows = append(inst.FollowedWindows, FollowedWindow{
			Index:           i + 1,
			Name:            t.Name,
			Agent:           t.Agent,
			CustomCommand:   t.CustomCommand,
			AutoYes:         t.AutoYes,
			ExtraArgs:       t.ExtraArgs,
			Notes:           t.Notes,
			WorkDir:         t.WorkDir,
			TerminalTheme:   t.TerminalTheme,
			TextColor:       t.TextColor,
			BackgroundColor: t.BackgroundColor,
			HideStatusLine:  t.HideStatusLine,
		})
	}
	return inst
}

// ImportPortableSessions adds configuration snapshots to the active project as
// fresh, stopped instances in one storage transaction. Reusing a source ID
// would make two projects address the same tmux session, so identity and every
// runtime field are deliberately regenerated through FromPortable.
func (s *Storage) ImportPortableSessions(sessions []PortableSession, portableGroups []PortableGroup) (int, error) {
	if len(sessions) == 0 {
		return 0, fmt.Errorf("there are no sessions to import")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadStorageDataLocked()
	if err != nil {
		return 0, err
	}
	takenNames := make(map[string]bool, len(data.Instances)+len(sessions))
	takenIDs := make(map[string]bool, len(data.Instances)+len(sessions))
	for _, instance := range data.Instances {
		if instance == nil {
			return 0, fmt.Errorf("active storage contains a null session")
		}
		takenNames[instance.Name] = true
		takenIDs[instance.ID] = true
	}

	groupByName := make(map[string]string, len(data.Groups))
	groupIDs := make(map[string]bool, len(data.Groups))
	for _, group := range data.Groups {
		if group == nil {
			return 0, fmt.Errorf("active storage contains a null group")
		}
		groupByName[group.Name] = group.ID
		groupIDs[group.ID] = true
	}
	portableGroupByName := make(map[string]PortableGroup, len(portableGroups))
	for _, group := range portableGroups {
		if strings.TrimSpace(group.Name) != "" {
			portableGroupByName[group.Name] = group
		}
	}

	ensureGroup := func(name string) string {
		if name == "" {
			return ""
		}
		if id := groupByName[name]; id != "" {
			return id
		}
		id := fmt.Sprintf("grp_%d", time.Now().UnixNano())
		for groupIDs[id] {
			id = fmt.Sprintf("grp_%d", time.Now().UnixNano())
		}
		metadata := portableGroupByName[name]
		data.Groups = append(data.Groups, &Group{
			ID: id, Name: name, Color: metadata.Color,
			BgColor: metadata.BgColor, FullRowColor: metadata.FullRowColor,
		})
		groupByName[name] = id
		groupIDs[id] = true
		return id
	}

	for _, portable := range sessions {
		portable.Name = uniquePortableSessionName(portable.Name, takenNames)
		takenNames[portable.Name] = true
		instance := portable.FromPortable(ensureGroup(portable.GroupName))
		for takenIDs[instance.ID] {
			instance.ID = generateID(instance.Name, instance.Agent)
		}
		takenIDs[instance.ID] = true
		data.Instances = append(data.Instances, instance)
	}
	data.SchemaVersion = recoverySchemaVersion
	data.Revision++
	if err := s.writeStorageDataLocked(data, true); err != nil {
		return 0, err
	}
	return len(sessions), nil
}

func uniquePortableSessionName(base string, taken map[string]bool) string {
	name := strings.TrimSpace(base)
	if name == "" {
		name = "Imported session"
	}
	if !taken[name] {
		return name
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s (%d)", name, suffix)
		if !taken[candidate] {
			return candidate
		}
	}
}
