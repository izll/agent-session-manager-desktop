package session

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func twoPages() []NotePage {
	return []NotePage{
		{ID: "a", Title: "Plan", Text: "ship on friday"},
		{ID: "b", Title: "Risks", Text: "the migration"},
	}
}

// A note written before pages existed is text only. It reads as one untitled
// page with a fixed ID, the same each time, holding exactly that text.
func TestLegacyNoteReadsAsOnePage(t *testing.T) {
	inst := &Instance{Notes: "old note\nsecond line"}
	want := []NotePage{{ID: LegacyNotePageID, Text: "old note\nsecond line"}}
	for range 2 {
		if got := inst.SessionNote().Pages(); !slices.Equal(got, want) {
			t.Fatalf("legacy note read as %+v, want %+v", got, want)
		}
	}
	if got := inst.MainTabNote().Pages(); got != nil {
		t.Fatalf("an empty note has pages: %+v", got)
	}
}

// One untitled page is stored as text alone, exactly as before pages: an old
// version reads it unchanged and the file does not grow a pages field.
func TestOneUntitledPageIsStoredAsTextOnly(t *testing.T) {
	inst := &Instance{}
	inst.SessionNote().SetPages([]NotePage{{ID: "x", Text: "just text"}})
	if inst.Notes != "just text" || inst.NotePages != nil {
		t.Fatalf("stored as notes=%q pages=%+v", inst.Notes, inst.NotePages)
	}
	raw, err := json.Marshal(inst)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "note_pages") {
		t.Fatalf("a single untitled page wrote a pages field: %s", raw)
	}
	inst.SessionNote().SetPages(nil)
	if inst.Notes != "" || inst.NotePages != nil {
		t.Fatalf("an emptied note left notes=%q pages=%+v", inst.Notes, inst.NotePages)
	}
}

// Pages survive the trip through JSON in every note: session, main tab, tab.
func TestNotePagesRoundTripInEveryNote(t *testing.T) {
	inst := &Instance{FollowedWindows: []FollowedWindow{{ID: "t1", Index: 1}}}
	inst.SessionNote().SetPages(twoPages())
	inst.MainTabNote().SetPages(twoPages())
	inst.FollowedWindows[0].Note().SetPages(twoPages())

	raw, err := json.Marshal(inst)
	if err != nil {
		t.Fatal(err)
	}
	var back Instance
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]NotePage{
		"session":  back.SessionNote().Pages(),
		"main tab": back.MainTabNote().Pages(),
		"tab":      back.FollowedWindows[0].Note().Pages(),
	} {
		if !slices.Equal(got, twoPages()) {
			t.Errorf("%s note came back as %+v", name, got)
		}
	}
}

// The text field holds every page with something in it, under its title, so
// an older version shows all of it; pages with no text are left out, and a
// note with nothing in any page is empty.
func TestCompatTextHoldsEveryPage(t *testing.T) {
	pages := append(twoPages(),
		NotePage{ID: "c", Title: "Empty", Text: "  \n"},
		NotePage{ID: "d", Text: "untitled words"},
	)
	inst := &Instance{}
	inst.SessionNote().SetPages(pages)
	want := "# Plan\nship on friday\n\n# Risks\nthe migration\n\nuntitled words"
	if inst.Notes != want {
		t.Fatalf("compat text = %q, want %q", inst.Notes, want)
	}
	inst.SessionNote().SetPages([]NotePage{{ID: "a", Title: "Only a title"}, {ID: "b"}})
	if inst.Notes != "" {
		t.Fatalf("pages without text gave compat text %q", inst.Notes)
	}
	if got := inst.SessionNote().Pages(); len(got) != 2 || got[0].Title != "Only a title" {
		t.Fatalf("title-only pages were not kept: %+v", got)
	}
}

// legacyInstance is the shape an older version decodes sessions.json into:
// it has the text fields and no pages.
type legacyInstance struct {
	ID              string `json:"id"`
	Notes           string `json:"notes,omitempty"`
	MainTabNotes    string `json:"main_tab_notes,omitempty"`
	FollowedWindows []struct {
		ID    string `json:"id,omitempty"`
		Index int    `json:"index"`
		Notes string `json:"notes,omitempty"`
	} `json:"followed_windows,omitempty"`
}

// Downgrade: an older version reads the joined text, and saving drops the
// pages field it does not know. Opened in this version again, every page's
// words are there, as one page. Nothing typed is lost.
func TestOlderVersionRoundTripKeepsEveryPagesText(t *testing.T) {
	inst := &Instance{ID: "s", FollowedWindows: []FollowedWindow{{ID: "t1", Index: 1}}}
	inst.SessionNote().SetPages(twoPages())
	inst.FollowedWindows[0].Note().SetPages(twoPages())
	raw, err := json.Marshal(inst)
	if err != nil {
		t.Fatal(err)
	}

	var old legacyInstance
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{old.Notes, old.FollowedWindows[0].Notes} {
		for _, page := range twoPages() {
			if !strings.Contains(text, page.Text) || !strings.Contains(text, page.Title) {
				t.Fatalf("an older version sees %q, missing page %+v", text, page)
			}
		}
	}
	rewritten, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}

	var back Instance
	if err := json.Unmarshal(rewritten, &back); err != nil {
		t.Fatal(err)
	}
	got := back.SessionNote().Pages()
	if len(got) != 1 || got[0].Text != old.Notes {
		t.Fatalf("after a downgrade round trip the note is %+v", got)
	}
}

