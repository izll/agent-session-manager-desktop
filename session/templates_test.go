package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestUpdateTemplatesKeepsConcurrentEdits(t *testing.T) {
	dir := t.TempDir()
	storages := []*Storage{{configDir: dir}, {configDir: dir}}
	const count = 40
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- storages[i%len(storages)].UpdateTemplates(func(lib *TemplateLibrary) error {
				lib.Templates = append(lib.Templates, SessionTemplate{ID: fmt.Sprintf("tpl-%d", i), Name: "name"})
				return nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	lib, err := storages[0].LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Templates) != count {
		t.Fatalf("saved %d templates, want %d", len(lib.Templates), count)
	}
}

func TestTemplateLibraryRoundTrips(t *testing.T) {
	dir := t.TempDir()
	s := &Storage{configDir: dir}

	// Nothing saved yet is not an error.
	lib, err := s.LoadTemplates()
	if err != nil {
		t.Fatalf("loading a missing library failed: %v", err)
	}
	if len(lib.Templates) != 0 {
		t.Errorf("fresh library has %d templates", len(lib.Templates))
	}

	lib.Templates = []SessionTemplate{{
		ID:          "tpl_1",
		Name:        "Fejlesztés",
		Description: "Claude + terminál",
		Session: PortableSession{
			Name:      "dev",
			Path:      "/tmp/project",
			Agent:     AgentClaude,
			AutoYes:   true,
			ExtraArgs: "--model opus",
			Tabs: []PortableTab{
				{Name: "shell", Agent: AgentTerminal},
				{Name: "tesztek", Agent: AgentAider, ExtraArgs: "--no-git"},
			},
		},
	}}
	if err := s.SaveTemplates(lib); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := s.LoadTemplates()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(loaded.Templates) != 1 {
		t.Fatalf("templates lost: %+v", loaded.Templates)
	}
	got := loaded.Templates[0]
	if got.Name != "Fejlesztés" || got.Description != "Claude + terminál" {
		t.Errorf("name/description lost: %+v", got)
	}
	if got.Session.Agent != AgentClaude || !got.Session.AutoYes || got.Session.ExtraArgs != "--model opus" {
		t.Errorf("main window settings lost: %+v", got.Session)
	}
	if len(got.Session.Tabs) != 2 {
		t.Fatalf("tabs lost: %+v", got.Session.Tabs)
	}
	if got.Session.Tabs[0].Agent != AgentTerminal || got.Session.Tabs[1].ExtraArgs != "--no-git" {
		t.Errorf("tab settings lost: %+v", got.Session.Tabs)
	}

	// The atomic write must not leave temporary files lying around.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "templates.json" {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}
}

// A corrupt file must report a readable error rather than silently losing the
// library or crashing.
func TestLoadTemplatesRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "templates.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Storage{configDir: dir}).LoadTemplates(); err == nil {
		t.Error("a corrupt template file was accepted")
	}
}

func TestValidateRejectsUnnamedTemplate(t *testing.T) {
	if err := (&SessionTemplate{Name: "  "}).Validate(); err == nil {
		t.Error("a template without a name was accepted")
	}
	// No directory is a legitimate state, not a validation failure.
	if err := (&SessionTemplate{Name: "reusable"}).Validate(); err != nil {
		t.Errorf("a directory-less template was rejected: %v", err)
	}
}

