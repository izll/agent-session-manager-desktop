package session

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/sahilm/fuzzy"
)

// SessionNotesWindow is the window index that addresses the session's own
// note rather than a tab's. Negative, so no multiplexer window can have it.
const SessionNotesWindow = -1

// NoteScope says which of a session's notes a search hit came from, matching
// the [This tab | Session] switch of the notes view.
type NoteScope string

const (
	NoteScopeSession NoteScope = "session"
	NoteScopeTab     NoteScope = "tab"
)

const (
	noteSearchResultLimit = 200
	// How much of a note the fuzzy fallback looks at, as for histories: fuzzy
	// scoring over a long text matches almost anything.
	noteFuzzyPrefix = 500
	// Context kept around a match in a snippet, in characters.
	noteSnippetBefore = 30
	noteSnippetAfter  = 70
)

// NoteMatch is one page of a note that matched a global search query.
type NoteMatch struct {
	SessionID   string
	SessionName string
	Scope       NoteScope
	// TabID names the tab for a tab note (MainTabID for the main tab), and is
	// empty for the session note. It is what the frontend opens the tab by:
	// window indices move when tabs are reordered or tmux renumbers them.
	TabID   string
	TabName string
	// WindowIndex is SessionNotesWindow for the session note and the stored
	// index of a followed tab. The main tab's index is only known by asking
	// tmux, which a search should not do for every session; it is left at
	// SessionNotesWindow and the main tab is found by TabID instead.
	WindowIndex int
	// PageID, PageTitle and PageIndex say which page of the note this is;
	// PageCount is how many the note has, so an untitled page can be named
	// the way the notes view names it ("Note 2").
	PageID    string
	PageTitle string
	PageIndex int
	PageCount int
	Text      string
	Snippet   string
}

// ID is the opaque identifier a note result is issued under, so a preview can
// be asked for without the webview naming a session and tab directly.
func (m NoteMatch) ID() string {
	target := m.TabID
	if m.Scope == NoteScopeSession {
		target = string(NoteScopeSession)
	}
	return NoteResultID(m.SessionID, target, m.PageID)
}

// NoteResultID builds the ID of a note result; see NoteMatch.ID.
func NoteResultID(sessionID, target, pageID string) string {
	return fmt.Sprintf("note:%s:%s:%s", sessionID, target, pageID)
}

// IsNoteResultID tells note results apart from history entries, whose IDs are
// generated hex strings and never contain a colon.
func IsNoteResultID(id string) bool {
	return strings.HasPrefix(id, "note:")
}

// allNotes lists every page with something in it — text or a title — of the
// given sessions' notes: for each session its own note first, then the main
// tab's, then the followed tabs' in order, each note's pages in page order.
func allNotes(instances []*Instance) []NoteMatch {
	var notes []NoteMatch
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		base := NoteMatch{SessionID: inst.ID, SessionName: inst.Name}

		note := base
		note.Scope = NoteScopeSession
		note.WindowIndex = SessionNotesWindow
		notes = appendNotePages(notes, note, inst.SessionNote().Pages())

		note = base
		note.Scope = NoteScopeTab
		note.TabID = MainTabID
		// The tab bar labels the main tab with the session's name.
		note.TabName = inst.Name
		note.WindowIndex = SessionNotesWindow
		notes = appendNotePages(notes, note, inst.MainTabNote().Pages())

		for i := range inst.FollowedWindows {
			fw := &inst.FollowedWindows[i]
			note := base
			note.Scope = NoteScopeTab
			note.TabID = fw.ID
			note.TabName = fw.Name
			if note.TabName == "" {
				// Same fallback as the tab bar, so the result names the tab
				// the way the user sees it.
				note.TabName = fmt.Sprintf("Tab %d", fw.Index)
			}
			note.WindowIndex = fw.Index
			notes = appendNotePages(notes, note, fw.Note().Pages())
		}
	}
	return notes
}

// appendNotePages adds one match per page of a note that has anything in it.
func appendNotePages(notes []NoteMatch, note NoteMatch, pages []NotePage) []NoteMatch {
	for i, page := range pages {
		if strings.TrimSpace(page.Text) == "" && page.Title == "" {
			continue
		}
		match := note
		match.PageID = page.ID
		match.PageTitle = page.Title
		match.PageIndex = i
		match.PageCount = len(pages)
		match.Text = page.Text
		notes = append(notes, match)
	}
	return notes
}

