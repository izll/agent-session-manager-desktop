package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"asmgr-desktop/session"
)

// Frontend API for the session-template library: the list, editing it, and
// creating a session (main window plus every tab) from one entry.

// TemplateTabInfo is one tab of a template as the frontend sees it.
type TemplateTabInfo struct {
	Name          string `json:"name"`
	Agent         string `json:"agent"`
	CustomCommand string `json:"customCommand"`
	AutoYes       bool   `json:"autoYes"`
	ExtraArgs     string `json:"extraArgs"`
	// WorkDir is empty for the usual case of "wherever the session is", which
	// is what lets a directory-less template follow the directory it is used in.
	WorkDir string `json:"workDir"`
}

// SessionTemplateInfo is one template as the frontend sees it.
type SessionTemplateInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// SessionName is the session name the template suggests; the create dialog
	// pre-fills it and the user may override.
	SessionName string `json:"sessionName"`
	Path        string `json:"path"`
	Agent       string `json:"agent"`
	AutoYes     bool   `json:"autoYes"`
	ExtraArgs   string `json:"extraArgs"`
	// NeedsPath is derived: true when the template has no directory of its own
	// and one must be chosen before it can be used.
	NeedsPath bool              `json:"needsPath"`
	Tabs      []TemplateTabInfo `json:"tabs"`
	UseCount  int               `json:"useCount"`
}

func templateToInfo(t session.SessionTemplate) SessionTemplateInfo {
	out := SessionTemplateInfo{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		SessionName: t.Session.Name,
		Path:        t.Session.Path,
		Agent:       string(t.Session.Agent),
		AutoYes:     t.Session.AutoYes,
		ExtraArgs:   t.Session.ExtraArgs,
		NeedsPath:   t.TemplateNeedsPath(),
		Tabs:        make([]TemplateTabInfo, 0, len(t.Session.Tabs)),
		UseCount:    t.UseCount,
	}
	for _, tab := range t.Session.Tabs {
		out.Tabs = append(out.Tabs, TemplateTabInfo{
			Name:          tab.Name,
			Agent:         string(tab.Agent),
			CustomCommand: tab.CustomCommand,
			AutoYes:       tab.AutoYes,
			ExtraArgs:     tab.ExtraArgs,
			WorkDir:       tab.WorkDir,
		})
	}
	return out
}

// GetSessionTemplates returns the templates, most used first.
func (a *App) GetSessionTemplates() ([]SessionTemplateInfo, error) {
	lib, err := a.storage.LoadTemplates()
	if err != nil {
		return nil, err
	}
	session.SortTemplates(lib.Templates)

	out := make([]SessionTemplateInfo, 0, len(lib.Templates))
	for _, t := range lib.Templates {
		out = append(out, templateToInfo(t))
	}
	return out, nil
}

// SaveSessionTemplate adds or updates one template. An empty ID creates a new
// entry. An empty path is allowed and means "ask when the template is used".
func (a *App) SaveSessionTemplate(id, name, description, sessionName, path, agent string, autoYes bool, extraArgs string, tabs []TemplateTabInfo) (string, error) {
	entry := session.SessionTemplate{
		ID:          id,
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Session: session.PortableSession{
			Name:      strings.TrimSpace(sessionName),
			Path:      strings.TrimSpace(path),
			Agent:     session.AgentType(agent),
			AutoYes:   autoYes,
			ExtraArgs: strings.TrimSpace(extraArgs),
		},
	}
	for _, tab := range tabs {
		entry.Session.Tabs = append(entry.Session.Tabs, session.PortableTab{
			Name:          strings.TrimSpace(tab.Name),
			Agent:         session.AgentType(tab.Agent),
			CustomCommand: strings.TrimSpace(tab.CustomCommand),
			AutoYes:       tab.AutoYes,
			ExtraArgs:     strings.TrimSpace(tab.ExtraArgs),
			WorkDir:       strings.TrimSpace(tab.WorkDir),
		})
	}
	if err := entry.Validate(); err != nil {
		return "", err
	}

	err := a.storage.UpdateTemplates(func(lib *session.TemplateLibrary) error {
		if id == "" {
			entry.ID = session.NewUniqueID("tpl", takenTemplateIDs(lib.Templates))
			entry.CreatedAt = time.Now()
			entry.Name = uniqueTemplateName(entry.Name, lib.Templates, "")
			lib.Templates = append(lib.Templates, entry)
			return nil
		}
		for i := range lib.Templates {
			if lib.Templates[i].ID == id {
				// Usage statistics belong to the template, not to this edit.
				entry.CreatedAt = lib.Templates[i].CreatedAt
				entry.UsedAt = lib.Templates[i].UsedAt
				entry.UseCount = lib.Templates[i].UseCount
				entry.Name = uniqueTemplateName(entry.Name, lib.Templates, id)
				lib.Templates[i] = entry
				return nil
			}
		}
		return fmt.Errorf("no such template")
	})
	if err != nil {
		return "", err
	}
	return entry.ID, nil
}

