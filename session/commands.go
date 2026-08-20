package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Saved commands: snippets the user keeps around and sends to a terminal from
// a searchable list, optionally organised into groups.
//
// A command may contain {name} placeholders. Those are collected when it is
// picked so the values can be asked for and substituted, which is what makes
// one entry cover a family of similar commands.

// placeholderRe matches {name} or {name:default}. The name is letters,
// digits, spaces, _ and - only, which keeps awk '{print $1}' and
// find -exec {} out of it. Go's regexp has no lookbehind, so a preceding "$"
// is captured and rejected afterwards — a shell ${VAR} is an environment
// variable, not something to ask the user for.
//
// The default runs to the closing brace and may contain anything but "}",
// so {tail:-n 100} and {msg:hello world} both work.
//
// Most brace syntax is already excluded by the narrow name pattern, but not
// all of it: awk '{print sum}' and jq '{name: .name}' would otherwise look
// like placeholders. Doubling the braces escapes them — {{print sum}} is
// sent as {print sum} and never asked about.
var placeholderRe = regexp.MustCompile(`\{\{([\p{L}\p{N}_][\p{L}\p{N}_ -]*?)(?::([^}]*))?\}\}`)

// Placeholder is one {name} or {name:default} found in a command.
type Placeholder struct {
	Name string `json:"name"`
	// Default is pre-filled in the prompt; empty when none was given.
	Default string `json:"default,omitempty"`
}

// SavedCommand is one entry in the command library.
type SavedCommand struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Command is the text sent to the terminal, possibly with {placeholders}.
	Command string `json:"command"`
	// Description is optional context shown under the name.
	Description string `json:"description,omitempty"`
	// GroupID is empty for ungrouped commands.
	GroupID string `json:"group_id,omitempty"`
	// SendEnter runs the command instead of only typing it. Off by default so
	// a half-remembered command can be reviewed before it executes.
	SendEnter bool      `json:"send_enter,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UsedAt    time.Time `json:"used_at,omitempty"`
	// UseCount drives "most used first" ordering in the picker.
	UseCount int `json:"use_count,omitempty"`
}

// CommandGroup organises saved commands.
type CommandGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Order positions the group in the list; ties fall back to the name.
	Order int `json:"order,omitempty"`
}

// CommandLibrary is the whole saved-command store.
type CommandLibrary struct {
	Commands []SavedCommand `json:"commands"`
	Groups   []CommandGroup `json:"groups,omitempty"`
}

// Placeholders returns the placeholder names in a command, in the order they
// first appear and without duplicates — the same name used twice is one
// question, answered once.
func Placeholders(command string) []string {
	list := PlaceholderList(command)
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.Name)
	}
	return out
}

// PlaceholderList returns the placeholders with their defaults, in the order
// they first appear and without duplicates. When the same name appears twice
// the first default wins — one question, one answer.
func PlaceholderList(command string) []Placeholder {
	matches := placeholderRe.FindAllStringSubmatch(command, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	var out []Placeholder
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, Placeholder{Name: name, Default: m[2]})
	}
	return out
}

// Expand substitutes the given values into a command, falling back to each
// placeholder's default where no value was supplied.
//
// A placeholder with neither is left as-is rather than silently blanked:
// sending "rm -rf " because a value was missing would be worse than sending
// something obviously wrong, and RunCommand refuses on what is left.
func Expand(command string, values map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(command, func(match string) string {
		sub := placeholderRe.FindStringSubmatch(match)
		if sub == nil {
			return match
		}
		name := strings.TrimSpace(sub[1])
		if v, ok := values[name]; ok {
			return v
		}
		// sub[2] is only meaningful when the ":" was actually present;
		// FindStringSubmatch gives "" for both "no default" and "empty
		// default", and an empty default is not worth distinguishing.
		if sub[2] != "" {
			return sub[2]
		}
		return match
	})
}

// Validate checks a command before it is stored.
func (c *SavedCommand) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("the command needs a name")
	}
	if strings.TrimSpace(c.Command) == "" {
		return fmt.Errorf("the command is empty")
	}
	return nil
}

// SortCommands orders the library for display: most used first, then most
// recently used, then by name — so the list settles on what the user reaches
// for while staying predictable for entries never used.
func SortCommands(cmds []SavedCommand) {
	sort.SliceStable(cmds, func(i, j int) bool {
		a, b := cmds[i], cmds[j]
		if a.UseCount != b.UseCount {
			return a.UseCount > b.UseCount
		}
		if !a.UsedAt.Equal(b.UsedAt) {
			return a.UsedAt.After(b.UsedAt)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

// SortGroups orders groups by their explicit order, then by name.
func SortGroups(groups []CommandGroup) {
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Order != groups[j].Order {
			return groups[i].Order < groups[j].Order
		}
		return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name)
	})
}

// --- Persistence ---------------------------------------------------------
//
// The library lives beside the app config rather than inside a project:
// commands like "tail the deploy log" are useful everywhere, and duplicating
// them per project would be busywork.

// commandsPath returns the library file path.
func (s *Storage) commandsPath() string {
	return filepath.Join(s.configDir, "commands.json")
}

// LoadCommands reads the saved commands. A missing file is not an error —
// it just means nothing has been saved yet.
func (s *Storage) LoadCommands() (*CommandLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadCommandsLocked()
}

func (s *Storage) loadCommandsLocked() (*CommandLibrary, error) {
	data, err := os.ReadFile(s.commandsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &CommandLibrary{}, nil
		}
		return nil, err
	}
	var lib CommandLibrary
	if err := json.Unmarshal(data, &lib); err != nil {
		return nil, fmt.Errorf("the saved commands file is unreadable: %w", err)
	}
	return &lib, nil
}

// SaveCommands writes the library, replacing the file atomically so a failed
// write cannot leave a half-written list behind.
func (s *Storage) SaveCommands(lib *CommandLibrary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveCommandsLocked(lib)
}

func (s *Storage) saveCommandsLocked(lib *CommandLibrary) error {
	if lib == nil {
		lib = &CommandLibrary{}
	}
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.configDir, 0o755); err != nil {
		return err
	}
	path := s.commandsPath()
	tmp, err := os.CreateTemp(s.configDir, ".commands-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// UpdateCommands performs a complete load/change/save cycle while holding the
// storage mutex. Callers that edit one entry must use this instead of separate
// LoadCommands and SaveCommands calls, otherwise concurrent Wails requests can
// both save snapshots derived from the same old library and lose one edit.
func (s *Storage) UpdateCommands(change func(*CommandLibrary) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withCrossProcessFileLock(filepath.Join(s.configDir, "commands.lock"), func() error {
		lib, err := s.loadCommandsLocked()
		if err != nil {
			return err
		}
		if err := change(lib); err != nil {
			return err
		}
		return s.saveCommandsLocked(lib)
	})
}