// LoadStoredInstances reads the active project's sessions as stored, without
// the tmux status probe LoadAll makes. Notes live in sessions.json, not in
// tmux, so searching them must not wait on a multiplexer — least of all a
// remote one — for a status nothing here reads.
func (s *Storage) LoadStoredInstances() ([]*Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	instances, _, _, err := s.loadAllWithSettingsLocked()
	return instances, err
}

// FindNote returns the note a result ID was issued for, read from the given
// sessions, so a preview shows the note as it is now rather than as it was
// when the search ran.
func FindNote(instances []*Instance, id string) (NoteMatch, bool) {
	for _, note := range allNotes(instances) {
		if note.ID() == id {
			return note, true
		}
	}
	return NoteMatch{}, false
}

// SearchNotes finds the notes containing query, ignoring case — the same
// match the history search makes first. Notes are read from the instances
// passed in, which the caller loads at query time: notes are small and change
// all the time, so indexing them would only add a way to be stale.
func SearchNotes(instances []*Instance, query string) []NoteMatch {
	needle := []rune(strings.ToLower(query))
	if len(needle) == 0 {
		return nil
	}
	var results []NoteMatch
	for _, note := range allNotes(instances) {
		// The text first, so the snippet shows the words around the match;
		// a page found by its title alone shows the start of its text.
		if at := indexFold([]rune(note.Text), needle); at >= 0 {
			note.Snippet = noteSnippet(note.Text, at, len(needle))
		} else if indexFold([]rune(note.PageTitle), needle) >= 0 {
			note.Snippet = startSnippet(note)
		} else {
			continue
		}
		results = append(results, note)
		if len(results) >= noteSearchResultLimit {
			break
		}
	}
	return results
}

// FuzzySearchNotes is the typo-tolerant fallback, used only when nothing —
// neither a history nor a note — contains the query itself; the history search
// falls back the same way.
func FuzzySearchNotes(instances []*Instance, query string) []NoteMatch {
	if query == "" {
		return nil
	}
	notes := allNotes(instances)
	if len(notes) == 0 {
		return nil
	}
	var results []NoteMatch
	for _, match := range fuzzy.FindFrom(query, noteFuzzySource(notes)) {
		note := notes[match.Index]
		note.Snippet = startSnippet(note)
		results = append(results, note)
		if len(results) >= noteSearchResultLimit {
			break
		}
	}
	return results
}

type noteFuzzySource []NoteMatch

func (s noteFuzzySource) String(i int) string {
	// The title leads, as it does on screen: a page is named by it.
	text := []rune(strings.TrimSpace(s[i].PageTitle + " " + s[i].Text))
	if len(text) > noteFuzzyPrefix {
		text = text[:noteFuzzyPrefix]
	}
	return string(text)
}

func (s noteFuzzySource) Len() int { return len(s) }

// indexFold finds needle (already lower-cased) in text, ignoring case, and
// returns the rune position of the match or -1.
//
// Compared rune by rune rather than by lower-casing the whole text and using
// a byte offset: lower-casing can change a character's byte length (İ is two
// bytes, i one), so a byte offset into the lowered text can point into the
// middle of a character of the original — and a snippet cut there is invalid
// UTF-8.
func indexFold(text, needle []rune) int {
	for start := 0; start+len(needle) <= len(text); start++ {
		matched := true
		for i, r := range needle {
			if unicode.ToLower(text[start+i]) != r {
				matched = false
				break
			}
		}
		if matched {
			return start
		}
	}
	return -1
}

// startSnippet is the opening of a page's text, or its title when the page
// has no text: a result is never an empty line.
func startSnippet(note NoteMatch) string {
	if snippet := noteSnippet(note.Text, -1, 0); snippet != "" {
		return snippet
	}
	return note.PageTitle
}

// noteSnippet cuts a single-line excerpt around the match at rune position at
// (of length runes), or from the start when at is negative.
func noteSnippet(text string, at, length int) string {
	runes := []rune(text)
	start, end := 0, len(runes)
	if at >= 0 {
		start = max(0, at-noteSnippetBefore)
		end = min(len(runes), at+length+noteSnippetAfter)
	} else {
		end = min(len(runes), noteSnippetBefore+noteSnippetAfter)
	}
	// A note is written in lines; the result list shows one, so line breaks
	// and runs of spaces are folded the way history snippets are.
	snippet := strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runes) {
		snippet += "..."
	}
	return snippet
}