// The whole point of the feature: instantiating a template must produce the
// main window AND every tab, each keeping its own agent and arguments.
func TestInstantiateTemplateCreatesTabs(t *testing.T) {
	dir := t.TempDir()
	tpl := SessionTemplate{
		Name: "két fül",
		Session: PortableSession{
			Name:      "dev",
			Path:      dir,
			Agent:     AgentClaude,
			AutoYes:   true,
			ExtraArgs: "--verbose",
			Tabs: []PortableTab{
				{Name: "claude-2", Agent: AgentClaude, ExtraArgs: "--model haiku"},
				{Name: "shell", Agent: AgentTerminal},
			},
		},
	}

	inst, err := tpl.InstantiateTemplate("", "")
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if inst.Name != "dev" || inst.Path != dir {
		t.Errorf("main window wrong: name=%q path=%q", inst.Name, inst.Path)
	}
	if inst.Agent != AgentClaude || !inst.AutoYes || inst.ExtraArgs != "--verbose" {
		t.Errorf("main window settings lost: %+v", inst)
	}
	if inst.Status != StatusStopped {
		t.Errorf("a fresh session should start stopped, got %v", inst.Status)
	}
	if inst.ID == "" {
		t.Error("no ID generated")
	}

	if len(inst.FollowedWindows) != 2 {
		t.Fatalf("got %d tabs, want 2: %+v", len(inst.FollowedWindows), inst.FollowedWindows)
	}
	// restoreFollowedWindows() spawns one tmux window per entry and builds the
	// agent command from these fields, so they are what makes the tabs real.
	first, second := inst.FollowedWindows[0], inst.FollowedWindows[1]
	if first.Name != "claude-2" || first.Agent != AgentClaude || first.ExtraArgs != "--model haiku" {
		t.Errorf("first tab wrong: %+v", first)
	}
	if second.Name != "shell" || second.Agent != AgentTerminal {
		t.Errorf("second tab wrong: %+v", second)
	}
	// Indices must be fresh and distinct; a duplicate would make two tabs
	// point at one tmux window.
	if first.Index == second.Index {
		t.Errorf("tabs share index %d", first.Index)
	}
	// Nothing may carry over that would make a new session resume someone
	// else's conversation.
	for _, fw := range inst.FollowedWindows {
		if fw.ResumeSessionID != "" {
			t.Errorf("tab %q carries a resume ID: %q", fw.Name, fw.ResumeSessionID)
		}
		if fw.Stopped {
			t.Errorf("tab %q would be created as a stopped placeholder", fw.Name)
		}
	}
	if inst.ResumeSessionID != "" {
		t.Errorf("session carries a resume ID: %q", inst.ResumeSessionID)
	}
}

// A template with no directory is the reusable kind: it must refuse to create
// anything until a path is supplied, then use exactly that path.
func TestInstantiateTemplateWithoutPath(t *testing.T) {
	tpl := SessionTemplate{
		Name:    "hordozható",
		Session: PortableSession{Name: "dev", Agent: AgentClaude, Tabs: []PortableTab{{Name: "shell", Agent: AgentTerminal}}},
	}
	if !tpl.TemplateNeedsPath() {
		t.Error("a template without a directory should report NeedsPath")
	}
	if _, err := tpl.InstantiateTemplate("", ""); err == nil {
		t.Error("a directory-less template was instantiated without a path")
	}

	dir := t.TempDir()
	inst, err := tpl.InstantiateTemplate("saját név", dir)
	if err != nil {
		t.Fatalf("instantiate with path: %v", err)
	}
	if inst.Path != dir {
		t.Errorf("path = %q, want %q", inst.Path, dir)
	}
	if inst.Name != "saját név" {
		t.Errorf("name = %q, want the supplied one", inst.Name)
	}
	// The tab had no directory of its own, so it must follow the session into
	// the newly chosen one rather than being pinned somewhere else.
	if len(inst.FollowedWindows) != 1 || inst.FollowedWindows[0].WorkDir != "" {
		t.Errorf("tab did not follow the session directory: %+v", inst.FollowedWindows)
	}
}

// A tab deliberately pinned elsewhere keeps its own directory even when the
// session is created somewhere new.
func TestInstantiateTemplateKeepsPinnedTabDir(t *testing.T) {
	sessionDir := t.TempDir()
	tabDir := t.TempDir()
	tpl := SessionTemplate{
		Name:    "vegyes",
		Session: PortableSession{Agent: AgentClaude, Tabs: []PortableTab{{Name: "logs", Agent: AgentTerminal, WorkDir: tabDir}}},
	}
	inst, err := tpl.InstantiateTemplate("dev", sessionDir)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if len(inst.FollowedWindows) != 1 || inst.FollowedWindows[0].WorkDir != tabDir {
		t.Errorf("pinned tab directory lost: %+v", inst.FollowedWindows)
	}
}

func TestInstantiateTemplateRejectsMissingDirectory(t *testing.T) {
	tpl := SessionTemplate{Name: "rossz", Session: PortableSession{Name: "dev", Path: filepath.Join(t.TempDir(), "nincs-ilyen")}}
	if _, err := tpl.InstantiateTemplate("", ""); err == nil {
		t.Error("a template pointing at a missing directory was instantiated")
	}
}

// Falling back to the folder name matches what the new-session dialog does, so
// a template with no suggested name still produces something recognisable.
func TestInstantiateTemplateNamesFromFolder(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "myproject")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	inst, err := (&SessionTemplate{Name: "t", Session: PortableSession{Agent: AgentClaude}}).InstantiateTemplate("", dir)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if inst.Name != "myproject" {
		t.Errorf("name = %q, want the folder name", inst.Name)
	}
}

