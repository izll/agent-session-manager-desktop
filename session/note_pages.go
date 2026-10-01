package session

import (
	"fmt"
	"strings"
	"unicode"
)

// A note — the project's, a session's, or a tab's — is a list of titled pages.
//
// Notes used to be a single text, and that text stays where it always was
// (the `notes` / `main_tab_notes` fields) so that an older version of the app
// opening the file still finds every note:
//
//   - A note that is one untitled page — every note written before pages
//     existed, and most notes since — is stored exactly as before, as text
//     only. Its file does not change at all.
//   - A note with several pages, or a titled one, keeps its pages in a
//     `note_pages` field beside the text, and the text holds all of the pages
//     with something in them, joined under their titles (NotePagesText).
//
// An older version knows nothing of `note_pages`: it shows the joined text,
// and the first time it saves the file the pages field is dropped (it decodes
// into a struct without the field). What remains is the joined text — every
// page's words, as one page. Opened again in this version, that is read back
// as a single untitled page. Titles of empty pages are all that a round trip
// through an old version can lose.
//
// The text is also what decides, on reading, whether the pages are still
// current: if it no longer matches the pages it was derived from, something
// that does not know about pages has written the note since, and the text is
// the newer copy (ReadNotePages).

// NotePage is one page of a note.
type NotePage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// LegacyNotePageID is the ID of the page a text-only note is read as. Fixed,
// so that a note read twice gives the same page both times and the notes view
// can remember which page was open.
const LegacyNotePageID = "page-1"

const (
	// Bounds against a corrupt or hostile file (imports go through here too),
	// far beyond anything typed by hand.
	maxNotePages          = 200
	maxNotePageTitle      = 200 // runes
	maxNotePageIDRunes    = 64
	notePageTitlePrefix   = "# "
	notePageJoinSeparator = "\n\n"
)

// NotePagesText is the single text a list of pages is stored and shown as
// wherever pages are not understood: by older versions of the app, by the
// session list's search, by the dot that says a note exists.
//
// A single untitled page is its text, unchanged. Otherwise every page with
// text in it, in order, under a "# Title" line when it has a title. Pages with
// no text are left out, so the result is empty exactly when no page has
// anything in it.
//
// The format is part of the stored data: ReadNotePages compares against it,
// and a change here would make every stored multi-page note look as if an old
// version had written it. Keep reading the old format if it ever changes.
func NotePagesText(pages []NotePage) string {
	if len(pages) == 1 && pages[0].Title == "" {
		return pages[0].Text
	}
	parts := make([]string, 0, len(pages))
	for _, p := range pages {
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		if p.Title == "" {
			parts = append(parts, p.Text)
			continue
		}
		parts = append(parts, notePageTitlePrefix+p.Title+"\n"+p.Text)
	}
	return strings.Join(parts, notePageJoinSeparator)
}

// ReadNotePages is the pages of a note stored as text and pages.
//
// Pages are used only while the text still matches them; otherwise the text
// has been written by something that does not know pages and is read as one
// page. An empty note has no pages: the caller shows it as one empty page.
func ReadNotePages(text string, stored []NotePage) []NotePage {
	if len(stored) > 0 && NotePagesText(stored) == text {
		return append([]NotePage(nil), stored...)
	}
	if text == "" {
		return nil
	}
	return []NotePage{{ID: LegacyNotePageID, Text: text}}
}

// WriteNotePages is how pages are stored: the text for the legacy field and
// the pages to keep beside it, which is nil when the text alone says it all —
// no pages, or one untitled page.
func WriteNotePages(pages []NotePage) (string, []NotePage) {
	pages = NormalizeNotePages(pages)
	switch {
	case len(pages) == 0:
		return "", nil
	case len(pages) == 1 && pages[0].Title == "":
		return pages[0].Text, nil
	default:
		return NotePagesText(pages), pages
	}
}

// NormalizeNotePages tidies pages as they come in from the webview or an
// imported file: titles on one line and of bounded length, every page with an
// ID of its own, and no more pages than any note could need. Text is left
// exactly as typed. Returns a new slice.
func NormalizeNotePages(pages []NotePage) []NotePage {
	if len(pages) > maxNotePages {
		pages = pages[:maxNotePages]
	}
	out := make([]NotePage, 0, len(pages))
	seen := make(map[string]bool, len(pages))
	for i, p := range pages {
		p.Title = normalizeNotePageTitle(p.Title)
		p.ID = strings.TrimSpace(p.ID)
		if len([]rune(p.ID)) > maxNotePageIDRunes || strings.ContainsFunc(p.ID, unicode.IsControl) {
			p.ID = ""
		}
		if p.ID == "" || seen[p.ID] {
			p.ID = freeNotePageID(seen, i+1)
		}
		seen[p.ID] = true
		out = append(out, p)
	}
	return out
}

func normalizeNotePageTitle(title string) string {
	// A title is a label in a strip of tabs and a "# " line in the joined
	// text: a line break would end it early, so runs of whitespace of any
	// kind become a single space.
	title = strings.Join(strings.Fields(title), " ")
	if runes := []rune(title); len(runes) > maxNotePageTitle {
		title = strings.TrimSpace(string(runes[:maxNotePageTitle]))
	}
	return title
}

func freeNotePageID(seen map[string]bool, n int) string {
	for ; ; n++ {
		id := fmt.Sprintf("page-%d", n)
		if !seen[id] {
			return id
		}
	}
}

// NoteSlot is where one note is kept: its text and its pages, two fields
// that are only ever read and written together through it.
type NoteSlot struct {
	text  *string
	pages *[]NotePage
}

// Pages returns the note's pages; see ReadNotePages.
func (n NoteSlot) Pages() []NotePage {
	return ReadNotePages(*n.text, *n.pages)
}

// SetPages replaces the whole note; see WriteNotePages.
func (n NoteSlot) SetPages(pages []NotePage) {
	*n.text, *n.pages = WriteNotePages(pages)
}

// SessionNote is the session's own note, shared by all of its tabs.
func (i *Instance) SessionNote() NoteSlot {
	return NoteSlot{text: &i.Notes, pages: &i.NotePages}
}

// MainTabNote is the note of the session's own tab.
func (i *Instance) MainTabNote() NoteSlot {
	return NoteSlot{text: &i.MainTabNotes, pages: &i.MainTabNotePages}
}

// Note is the tab's own note.
func (w *FollowedWindow) Note() NoteSlot {
	return NoteSlot{text: &w.Notes, pages: &w.NotePages}
}

// ProjectNote is the project's own note.
func (d *StorageData) ProjectNote() NoteSlot {
	return NoteSlot{text: &d.Notes, pages: &d.NotePages}
}
