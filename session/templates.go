package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Session templates: a saved arrangement — the main window's agent plus the
// tabs that usually sit beside it — that creates a whole set-up in one step.
//
// The arrangement is stored as a PortableSession, the same shape the export
// file uses. That type already answers the hard question this feature asks:
// which fields describe the CONFIGURATION and which describe one particular
// run. Duplicating it here would mean maintaining two lists of "what a session
// is" that would drift the first time a field is added to Instance.

// SessionTemplate is one entry in the template library.
type SessionTemplate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Description is optional context shown under the name.
	Description string `json:"description,omitempty"`
	// Session holds the arrangement itself. Its Name is the suggested session
	// name; its Path may be empty, meaning "ask when the template is used"
	// (see TemplateNeedsPath).
	Session   PortableSession `json:"session"`
	CreatedAt time.Time       `json:"created_at,omitempty"`
	UsedAt    time.Time       `json:"used_at,omitempty"`
	// UseCount drives "most used first" ordering, like the command library.
	UseCount int `json:"use_count,omitempty"`
}

// TemplateLibrary is the whole template store.
type TemplateLibrary struct {
	Templates []SessionTemplate `json:"templates"`
}

// TemplateNeedsPath reports whether using this template has to ask for a
// working directory first.
//
// A template with no directory is the reusable kind — "Claude plus a terminal
// plus a test runner" applies to every project — so an empty Path is a
// deliberate state, not a broken one.
func (t *SessionTemplate) TemplateNeedsPath() bool {
	return strings.TrimSpace(t.Session.Path) == ""
}

// Validate checks a template before it is stored. The directory is not
// required: templates without one are prompted for at creation time.
func (t *SessionTemplate) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("the template needs a name")
	}
	return nil
}

// SortTemplates orders the library for display: most used first, then most
// recently used, then by name — matching the saved-command library, so both
// collections settle towards what the user actually reaches for.
func SortTemplates(templates []SessionTemplate) {
	sort.SliceStable(templates, func(i, j int) bool {
		a, b := templates[i], templates[j]
		if a.UseCount != b.UseCount {
			return a.UseCount > b.UseCount
		}
		if !a.UsedAt.Equal(b.UsedAt) {
			return a.UsedAt.After(b.UsedAt)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

// TemplateFromInstance captures a live session's arrangement. Runtime state is
// dropped by ToPortable; what is left is exactly what a template should hold.
//
// The group is not carried: a template is used across projects, where the
// group IDs mean nothing, and the new-session dialog already offers a group.
func TemplateFromInstance(inst *Instance, templateName string, keepPath bool) SessionTemplate {
	bundle := ToPortable([]*Instance{inst}, nil, "")
	ps := PortableSession{}
	if len(bundle.Sessions) > 0 {
		ps = bundle.Sessions[0]
	}
	if !keepPath {
		ps.Path = ""
	}
	// Favourite is a per-session mark ("this one matters right now"), not part
	// of an arrangement — a template that starred everything it created would
	// fill the favourites group with noise.
	ps.Favorite = false

	name := strings.TrimSpace(templateName)
	if name == "" {
		name = inst.Name
	}
	return SessionTemplate{
		Name:      name,
		Session:   ps,
		CreatedAt: time.Now(),
	}
}

// InstantiateTemplate builds the Instance a template describes, ready to be
// stored and started.
//
// The tabs are carried as FollowedWindows in the stopped-session state that
// Start() already knows how to realise: restoreFollowedWindows() walks that
// list and spawns a tmux window per entry, building each tab's agent command
// from its own Agent/AutoYes/ExtraArgs. Constructing the tabs here rather than
// issuing tmux commands keeps a single implementation of "how a tab is
// spawned" for both restart and template use.
//
// path overrides the template's own directory; it is required when the
// template has none.
func (t *SessionTemplate) InstantiateTemplate(name, path string) (*Instance, error) {
	ps := t.Session

	if strings.TrimSpace(path) != "" {
		ps.Path = path
	}
	if strings.TrimSpace(ps.Path) == "" {
		return nil, fmt.Errorf("this template has no working directory — choose one")
	}
	// Same normalisation NewInstance does, so a template used with "~/work"
	// or a relative path behaves like a hand-created session.
	absPath, err := filepath.Abs(expandTilde(ps.Path))
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	if info, err := os.Stat(absPath); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("path does not exist: %s", absPath)
	}
	ps.Path = absPath

	if n := strings.TrimSpace(name); n != "" {
		ps.Name = n
	}
	if strings.TrimSpace(ps.Name) == "" {
		ps.Name = filepath.Base(absPath)
	}

	// A tab's WorkDir is only stored when it differs from the session path, so
	// a template moved to another directory brings its tabs along with it. A
	// tab that was deliberately pinned elsewhere keeps its own absolute path.
	return ps.FromPortable(""), nil
}

// --- Persistence ---------------------------------------------------------
//
// Beside the app config rather than inside a project, for the same reason the
// command library is: an arrangement is worth reusing across projects, and
// copying it per project would be busywork.

// templatesPath returns the library file path.
func (s *Storage) templatesPath() string {
	return filepath.Join(s.configDir, "templates.json")
}

// LoadTemplates reads the saved templates. A missing file is not an error —
// it just means nothing has been saved yet.
func (s *Storage) LoadTemplates() (*TemplateLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadTemplatesLocked()
}

func (s *Storage) loadTemplatesLocked() (*TemplateLibrary, error) {
	data, err := os.ReadFile(s.templatesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &TemplateLibrary{}, nil
		}
		return nil, err
	}
	var lib TemplateLibrary
	if err := json.Unmarshal(data, &lib); err != nil {
		return nil, fmt.Errorf("the session templates file is unreadable: %w", err)
	}
	return &lib, nil
}

// SaveTemplates writes the library, replacing the file atomically so a failed
// write cannot leave a half-written list behind.
func (s *Storage) SaveTemplates(lib *TemplateLibrary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveTemplatesLocked(lib)
}

func (s *Storage) saveTemplatesLocked(lib *TemplateLibrary) error {
	if lib == nil {
		lib = &TemplateLibrary{}
	}
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.configDir, 0o755); err != nil {
		return err
	}
	path := s.templatesPath()
	tmp, err := os.CreateTemp(s.configDir, ".templates-*")
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

// UpdateTemplates performs a complete load/change/save cycle under one lock,
// preventing concurrent template edits or usage updates from overwriting one
// another with stale snapshots.
func (s *Storage) UpdateTemplates(change func(*TemplateLibrary) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return withCrossProcessFileLock(filepath.Join(s.configDir, "templates.lock"), func() error {
		lib, err := s.loadTemplatesLocked()
		if err != nil {
			return err
		}
		if err := change(lib); err != nil {
			return err
		}
		return s.saveTemplatesLocked(lib)
	})
}
