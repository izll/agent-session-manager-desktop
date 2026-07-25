package main

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Importing terminal colour schemes from other emulators. The formats differ
// but all carry the same 16 ANSI colours plus background/foreground/cursor,
// so each parser just maps its own naming onto our palette keys.
//
// Supported:
//   - Konsole      .colorscheme   INI, "Color=r,g,b"
//   - iTerm2       .itermcolors   Apple plist, 0..1 float components
//   - Kitty/Alacritty/WezTerm/Ghostty and friends: any text file with
//     recognisable "key <sep> #rrggbb" lines (one loose parser)

// ImportedScheme is one parsed palette ready to be added to the user's list.
type ImportedScheme struct {
	Name   string            `json:"name"`
	Source string            `json:"source"` // "konsole", "iterm2", "hex", …
	Colors map[string]string `json:"colors"`
}

// ansiOrder maps ANSI index → our palette key, normal then intense.
var ansiOrder = []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
var ansiBright = []string{"brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue", "brightMagenta", "brightCyan", "brightWhite"}

func rgbToHex(r, g, b int) string {
	clamp := func(v int) int {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return v
	}
	return fmt.Sprintf("#%02x%02x%02x", clamp(r), clamp(g), clamp(b))
}

// ---------------------------------------------------------------- Konsole

var konsoleSectionRe = regexp.MustCompile(`^\[(.+)\]$`)