// If the text no longer matches the pages, something that does not know pages
// wrote the note since, and the text is the newer copy.
func TestEditedCompatTextWinsOverStalePages(t *testing.T) {
	inst := &Instance{}
	inst.SessionNote().SetPages(twoPages())
	inst.Notes += "\nadded by an older version"
	got := inst.SessionNote().Pages()
	if len(got) != 1 || got[0].Text != inst.Notes || got[0].ID != LegacyNotePageID {
		t.Fatalf("stale pages were preferred to newer text: %+v", got)
	}
}

func TestNormalizeNotePages(t *testing.T) {
	got := NormalizeNotePages([]NotePage{
		{ID: "a", Title: "  two\nlines\tand  tabs ", Text: " kept as typed \n"},
		{ID: "a", Title: "duplicate id"},
		{ID: "", Title: "no id"},
		{ID: "bad\x00id", Title: "control"},
		{ID: strings.Repeat("x", 100), Title: strings.Repeat("é", 300)},
	})
	if got[0].Title != "two lines and tabs" || got[0].Text != " kept as typed \n" {
		t.Fatalf("first page = %+v", got[0])
	}
	seen := map[string]bool{}
	for _, p := range got {
		if p.ID == "" || seen[p.ID] {
			t.Fatalf("page IDs not unique and non-empty: %+v", got)
		}
		seen[p.ID] = true
	}
	if len([]rune(got[4].Title)) != maxNotePageTitle || len(got[4].ID) > maxNotePageIDRunes {
		t.Fatalf("over-long page = %+v", got[4])
	}
	many := make([]NotePage, maxNotePages+5)
	if got := NormalizeNotePages(many); len(got) != maxNotePages {
		t.Fatalf("%d pages kept, want %d", len(got), maxNotePages)
	}
}

