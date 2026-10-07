package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	commandLibraryMaxBytes  = 16 << 20
	commandLibraryMaxItems  = 10000
	commandLibraryMaxGroups = 1000
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
	SendEnter bool `json:"send_enter,omitempty"`
	// InBackground is the old two-way form of RunIn, kept so a library
	// written before RunIn existed still runs its commands where it meant
	// to. Read through RunTarget, never directly; writers set RunIn.
	InBackground bool `json:"in_background,omitempty"`
	// RunIn is where the command runs: RunInCurrent types it into the tab it
	// was picked from, RunInNewTab opens a tab and switches to it, and
	// RunInBackground opens one without switching, so a build or a server
	// does not take over the tab it was started from.
	//
	// A tab of its own always runs the command: typing it into a tab nobody
	// is looking at would do nothing.
	RunIn     string    `json:"run_in,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UsedAt    time.Time `json:"used_at,omitempty"`
	// UseCount drives "most used first" ordering in the picker.
	UseCount int `json:"use_count,omitempty"`
}

// Where a saved command runs.
const (
	RunInCurrent    = "current"
	RunInNewTab     = "newtab"
	RunInBackground = "background"
)

// ValidRunIn reports whether target is one of the three run targets.
func ValidRunIn(target string) bool {
	switch target {
	case RunInCurrent, RunInNewTab, RunInBackground:
		return true
	}
	return false
}

// RunTarget is where this command runs, resolving the older InBackground
// flag for libraries written before RunIn existed.
//
// An unset RunIn means one of two different things, which is why the old
// flag cannot simply be dropped: with InBackground set, the command was
// saved to run in a tab of its own, and answering "current" would move a
// running server into the tab the user was reading.
func (c *SavedCommand) RunTarget() string {
	if ValidRunIn(c.RunIn) {
		return c.RunIn
	}
	if c.InBackground {
		return RunInBackground
	}
	return RunInCurrent
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
	// Empty is allowed: it is how a library written before RunIn existed
	// reads, and RunTarget resolves it. An unrecognised value is not, so a
	// typo cannot silently become "type it into the tab you are reading".
	if c.RunIn != "" && !ValidRunIn(c.RunIn) {
		return fmt.Errorf("unknown run target %q", c.RunIn)
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
// There are two libraries, and the difference is which commands belong
// everywhere.
//
// The shared one lives beside the app config, because commands like "tail the
// deploy log" are useful in every project and duplicating them per project
// would be busywork. That was the only library at first, and it is still the
// one a command goes to by default.
//
// A project library sits beside that project's sessions. It exists because
// the shared list is where a build command does not belong: "./gradlew
// bootRun" is an offer in a Java project and noise in a Node one, and a
// single list shows every entry everywhere. The two are read together, the
// project's first, so nothing had to move for this to arrive.

// ErrNoActiveProject is returned when a project-only operation is asked for
// while the default (unnamed) project is active, which has no library of its
// own. A sentinel rather than a message, so callers can tell this apart from
// a real failure without matching on text.
var ErrNoActiveProject = errors.New("no project is active")

// commandsPath returns the shared library's file path.
func (s *Storage) commandsPath() string {
	return filepath.Join(s.configDir, "commands.json")
}

// projectCommandsPath returns the active project's library file, or "" when
// the default (unnamed) project is active — that one has no directory of its
// own, so its commands are the shared ones.
func (s *Storage) projectCommandsPath() string {
	if s.projectID == "" {
		return ""
	}
	return filepath.Join(s.configDir, "projects", s.projectID, "commands.json")
}

// LoadProjectCommands reads the active project's own library. A missing file,
// or no active project, means an empty one rather than an error.
func (s *Storage) LoadProjectCommands() (*CommandLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadProjectCommandsLocked()
}

func (s *Storage) loadProjectCommandsLocked() (*CommandLibrary, error) {
	path := s.projectCommandsPath()
	if path == "" {
		return &CommandLibrary{}, nil
	}
	data, err := readFileAtMost(path, commandLibraryMaxBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return &CommandLibrary{}, nil
		}
		return nil, err
	}
	var lib CommandLibrary
	if err := json.Unmarshal(data, &lib); err != nil {
		return nil, fmt.Errorf("the project's saved commands file is unreadable: %w", err)
	}
	if err := validateCommandLibrary(&lib); err != nil {
		return nil, fmt.Errorf("the project's saved commands file is invalid: %w", err)
	}
	return &lib, nil
}

// UpdateProjectCommands applies change to the active project's library under
// the same cross-process lock the shared one uses.
//
// Refuses when no project is active: writing would land in the shared
// library, which is not what a caller asking for the project's one meant.
func (s *Storage) UpdateProjectCommands(change func(*CommandLibrary) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.projectCommandsPath()
	if path == "" {
		return ErrNoActiveProject
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create the project directory: %w", err)
	}
	return withCrossProcessFileLock(filepath.Join(dir, "commands.lock"), func() error {
		lib, err := s.loadProjectCommandsLocked()
		if err != nil {
			return err
		}
		if err := change(lib); err != nil {
			return err
		}
		return s.writeCommandsTo(path, lib)
	})
}

// LoadCommands reads the saved commands. A missing file is not an error —
// it just means nothing has been saved yet.
func (s *Storage) LoadCommands() (*CommandLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadCommandsLocked()
}

func (s *Storage) loadCommandsLocked() (*CommandLibrary, error) {
	data, err := readFileAtMost(s.commandsPath(), commandLibraryMaxBytes)
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
	if err := validateCommandLibrary(&lib); err != nil {
		return nil, fmt.Errorf("the saved commands file is invalid: %w", err)
	}
	return &lib, nil
}

func validateCommandLibrary(lib *CommandLibrary) error {
	if lib == nil {
		return nil
	}
	if len(lib.Commands) > commandLibraryMaxItems || len(lib.Groups) > commandLibraryMaxGroups {
		return fmt.Errorf("library exceeds the item limit")
	}
	groups := make(map[string]struct{}, len(lib.Groups))
	for index, group := range lib.Groups {
		if strings.TrimSpace(group.ID) == "" || strings.TrimSpace(group.Name) == "" {
			return fmt.Errorf("group %d has an empty ID or name", index)
		}
		if _, duplicate := groups[group.ID]; duplicate {
			return fmt.Errorf("duplicate group ID %q", group.ID)
		}
		groups[group.ID] = struct{}{}
	}
	commands := make(map[string]struct{}, len(lib.Commands))
	for index := range lib.Commands {
		command := &lib.Commands[index]
		if strings.TrimSpace(command.ID) == "" {
			return fmt.Errorf("command %d has an empty ID", index)
		}
		if _, duplicate := commands[command.ID]; duplicate {
			return fmt.Errorf("duplicate command ID %q", command.ID)
		}
		if err := command.Validate(); err != nil {
			return fmt.Errorf("command %q is invalid: %w", command.ID, err)
		}
		if command.GroupID != "" {
			if _, exists := groups[command.GroupID]; !exists {
				return fmt.Errorf("command %q references missing group %q", command.ID, command.GroupID)
			}
		}
		commands[command.ID] = struct{}{}
	}
	return nil
}

// SaveCommands writes the library, replacing the file atomically so a failed
// write cannot leave a half-written list behind.
func (s *Storage) SaveCommands(lib *CommandLibrary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withCrossProcessFileLock(filepath.Join(s.configDir, "commands.lock"), func() error {
		return s.saveCommandsLocked(lib)
	})
}

func (s *Storage) saveCommandsLocked(lib *CommandLibrary) error {
	if err := os.MkdirAll(s.configDir, 0o755); err != nil {
		return err
	}
	return s.writeCommandsTo(s.commandsPath(), lib)
}

// writeCommandsTo validates a library and writes it to path. Shared by the
// two libraries so neither can be written unvalidated or non-atomically.
func (s *Storage) writeCommandsTo(path string, lib *CommandLibrary) error {
	if lib == nil {
		lib = &CommandLibrary{}
	}
	if err := validateCommandLibrary(lib); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return err
	}
	// Through the shared helper so the write is fsynced before the rename.
	return writeFileAtomic(path, data, 0600)
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
