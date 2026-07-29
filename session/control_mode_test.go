package session

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

// These tests run on every platform even though control mode is only compiled
// on Windows: the decoder is where a bug would silently corrupt the terminal,
// and it must be exercised on the machine development actually happens on.
// Fragments below are taken verbatim from a control-mode transcript captured on
// a real Windows machine, plus a live stream captured on Linux.

func TestDecodeOctalEscapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []byte
	}{
		{
			name: "plain text passes through",
			in:   "bash-5.2$ ",
			want: []byte("bash-5.2$ "),
		},
		{
			name: "ESC is octal 033",
			in:   `\033[m`,
			want: []byte("\x1b[m"),
		},
		{
			// From the transcript: the very first %output payload.
			name: "modifyOtherKeys query from the transcript",
			in:   `\033[<u\033[>1u\033[>4;2m`,
			want: []byte("\x1b[<u\x1b[>1u\x1b[>4;2m"),
		},
		{
			name: "CRLF is 015 012",
			in:   `\015\012`,
			want: []byte("\r\n"),
		},
		{
			name: "tab is 011",
			in:   `tab[\011]`,
			want: []byte("tab[\t]"),
		},
		{
			// The case that forces byte-oriented decoding: these are the three
			// UTF-8 bytes of a box-drawing character, escaped individually.
			name: "box drawing character as octal UTF-8 bytes",
			in:   `\342\224\200`,
			want: []byte{0xe2, 0x94, 0x80},
		},
		{
			// From the transcript: the top-left rounded corner of Claude
			// Code's welcome box.
			name: "transcript box corner",
			in:   `\342\225\255`,
			want: []byte{0xe2, 0x95, 0xad},
		},
		{
			// The multiplexer leaves UTF-8 raw when it does not need escaping,
			// so both forms occur in the same stream.
			name: "raw UTF-8 bytes pass through unescaped",
			in:   "BOX\xe2\x94\x80END",
			want: []byte("BOX\xe2\x94\x80END"),
		},
		{
			name: "literal backslash is 134",
			in:   `BS[\134]`,
			want: []byte(`BS[\]`),
		},
		{
			// Two escaped backslashes in a row must decode to two, not one:
			// an off-by-one here would eat a character of the user's output.
			name: "double backslash",
			in:   `\134\134`,
			want: []byte(`\\`),
		},
		{
			// A backslash followed by octal-looking text must not consume the
			// text: only exactly three digits form an escape.
			name: "backslash then literal digits",
			in:   `\134342`,
			want: []byte(`\342`),
		},
		{
			name: "NUL and high bytes",
			in:   `\000\377`,
			want: []byte{0x00, 0xff},
		},
		{
			// Not valid UTF-8 on its own — must survive untouched rather than
			// being replaced by U+FFFD, which is what rune decoding would do.
			name: "lone continuation byte is not mangled",
			in:   `\200`,
			want: []byte{0x80},
		},
		{
			name: "empty payload",
			in:   ``,
			want: []byte{},
		},
		{
			// Malformed input must degrade, not drop bytes.
			name: "trailing backslash kept literally",
			in:   `abc\`,
			want: []byte(`abc\`),
		},
		{
			name: "backslash with too few digits kept literally",
			in:   `\01`,
			want: []byte(`\01`),
		},
		{
			name: "backslash before non-octal digit kept literally",
			in:   `\389`,
			want: []byte(`\389`),
		},
		{
			name: "escape at the very end of the payload",
			in:   `x\033`,
			want: []byte("x\x1b"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeOctalEscapes([]byte(tt.in))
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("decodeOctalEscapes(%q) = % x, want % x", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseOutputLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantOK   bool
		wantPane string
		wantData []byte
	}{
		{
			name:     "output line from the transcript",
			line:     `%output %1 \033[m`,
			wantOK:   true,
			wantPane: "%1",
			wantData: []byte("\x1b[m"),
		},
		{
			name:     "payload containing spaces is kept whole",
			line:     `%output %1 Welcome back Zolt\303\241n!`,
			wantOK:   true,
			wantPane: "%1",
			wantData: []byte("Welcome back Zoltán!"),
		},
		{
			name:     "multi-digit pane id",
			line:     `%output %657 hi`,
			wantOK:   true,
			wantPane: "%657",
			wantData: []byte("hi"),
		},
		{
			name:     "empty payload is still an output line",
			line:     `%output %1 `,
			wantOK:   true,
			wantPane: "%1",
			wantData: []byte{},
		},
		// Every other notification is protocol chatter and must be swallowed:
		// forwarding any of these would paint protocol text into the terminal.
		{name: "begin", line: `%begin 1785271950 1 0`},
		{name: "end", line: `%end 1785271950 1 0`},
		{name: "window-add", line: `%window-add @1`},
		{name: "sessions-changed", line: `%sessions-changed`},
		{name: "session-changed", line: `%session-changed $18 asm_claude_x`},
		{name: "exit", line: `%exit`},
		{name: "leading DCS is not a notification", line: "\x1bP1000p"},
		{name: "empty line", line: ``},
		{name: "output-prefixed but different command", line: `%output-foo %1 x`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pane, data, ok := parseOutputLine([]byte(tt.line))
			if ok != tt.wantOK {
				t.Fatalf("parseOutputLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if pane != tt.wantPane {
				t.Fatalf("pane = %q, want %q", pane, tt.wantPane)
			}
			if !bytes.Equal(data, tt.wantData) {
				t.Fatalf("data = % x, want % x", data, tt.wantData)
			}
		})
	}
}

// The reader must yield the pane bytes and nothing else, from a stream shaped
// exactly like the captured transcript (CRLF-terminated, DCS header first).
func TestControlModeReaderYieldsOnlyPaneOutput(t *testing.T) {
	stream := "\x1bP1000p%begin 1785271950 1 0\r\n" +
		"%end 1785271950 1 0\r\n" +
		"%window-add @1\r\n" +
		"%sessions-changed\r\n" +
		"%session-changed $18 asm_claude_asmgr-teszt-2\r\n" +
		`%output %1 \033[<u\033[>1u\033[>4;2m` + "\r\n" +
		`%output %1 \342\225\255Claude Code` + "\r\n" +
		`%output %1 \033[m` + "\r\n" +
		"%exit\r\n"

	got, err := io.ReadAll(newControlModeReader(strings.NewReader(stream)))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	want := "\x1b[<u\x1b[>1u\x1b[>4;2m" + "\xe2\x95\xadClaude Code" + "\x1b[m"
	if string(got) != want {
		t.Fatalf("reader produced % x, want % x", got, want)
	}
}

// A %output line larger than any scanner default must survive intact. This is
// the bufio.Scanner trap: its 64KB token limit would stop the stream silently.
func TestControlModeReaderHandlesLinesBeyondScannerLimit(t *testing.T) {
	const payloadBytes = 300 * 1024
	// Escaped form is 4x the decoded size, so the wire line is ~1.2MB.
	line := `%output %1 ` + strings.Repeat(`\101`, payloadBytes) + "\r\n"

	got, err := io.ReadAll(newControlModeReader(strings.NewReader(line)))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != payloadBytes {
		t.Fatalf("decoded %d bytes, want %d — a long line was truncated", len(got), payloadBytes)
	}
	if bytes.Trim(got, "A") != nil {
		t.Fatal("long line decoded to unexpected bytes")
	}
}

// Output must not be lost when the stream ends without a final newline.
func TestControlModeReaderFlushesFinalPartialLine(t *testing.T) {
	got, err := io.ReadAll(newControlModeReader(strings.NewReader(`%output %1 \033[m`)))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "\x1b[m" {
		t.Fatalf("got % x, want the final unterminated line", got)
	}
}

// A small caller buffer must not lose the remainder of a decoded line.
func TestControlModeReaderSplitsAcrossSmallReads(t *testing.T) {
	r := newControlModeReader(strings.NewReader("%output %1 abcdefghij\r\n"))
	var got []byte
	buf := make([]byte, 3)
	for {
		n, err := r.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
	}
	if string(got) != "abcdefghij" {
		t.Fatalf("got %q, want the whole payload reassembled", got)
	}
}

func TestKeystrokeArgs(t *testing.T) {
	tests := []struct {
		name string
		pane string
		in   []byte
		want []string
	}{
		{
			name: "plain text",
			pane: "%1",
			in:   []byte("ls"),
			want: []string{"send-keys", "-t", "%1", "-l", "ls"},
		},
		{
			// The payload is one argv element, so an embedded space cannot be
			// re-split into separate arguments.
			name: "spaces stay inside a single argument",
			pane: "%1",
			in:   []byte("echo hello world"),
			want: []string{"send-keys", "-t", "%1", "-l", "echo hello world"},
		},
		{
			// Quotes and backslashes would need escaping on a text command
			// channel; as an argv element they need none.
			name: "quotes and backslash pass through unescaped",
			pane: "%1",
			in:   []byte(`\"';`),
			want: []string{"send-keys", "-t", "%1", "-l", `\"';`},
		},
		{
			// The whole reason the hex encoding was abandoned: psmux echoed
			// "c3 a9" into the pane as text instead of decoding it.
			name: "multi-byte UTF-8 stays raw",
			pane: "%1",
			in:   []byte("é"),
			want: []string{"send-keys", "-t", "%1", "-l", "é"},
		},
		{
			name: "escape sequence stays raw",
			pane: "%1",
			in:   []byte{0x1b, '[', 'A'},
			want: []string{"send-keys", "-t", "%1", "-l", "\x1b[A"},
		},
		{
			// A leading dash must not turn into a flag: it stays inside the
			// payload argument, after -l.
			name: "leading dash is payload, not a flag",
			pane: "%1",
			in:   []byte("-rf"),
			want: []string{"send-keys", "-t", "%1", "-l", "-rf"},
		},
		{
			// Empty pane means the lookup failed; -t must then be omitted
			// entirely rather than passed as an empty target.
			name: "no pane omits the target flag",
			pane: "",
			in:   []byte("a"),
			want: []string{"send-keys", "-l", "a"},
		},
		{
			name: "pane id may be a session target",
			pane: "asm_claude_x:0",
			in:   []byte("a"),
			want: []string{"send-keys", "-t", "asm_claude_x:0", "-l", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keystrokeArgs(tt.pane, tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("keystrokeArgs = %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("keystrokeArgs = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// Every byte value must reach the pane unchanged. This is the property the
// whole input path depends on, and the one the hex encoding failed to deliver
// on psmux: the payload must survive as raw bytes in a single argv element.
func TestKeystrokeArgsCarriesEveryByteValue(t *testing.T) {
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	args := keystrokeArgs("%1", all)
	if len(args) != 5 {
		t.Fatalf("got %d args, want exactly 5 (payload must not be split)", len(args))
	}
	if got := []byte(args[4]); !bytes.Equal(got, all) {
		t.Fatalf("payload altered: got % x, want % x", got, all)
	}
}
// byte value, including those that are not valid UTF-8 on their own.
func TestDecodeOctalEscapesRoundTripsEveryByte(t *testing.T) {
	var escaped bytes.Buffer
	want := make([]byte, 256)
	for i := 0; i < 256; i++ {
		want[i] = byte(i)
		escaped.WriteString(octalEscape(byte(i)))
	}
	got := decodeOctalEscapes(escaped.Bytes())
	if !bytes.Equal(got, want) {
		t.Fatalf("round trip lost bytes:\n got % x\nwant % x", got, want)
	}
}

func octalEscape(b byte) string {
	const digits = "01234567"
	return string([]byte{'\\', digits[b>>6], digits[(b>>3)&7], digits[b&7]})
}

// Enter must leave as a key name, not as a literal CR byte. Measured on psmux:
// a literal \r lands in the command line without submitting it, so a terminal
// wired the naive way can type but never run anything.
func TestKeystrokeCommandsSendsEnterByName(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want [][]string
	}{
		{
			name: "text with no CR is a single literal command",
			in:   []byte("ls"),
			want: [][]string{{"send-keys", "-t", "%1", "-l", "ls"}},
		},
		{
			name: "trailing CR becomes its own Enter command",
			in:   []byte("ls\r"),
			want: [][]string{
				{"send-keys", "-t", "%1", "-l", "ls"},
				{"send-keys", "-t", "%1", "Enter"},
			},
		},
		{
			name: "a lone CR is just Enter, with no empty literal",
			in:   []byte("\r"),
			want: [][]string{{"send-keys", "-t", "%1", "Enter"}},
		},
		{
			name: "CR in the middle splits the payload",
			in:   []byte("a\rb"),
			want: [][]string{
				{"send-keys", "-t", "%1", "-l", "a"},
				{"send-keys", "-t", "%1", "Enter"},
				{"send-keys", "-t", "%1", "-l", "b"},
			},
		},
		{
			name: "consecutive CRs produce no empty literal between them",
			in:   []byte("\r\r"),
			want: [][]string{
				{"send-keys", "-t", "%1", "Enter"},
				{"send-keys", "-t", "%1", "Enter"},
			},
		},
		{
			// A pasted here-doc must not execute line by line.
			name: "bare LF stays a literal byte",
			in:   []byte("a\nb"),
			want: [][]string{{"send-keys", "-t", "%1", "-l", "a\nb"}},
		},
		{
			// CRLF: the CR submits, the LF stays data.
			name: "CRLF submits once and keeps the LF",
			in:   []byte("a\r\nb"),
			want: [][]string{
				{"send-keys", "-t", "%1", "-l", "a"},
				{"send-keys", "-t", "%1", "Enter"},
				{"send-keys", "-t", "%1", "-l", "\nb"},
			},
		},
		{
			name: "empty input produces no commands at all",
			in:   nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keystrokeCommands("%1", tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("keystrokeCommands = %q, want %q", got, tt.want)
			}
			for i := range got {
				if len(got[i]) != len(tt.want[i]) {
					t.Fatalf("command %d = %q, want %q", i, got[i], tt.want[i])
				}
				for j := range got[i] {
					if got[i][j] != tt.want[i][j] {
						t.Fatalf("command %d = %q, want %q", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

// Whatever the split, concatenating the literal payloads and one CR per Enter
// must reproduce the input exactly — no byte invented, dropped or reordered.
func TestKeystrokeCommandsPreservesEveryByte(t *testing.T) {
	in := make([]byte, 0, 512)
	for i := 0; i < 256; i++ {
		in = append(in, byte(i))
	}
	in = append(in, []byte("é\r\r\x1b[Aend\r")...)

	var rebuilt []byte
	for _, cmd := range keystrokeCommands("%1", in) {
		last := cmd[len(cmd)-1]
		switch {
		case last == "Enter":
			rebuilt = append(rebuilt, '\r')
		case strings.HasPrefix(last, "0x") && len(last) == 4:
			// A byte routed around -l as a hex key name.
			var b byte
			if _, err := fmt.Sscanf(last, "0x%02x", &b); err != nil {
				t.Fatalf("unparsable hex key name %q", last)
			}
			rebuilt = append(rebuilt, b)
		default:
			rebuilt = append(rebuilt, []byte(last)...)
		}
	}
	if !bytes.Equal(rebuilt, in) {
		t.Fatalf("round-trip altered the stream:\n got % x\nwant % x", rebuilt, in)
	}
}

// The opening frame must reach the caller before any live output, and must not
// disturb what the stream delivers afterwards.
func TestPrimeWithScreenIsDeliveredFirst(t *testing.T) {
	r := newControlModeReader(strings.NewReader("%output %1 live\r\n"))
	r.primeWithScreen([]byte("SNAPSHOT"))

	var got []byte
	buf := make([]byte, 64)
	for {
		n, err := r.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
	}
	if string(got) != "SNAPSHOTlive" {
		t.Fatalf("got %q, want the snapshot ahead of the live stream", got)
	}
}

// An empty snapshot is the failure path (capture-pane errored); it must leave
// the stream untouched rather than injecting anything.
func TestPrimeWithScreenIgnoresEmptySnapshot(t *testing.T) {
	r := newControlModeReader(strings.NewReader("%output %1 live\r\n"))
	r.primeWithScreen(nil)

	buf := make([]byte, 64)
	n, _ := r.Read(buf)
	if string(buf[:n]) != "live" {
		t.Fatalf("got %q, want the live stream unchanged", buf[:n])
	}
}

// The snapshot must survive a caller whose buffer is smaller than the screen —
// a full screenful is far larger than a typical read buffer.
func TestPrimeWithScreenSurvivesSmallReads(t *testing.T) {
	screen := bytes.Repeat([]byte("A"), 500)
	r := newControlModeReader(strings.NewReader("%output %1 Z\r\n"))
	r.primeWithScreen(screen)

	var got []byte
	buf := make([]byte, 7)
	for {
		n, err := r.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
	}
	if want := string(screen) + "Z"; string(got) != want {
		t.Fatalf("got %d bytes, want %d with the snapshot intact", len(got), len(want))
	}
}

// psmux drops 0x27 from a -l payload — a sweep of every printable ASCII byte
// through the real path showed it to be the only casualty. It has to leave as
// a hex key name instead, or typing an apostrophe does nothing at all.
func TestKeystrokeCommandsRoutesApostropheAround(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want [][]string
	}{
		{
			name: "lone apostrophe becomes a hex key name",
			in:   []byte("'"),
			want: [][]string{{"send-keys", "-t", "%1", "0x27"}},
		},
		{
			name: "apostrophe inside a word splits the literal run",
			in:   []byte("abc'def"),
			want: [][]string{
				{"send-keys", "-t", "%1", "-l", "abc"},
				{"send-keys", "-t", "%1", "0x27"},
				{"send-keys", "-t", "%1", "-l", "def"},
			},
		},
		{
			name: "consecutive apostrophes emit no empty literal",
			in:   []byte("''"),
			want: [][]string{
				{"send-keys", "-t", "%1", "0x27"},
				{"send-keys", "-t", "%1", "0x27"},
			},
		},
		{
			// The two detours have to compose: shell quoting plus Enter is the
			// single most ordinary thing a user types.
			name: "apostrophe and Enter together",
			in:   []byte("echo 'hi'\r"),
			want: [][]string{
				{"send-keys", "-t", "%1", "-l", "echo "},
				{"send-keys", "-t", "%1", "0x27"},
				{"send-keys", "-t", "%1", "-l", "hi"},
				{"send-keys", "-t", "%1", "0x27"},
				{"send-keys", "-t", "%1", "Enter"},
			},
		},
		{
			// A double quote is NOT affected and must stay in the literal run,
			// or every quoted string would fragment needlessly.
			name: "double quote stays literal",
			in:   []byte(`say "hi"`),
			want: [][]string{{"send-keys", "-t", "%1", "-l", `say "hi"`}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keystrokeCommands("%1", tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("keystrokeCommands = %q, want %q", got, tt.want)
			}
			for i := range got {
				if len(got[i]) != len(tt.want[i]) {
					t.Fatalf("command %d = %q, want %q", i, got[i], tt.want[i])
				}
				for j := range got[i] {
					if got[i][j] != tt.want[i][j] {
						t.Fatalf("command %d = %q, want %q", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

// UTF-8 must never take the hex-name route: that form re-encodes per code
// point, so é would arrive as Ã©. Only the apostrophe is special-cased.
func TestKeystrokeCommandsKeepsUTF8Literal(t *testing.T) {
	cmds := keystrokeCommands("%1", []byte("héllo"))
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1 — UTF-8 must not be split", len(cmds))
	}
	if last := cmds[0][len(cmds[0])-1]; last != "héllo" {
		t.Fatalf("payload = %q, want the raw UTF-8 text", last)
	}
}