// parseKonsole reads a KDE Konsole .colorscheme (INI with "Color=r,g,b").
func parseKonsole(r io.Reader, fallbackName string) (*ImportedScheme, error) {
	out := &ImportedScheme{Name: fallbackName, Source: "konsole", Colors: map[string]string{}}
	section := ""
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := konsoleSectionRe.FindStringSubmatch(line); m != nil {
			section = m[1]
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)

		if section == "General" && strings.EqualFold(key, "Description") && value != "" {
			out.Name = value
			continue
		}
		if !strings.EqualFold(key, "Color") {
			continue
		}
		parts := strings.Split(value, ",")
		if len(parts) < 3 {
			continue
		}
		rr, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		gg, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		bb, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		hex := rgbToHex(rr, gg, bb)

		switch {
		case section == "Background":
			out.Colors["background"] = hex
		case section == "Foreground":
			out.Colors["foreground"] = hex
		case section == "Color" || section == "Cursor":
			// Konsole's [Color] doesn't exist; [Cursor] is optional.
			if section == "Cursor" {
				out.Colors["cursor"] = hex
			}
		default:
			// ColorN / ColorNIntense
			if strings.HasPrefix(section, "Color") {
				rest := strings.TrimPrefix(section, "Color")
				intense := strings.HasSuffix(rest, "Intense")
				rest = strings.TrimSuffix(rest, "Intense")
				// Faint variants exist too; we only take normal + intense.
				if strings.HasSuffix(rest, "Faint") {
					continue
				}
				idx, err := strconv.Atoi(rest)
				if err != nil || idx < 0 || idx > 7 {
					continue
				}
				if intense {
					out.Colors[ansiBright[idx]] = hex
				} else {
					out.Colors[ansiOrder[idx]] = hex
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out.Colors) == 0 {
		return nil, fmt.Errorf("no colours found")
	}
	return out, nil
}

// ---------------------------------------------------------------- iTerm2

// itermPlist is the minimal shape of an .itermcolors file: a dict of colour
// names, each holding a dict of 0..1 float components.
type itermPlist struct {
	Dict struct {
		Keys  []string `xml:"key"`
		Dicts []struct {
			Keys  []string  `xml:"key"`
			Reals []float64 `xml:"real"`
		} `xml:"dict"`
	} `xml:"dict"`
}

// parseITerm reads an iTerm2 .itermcolors palette.
func parseITerm(data []byte, fallbackName string) (*ImportedScheme, error) {
	var p itermPlist
	if err := xml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	out := &ImportedScheme{Name: fallbackName, Source: "iterm2", Colors: map[string]string{}}

	for i, name := range p.Dict.Keys {
		if i >= len(p.Dict.Dicts) {
			break
		}
		d := p.Dict.Dicts[i]
		// Component order in the file follows its own key list.
		var rr, gg, bb float64
		for j, k := range d.Keys {
			if j >= len(d.Reals) {
				break
			}
			switch k {
			case "Red Component":
				rr = d.Reals[j]
			case "Green Component":
				gg = d.Reals[j]
			case "Blue Component":
				bb = d.Reals[j]
			}
		}
		hex := rgbToHex(int(math.Round(rr*255)), int(math.Round(gg*255)), int(math.Round(bb*255)))

		switch {
		case name == "Background Color":
			out.Colors["background"] = hex
		case name == "Foreground Color":
			out.Colors["foreground"] = hex
		case name == "Cursor Color":
			out.Colors["cursor"] = hex
		case name == "Selection Color":
			out.Colors["selectionBackground"] = hex
		case strings.HasPrefix(name, "Ansi "):
			// "Ansi 0 Color" … "Ansi 15 Color"
			f := strings.Fields(name)
			if len(f) < 2 {
				continue
			}
			idx, err := strconv.Atoi(f[1])
			if err != nil || idx < 0 || idx > 15 {
				continue
			}
			if idx < 8 {
				out.Colors[ansiOrder[idx]] = hex
			} else {
				out.Colors[ansiBright[idx-8]] = hex
			}
		}
	}
	if len(out.Colors) == 0 {
		return nil, fmt.Errorf("no colours found")
	}
	return out, nil
}

// ------------------------------------------------------- generic hex format

var hexLineRe = regexp.MustCompile(`(?i)^\s*["']?([a-z0-9_.\- ]+?)["']?\s*[:=]\s*["']?(#[0-9a-f]{6}|0x[0-9a-f]{6})["']?`)

// parseHexish handles the many "key = #rrggbb" formats (Kitty, Alacritty,
// WezTerm, Ghostty, Windows Terminal / VS Code JSON, …) with one loose
// line-based parser. Unknown keys are ignored.
func parseHexish(r io.Reader, fallbackName string) (*ImportedScheme, error) {
	out := &ImportedScheme{Name: fallbackName, Source: "hex", Colors: map[string]string{}}

	// Aliases seen across the ecosystem → our keys.
	alias := map[string]string{
		"background": "background", "bg": "background",
		"foreground": "foreground", "fg": "foreground",
		"cursor": "cursor", "cursorcolor": "cursor", "cursor_bg": "cursor",
		"selection": "selectionBackground", "selection_bg": "selectionBackground",
		"selectionbackground": "selectionBackground",
		"black": "black", "red": "red", "green": "green", "yellow": "yellow",
		"blue": "blue", "magenta": "magenta", "purple": "magenta",
		"cyan": "cyan", "white": "white",
		"brightblack": "brightBlack", "brightred": "brightRed",
		"brightgreen": "brightGreen", "brightyellow": "brightYellow",
		"brightblue": "brightBlue", "brightmagenta": "brightMagenta",
		"brightpurple": "brightMagenta", "brightcyan": "brightCyan",
		"brightwhite": "brightWhite",
	}

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		m := hexLineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(m[1]))
		key = strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(key)
		hex := strings.ToLower(m[2])
		hex = "#" + strings.TrimPrefix(strings.TrimPrefix(hex, "#"), "0x")

		// colorN (kitty: color0..color15)
		if strings.HasPrefix(key, "color") {
			if idx, err := strconv.Atoi(strings.TrimPrefix(key, "color")); err == nil && idx >= 0 && idx <= 15 {
				if idx < 8 {
					out.Colors[ansiOrder[idx]] = hex
				} else {
					out.Colors[ansiBright[idx-8]] = hex
				}
				continue
			}
		}
		// terminal.ansiBlue (VS Code) → blue
		key = strings.TrimPrefix(key, "terminalansi")
		key = strings.TrimPrefix(key, "terminal")
		if mapped, ok := alias[key]; ok {
			out.Colors[mapped] = hex
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out.Colors) < 4 {
		return nil, fmt.Errorf("not enough colours found")
	}
	return out, nil
}

// ---------------------------------------------------------------- dispatch

// parseSchemeFile picks a parser by extension, then by content.
func parseSchemeFile(path string) (*ImportedScheme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	switch strings.ToLower(filepath.Ext(path)) {
	case ".colorscheme":
		return parseKonsole(strings.NewReader(string(data)), base)
	case ".itermcolors":
		return parseITerm(data, base)
	}
	// Fall back on content sniffing.
	if strings.Contains(string(data), "<plist") {
		return parseITerm(data, base)
	}
	if strings.Contains(string(data), "[Background]") {
		return parseKonsole(strings.NewReader(string(data)), base)
	}
	return parseHexish(strings.NewReader(string(data)), base)
}