// Saving a session as a template must capture its tabs — a template holding
// only the main window would save the user nothing.
func TestTemplateFromInstanceCapturesTabs(t *testing.T) {
	inst := &Instance{
		ID:              "asm_claude_dev_1",
		Name:            "dev",
		Path:            "/tmp/project",
		Agent:           AgentClaude,
		AutoYes:         true,
		ExtraArgs:       "--verbose",
		Status:          StatusRunning,
		Favorite:        true,
		ResumeSessionID: "abc-123",
		FollowedWindows: []FollowedWindow{
			{Index: 3, Name: "shell", Agent: AgentTerminal},
			{Index: 7, Name: "aider", Agent: AgentAider, ExtraArgs: "--no-git", ResumeSessionID: "def-456"},
		},
	}

	tpl := TemplateFromInstance(inst, "Fejlesztői elrendezés", true)
	if tpl.Name != "Fejlesztői elrendezés" {
		t.Errorf("name = %q", tpl.Name)
	}
	if tpl.Session.Path != "/tmp/project" {
		t.Errorf("keepPath did not keep the directory: %q", tpl.Session.Path)
	}
	if len(tpl.Session.Tabs) != 2 || tpl.Session.Tabs[1].ExtraArgs != "--no-git" {
		t.Fatalf("tabs not captured: %+v", tpl.Session.Tabs)
	}
	// Runtime state must not travel: PortableTab has no resume field at all,
	// and Favorite is a per-session mark rather than part of an arrangement.
	if tpl.Session.Favorite {
		t.Error("the favourite mark was copied into the template")
	}

	// Dropping the path is what makes the arrangement reusable elsewhere.
	reusable := TemplateFromInstance(inst, "", false)
	if !reusable.TemplateNeedsPath() {
		t.Errorf("keepPath=false left a directory: %q", reusable.Session.Path)
	}
	if reusable.Name != "dev" {
		t.Errorf("empty template name should fall back to the session name, got %q", reusable.Name)
	}
	if len(reusable.Session.Tabs) != 2 {
		t.Errorf("tabs lost when dropping the path: %+v", reusable.Session.Tabs)
	}
}

func TestSortTemplatesPutsUsedFirst(t *testing.T) {
	now := time.Now()
	templates := []SessionTemplate{
		{Name: "zebra"},
		{Name: "alma"},
		{Name: "gyakori", UseCount: 5, UsedAt: now.Add(-time.Hour)},
		{Name: "friss", UseCount: 1, UsedAt: now},
		{Name: "régi", UseCount: 1, UsedAt: now.Add(-24 * time.Hour)},
	}
	SortTemplates(templates)
	want := []string{"gyakori", "friss", "régi", "alma", "zebra"}
	for i, name := range want {
		if templates[i].Name != name {
			t.Fatalf("order = %v, want %v", templateNames(templates), want)
		}
	}
}

func templateNames(templates []SessionTemplate) []string {
	out := make([]string, len(templates))
	for i, t := range templates {
		out[i] = t.Name
	}
	return out
}

// A template already used to create sessions can still be deleted; the
// sessions it produced are independent copies, not live references.
func TestDeletingTemplateLeavesItsSessionsAlone(t *testing.T) {
	s := newTestStorage(t)
	dir := t.TempDir()

	lib := &TemplateLibrary{Templates: []SessionTemplate{{
		ID:      "tpl_1",
		Name:    "használatban",
		Session: PortableSession{Name: "dev", Path: dir, Agent: AgentClaude, Tabs: []PortableTab{{Name: "shell", Agent: AgentTerminal}}},
	}}}
	if err := s.SaveTemplates(lib); err != nil {
		t.Fatalf("save templates: %v", err)
	}

	inst, err := lib.Templates[0].InstantiateTemplate("", "")
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if err := s.AddInstance(inst); err != nil {
		t.Fatalf("add instance: %v", err)
	}

	lib.Templates = nil
	if err := s.SaveTemplates(lib); err != nil {
		t.Fatalf("delete template: %v", err)
	}

	kept, err := s.GetInstance(inst.ID)
	if err != nil {
		t.Fatalf("the session created from the template disappeared: %v", err)
	}
	if len(kept.FollowedWindows) != 1 || kept.FollowedWindows[0].Name != "shell" {
		t.Errorf("the session lost its tabs: %+v", kept.FollowedWindows)
	}
}
