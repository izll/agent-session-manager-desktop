package session

import (
	"bytes"
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

func TestEncodeKeystrokes(t *testing.T) {
	tests := []struct {
		name string
		pane string
		in   []byte
		want string
	}{
		{
			name: "plain ascii",
			pane: "%1",
			in:   []byte("ls"),
			want: "send-keys -H -t %1 6c 73\n",
		},
		{
			// The bytes xterm.js sends for Enter and for the Up arrow.
			name: "carriage return and escape sequence",
			pane: "%1",
			in:   []byte("\r\x1b[A"),
			want: "send-keys -H -t %1 0d 1b 5b 41\n",
		},
		{
			// Backspace. -l would drop or misapply this one.
			name: "DEL",
			pane: "%1",
			in:   []byte{0x7f},
			want: "send-keys -H -t %1 7f\n",
		},
		{
			// -l re-encodes these as code points (c3 83 c2 a9); -H does not.
			name: "multi-byte UTF-8",
			pane: "%1",
			in:   []byte("é"),
			want: "send-keys -H -t %1 c3 a9\n",
		},
		{
			// Would need quoting in any literal form; hex is inert.
			name: "backslash and quotes need no quoting as hex",
			pane: "%1",
			in:   []byte(`\"';`),
			want: "send-keys -H -t %1 5c 22 27 3b\n",
		},
		{
			name: "control bytes including NUL",
			pane: "%1",
			in:   []byte{0x00, 0x01, 0x03},
			want: "send-keys -H -t %1 00 01 03\n",
		},
		{
			name: "pane id may be a session target",
			pane: "asm_claude_x:0",
			in:   []byte("a"),
			want: "send-keys -H -t asm_claude_x:0 61\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(encodeKeystrokes(tt.pane, tt.in)); got != tt.want {
				t.Fatalf("encodeKeystrokes = %q, want %q", got, tt.want)
			}
		})
	}
}

// Every byte value must survive the encoding as its own two-digit argument;
// this is the property the whole input path depends on.
func TestEncodeKeystrokesCoversEveryByteValue(t *testing.T) {
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	line := string(encodeKeystrokes("%1", all))

	fields := strings.Fields(strings.TrimSuffix(line, "\n"))
	// send-keys, -H, -t, %1, then one field per byte.
	if len(fields) != 4+256 {
		t.Fatalf("got %d fields, want %d", len(fields), 4+256)
	}
	for i, f := range fields[4:] {
		if len(f) != 2 {
			t.Fatalf("byte %d encoded as %q, want exactly two hex digits", i, f)
		}
	}
	if !strings.HasSuffix(line, "\n") {
		t.Fatal("command line must be newline-terminated or the parser never runs it")
	}
}

// Decoding must be the exact inverse of the multiplexer's escaping for every
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