// On disk, through the storage: pages and the compat text side by side, read
// back as pages.
func TestStorageKeepsNotePages(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	inst := &Instance{ID: "s1", Name: "API", Path: "/tmp/api", Status: StatusStopped,
		CreatedAt: time.Now(), UpdatedAt: time.Now()}
	inst.SessionNote().SetPages(twoPages())
	if err := storage.SaveAll([]*Instance{inst}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(storage.configPath)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk struct {
		Instances []legacyInstance `json:"instances"`
	}
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(onDisk.Instances[0].Notes, "ship on friday") || !strings.Contains(string(raw), `"note_pages"`) {
		t.Fatalf("sessions.json lacks the compat text or the pages:\n%s", raw)
	}
	loaded, err := storage.GetInstance("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.SessionNote().Pages(); !slices.Equal(got, twoPages()) {
		t.Fatalf("loaded pages = %+v", got)
	}
}

// The project's note has pages too, and an unchanged write is not a write.
func TestProjectNotePages(t *testing.T) {
	s := newTestStorage(t)
	if err := s.SetProjectNotePages(twoPages()); err != nil {
		t.Fatal(err)
	}
	got, err := s.ProjectNotePages()
	if err != nil || !slices.Equal(got, twoPages()) {
		t.Fatalf("project pages = %+v, %v", got, err)
	}
	data, err := s.loadStorageDataLocked()
	if err != nil {
		t.Fatal(err)
	}
	revision := data.Revision
	if err := s.SetProjectNotePages(twoPages()); err != nil {
		t.Fatal(err)
	}
	if data, _ := s.loadStorageDataLocked(); data.Revision != revision {
		t.Fatalf("an unchanged note was written again: revision %d -> %d", revision, data.Revision)
	}
}

// A backup holds the pages, and restoring it brings them back.
func TestBackupRestoresNotePages(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	inst := &Instance{ID: "s1", Name: "API", Path: "/tmp/api", Status: StatusStopped,
		CreatedAt: time.Now(), UpdatedAt: time.Now()}
	inst.SessionNote().SetPages(twoPages())
	if err := storage.SaveAll([]*Instance{inst}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.CreateBackup(); err != nil {
		t.Fatal(err)
	}
	backups, err := storage.ListBackups()
	if err != nil || len(backups) == 0 {
		t.Fatalf("backups = %+v, %v", backups, err)
	}
	inst.SessionNote().SetPages(nil)
	if err := storage.UpdateInstance(inst); err != nil {
		t.Fatal(err)
	}
	// Every backup listed so far was made while the note had its pages.
	if err := storage.RestoreBackup(backups[0].ID); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.GetInstance("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.SessionNote().Pages(); !slices.Equal(got, twoPages()) {
		t.Fatalf("restored pages = %+v", got)
	}
}

// A tab sent to the trash and restored keeps its note's pages.
func TestTrashedTabKeepsNotePages(t *testing.T) {
	storage := newRecoveryTestStorage(t)
	inst := &Instance{ID: "s1", Name: "API", Path: "/tmp/api", Status: StatusStopped,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		FollowedWindows: []FollowedWindow{{ID: "t7", Index: 7, Agent: AgentClaude, Name: "Review"}}}
	inst.FollowedWindows[0].Note().SetPages(twoPages())
	if err := storage.SaveAll([]*Instance{inst}, []*Group{}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := storage.TrashTab("s1", 7); err != nil {
		t.Fatal(err)
	}
	trash, err := storage.ListTrash()
	if err != nil || len(trash) != 1 {
		t.Fatalf("trash = %+v, %v", trash, err)
	}
	if _, err := storage.RestoreTrashItem(trash[0].ID); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.GetInstance("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.FollowedWindows[0].Note().Pages(); !slices.Equal(got, twoPages()) {
		t.Fatalf("restored tab pages = %+v", got)
	}
}

// Export and import carry every note's pages; a file exported before pages
// existed still imports its text.
func TestPortableNotePages(t *testing.T) {
	inst := &Instance{ID: "s1", Name: "API", Path: "/tmp/api",
		FollowedWindows: []FollowedWindow{{ID: "t1", Index: 1, Name: "Tests"}}}
	inst.SessionNote().SetPages(twoPages())
	inst.MainTabNote().SetPages(twoPages())
	inst.FollowedWindows[0].Note().SetPages(twoPages())

	bundle := ToPortable([]*Instance{inst}, nil, "test")
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var back PortableBundle
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	imported := back.Sessions[0].FromPortable("")
	for name, got := range map[string][]NotePage{
		"session":  imported.SessionNote().Pages(),
		"main tab": imported.MainTabNote().Pages(),
		"tab":      imported.FollowedWindows[0].Note().Pages(),
	} {
		if !slices.Equal(got, twoPages()) {
			t.Errorf("imported %s note = %+v", name, got)
		}
	}

	old := PortableSession{Name: "old", Path: "/tmp/old", Notes: "from an old export",
		Tabs: []PortableTab{{Name: "T", Notes: "old tab note"}}}
	imported = old.FromPortable("")
	if got := imported.SessionNote().Pages(); len(got) != 1 || got[0].Text != "from an old export" {
		t.Fatalf("old export's session note = %+v", got)
	}
	if got := imported.FollowedWindows[0].Note().Pages(); len(got) != 1 || got[0].Text != "old tab note" {
		t.Fatalf("old export's tab note = %+v", got)
	}
}

// Every page is searched, by its text and by its title, and a result says
// which page it is so opening it lands there.
func TestSearchNotesFindsEveryPage(t *testing.T) {
	inst := &Instance{ID: "s1", Name: "API"}
	inst.SessionNote().SetPages([]NotePage{
		{ID: "a", Title: "Plan", Text: "ship on friday"},
		{ID: "b", Title: "Risks", Text: "the database migration"},
		{ID: "c", Title: "Migration owners"},
		{ID: "d", Text: "untitled page about migration"},
	})
	src := NoteSources{Instances: []*Instance{inst}}

	matches := SearchNotes(src, "migration")
	if len(matches) != 3 {
		t.Fatalf("got %d matches, want 3: %+v", len(matches), matches)
	}
	byPage := map[string]NoteMatch{}
	for _, m := range matches {
		byPage[m.PageID] = m
	}
	risks := byPage["b"]
	if risks.PageTitle != "Risks" || risks.PageIndex != 1 || risks.PageCount != 4 ||
		!strings.Contains(risks.Snippet, "migration") || risks.Scope != NoteScopeSession {
		t.Fatalf("second page match = %+v", risks)
	}
	if owners, ok := byPage["c"]; !ok || owners.Snippet != "Migration owners" {
		t.Fatalf("a page found by its title alone = %+v (found %v)", owners, ok)
	}
	if untitled := byPage["d"]; untitled.PageTitle != "" || untitled.PageIndex != 3 {
		t.Fatalf("untitled page match = %+v", untitled)
	}
	for _, m := range matches {
		found, ok := FindNote(src, m.ID())
		if !ok || found.PageID != m.PageID || found.Text != m.Text {
			t.Fatalf("FindNote(%q) = %+v, %v", m.ID(), found, ok)
		}
	}
	if got := SearchNotes(src, "Plan"); len(got) != 1 || got[0].PageID != "a" {
		t.Fatalf("title search = %+v", got)
	}
	if got := FuzzySearchNotes(src, "rsks"); len(got) == 0 || got[0].PageID != "b" {
		t.Fatalf("fuzzy title search = %+v", got)
	}
}
