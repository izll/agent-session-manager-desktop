// Package whatsnew reads the release notes shipped inside the binary and
// decides, at launch, which of them the user has not seen yet.
package whatsnew

import (
	"regexp"
	"strings"
)

// Entry is one release in the changelog.
type Entry struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	// Intro holds the paragraphs between the version heading and its first
	// section ("First public release.", the 1.0 note).
	Intro    []string  `json:"intro"`
	Sections []Section `json:"sections"`
}

// Section is one "### Added / Changed / Fixed" block of an entry.
type Section struct {
	// Kind is the heading lower-cased ("added", "fixed"), for the UI to
	// translate; Title is the heading as written, for a kind it does not know.
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Intro holds paragraphs written under the heading outside the list.
	Intro []string `json:"intro"`
	// Items are the bullets, each joined onto one line. The text keeps its
	// inline markdown (**bold**, `code`, [links](url)) for the UI to render.
	Items []string `json:"items"`
}

// versionHeading matches "## 1.1.18 — 2026-09-29" and the Keep a Changelog
// spellings "## [1.1.18] - 2026-09-29" and "## v1.1.18 – 2026-09-29".
var versionHeading = regexp.MustCompile(`^##\s+\[?v?(\d+\.\d+\.\d+[0-9A-Za-z.+-]*?)\]?(?:\s+[—–-]+\s*(\S+))?\s*$`)

// linkDefinition is a markdown reference ("[1.1.18]: https://…") — metadata,
// not something to show.
var linkDefinition = regexp.MustCompile(`^\[[^\]]+\]:\s*\S+`)

// bullet matches a list item written at the left margin. A deeper indent is
// the wrapped continuation of the item above, never a new one: the changelog
// wraps at 80 columns, and a wrapped line may happen to start with "- ".
var bullet = regexp.MustCompile(`^ ?[-*+]\s+`)

// parser collects one entry at a time; a block is the bullet or paragraph
// being read, one source line per element.
type parser struct {
	entries []Entry
	entry   *Entry
	section *Section
	block   []string
	isItem  bool
	// blank records a blank line inside the current list item, after which
	// only an indented line still belongs to the item.
	blank bool
	// skipping is set under a heading that is not a version.
	skipping bool
}

// Parse reads a Keep a Changelog file. It never fails: text it does not
// recognise is skipped rather than rejected, so a changelog edited by hand
// cannot take the dialog down. Everything above the first version heading
// (the title and the file's own introduction) is left out, and so is a
// heading that is not a version, such as "## [Unreleased]", with its content.
// CRLF line endings are accepted: a Windows checkout may have them.
func Parse(data []byte) []Entry {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	p := &parser{}
	for _, line := range strings.Split(text, "\n") {
		p.line(line)
	}
	p.closeEntry()
	return p.entries
}

func (p *parser) line(line string) {
	trimmed := strings.TrimSpace(line)
	switch {
	case trimmed == "##" || strings.HasPrefix(trimmed, "## "):
		p.closeEntry()
		m := versionHeading.FindStringSubmatch(trimmed)
		p.skipping = m == nil
		if m != nil {
			p.entry = &Entry{Version: m[1], Date: m[2]}
		}
		return
	case trimmed == "#" || strings.HasPrefix(trimmed, "# "):
		// The document title: ends whatever was being read.
		p.closeEntry()
		p.skipping = true
		return
	}
	if p.skipping || p.entry == nil {
		return
	}

	switch {
	case strings.HasPrefix(trimmed, "#"):
		p.endBlock()
		title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		p.entry.Sections = append(p.entry.Sections, Section{Kind: strings.ToLower(title), Title: title})
		p.section = &p.entry.Sections[len(p.entry.Sections)-1]
	case trimmed == "":
		// A blank line ends a paragraph, but not yet a list item: an indented
		// line after it is the item's second paragraph.
		if p.isItem {
			p.blank = true
		} else {
			p.endBlock()
		}
	case bullet.MatchString(line):
		p.endBlock()
		p.block = []string{strings.TrimSpace(bullet.ReplaceAllString(line, ""))}
		p.isItem = true
	case linkDefinition.MatchString(trimmed):
		p.endBlock()
	case p.isItem && p.blank && !strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "\t"):
		// Unindented text after a blank line leaves the list.
		p.endBlock()
		p.block = []string{trimmed}
	default:
		p.block = append(p.block, trimmed)
		p.blank = false
	}
}

func (p *parser) endBlock() {
	if len(p.block) > 0 && p.entry != nil {
		text := strings.Join(p.block, " ")
		switch {
		case p.isItem && p.section == nil:
			// A list with no section heading above it gets an unnamed one.
			p.entry.Sections = append(p.entry.Sections, Section{})
			p.section = &p.entry.Sections[len(p.entry.Sections)-1]
			p.section.Items = append(p.section.Items, text)
		case p.isItem:
			p.section.Items = append(p.section.Items, text)
		case p.section != nil:
			p.section.Intro = append(p.section.Intro, text)
		default:
			p.entry.Intro = append(p.entry.Intro, text)
		}
	}
	p.block, p.isItem, p.blank = nil, false, false
}

func (p *parser) closeEntry() {
	p.endBlock()
	if p.entry != nil {
		// Empty lists rather than nil ones, so the UI receives [] and not null.
		e := *p.entry
		e.Intro = nonNil(e.Intro)
		if e.Sections == nil {
			e.Sections = []Section{}
		}
		for i := range e.Sections {
			e.Sections[i].Intro = nonNil(e.Sections[i].Intro)
			e.Sections[i].Items = nonNil(e.Sections[i].Items)
		}
		p.entries = append(p.entries, e)
	}
	p.entry, p.section = nil, nil
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