// SaveSessionAsTemplate captures an existing session's arrangement — its agent
// and all of its tabs — as a new template.
//
// keepPath decides whether the template stays pinned to this session's
// directory or becomes reusable across projects.
func (a *App) SaveSessionAsTemplate(sessionID, templateName string, keepPath bool, expectedProjectID string) (string, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return "", err
	}
	defer done()

	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return "", err
	}

	entry := session.TemplateFromInstance(inst, templateName, keepPath)
	if err := entry.Validate(); err != nil {
		return "", err
	}
	err = a.storage.UpdateTemplates(func(lib *session.TemplateLibrary) error {
		entry.ID = session.NewUniqueID("tpl", takenTemplateIDs(lib.Templates))
		entry.Name = uniqueTemplateName(entry.Name, lib.Templates, "")
		lib.Templates = append(lib.Templates, entry)
		return nil
	})
	if err != nil {
		return "", err
	}
	log.Printf("[templates] saved %q from session %s (%d tabs)", entry.Name, sessionID, len(entry.Session.Tabs))
	return entry.ID, nil
}

// DeleteSessionTemplate removes one template. Sessions already created from it
// are untouched — a template is a starting point, not a live link.
func (a *App) DeleteSessionTemplate(id string) error {
	return a.storage.UpdateTemplates(func(lib *session.TemplateLibrary) error {
		out := lib.Templates[:0]
		removed := false
		for _, t := range lib.Templates {
			if t.ID == id {
				removed = true
				continue
			}
			out = append(out, t)
		}
		if !removed {
			return fmt.Errorf("no such template")
		}
		lib.Templates = out
		return nil
	})
}

// CreateSessionFromTemplate creates a session with the template's main window
// AND all of its tabs.
//
// The tabs are stored as followed windows on the stopped instance; Start()
// then spawns one tmux window per tab through restoreFollowedWindows(). That
// is the same path a restart takes, so a template tab starts exactly like a
// hand-made one.
//
// name and path override the template's own; path is required when the
// template has none.
func (a *App) CreateSessionFromTemplate(id, name, path, expectedProjectID string) (*SessionInfo, error) {
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return nil, err
	}
	defer done()

	lib, err := a.storage.LoadTemplates()
	if err != nil {
		return nil, err
	}
	var found *session.SessionTemplate
	for i := range lib.Templates {
		if lib.Templates[i].ID == id {
			found = &lib.Templates[i]
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("no such template")
	}

	inst, err := found.InstantiateTemplate(name, path)
	if err != nil {
		return nil, err
	}
	// Same refusal as CreateSession, for the same reason: a session saved
	// without the multiplexer or the agent it needs is a permanent sidebar entry
	// that can never run. This path was missing both checks.
	if err := session.CheckMultiplexer(); err != nil {
		return nil, err
	}
	if err := session.CheckAgentCommand(inst); err != nil {
		return nil, err
	}
	if err := a.storage.AddInstance(inst); err != nil {
		return nil, err
	}

	// A failed statistics write must not look like a failed creation: the
	// session already exists.
	if serr := a.storage.UpdateTemplates(func(latest *session.TemplateLibrary) error {
		for i := range latest.Templates {
			if latest.Templates[i].ID == id {
				latest.Templates[i].UsedAt = time.Now()
				latest.Templates[i].UseCount++
				return nil
			}
		}
		return fmt.Errorf("no such template")
	}); serr != nil {
		log.Printf("[templates] could not record use of %s: %v", id, serr)
	}

	log.Printf("[templates] created session %s from %q with %d tabs", inst.ID, found.Name, len(inst.FollowedWindows))
	info := a.instanceToSessionInfo(inst)
	return &info, nil
}

// uniqueTemplateName appends a counter until the name is free. Two templates
// with the same name in a picker list are indistinguishable, and the user
// would have no way to tell which one they are about to use.
func uniqueTemplateName(base string, existing []session.SessionTemplate, ignoreID string) string {
	taken := make(map[string]bool, len(existing))
	for _, t := range existing {
		if t.ID == ignoreID {
			continue
		}
		taken[t.Name] = true
	}
	name := strings.TrimSpace(base)
	if name == "" {
		name = "Template"
	}
	if !taken[name] {
		return name
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)", name, n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// takenTemplateIDs collects the template IDs already in use.
func takenTemplateIDs(templates []session.SessionTemplate) map[string]bool {
	taken := make(map[string]bool, len(templates))
	for _, t := range templates {
		taken[t.ID] = true
	}
	return taken
}
