package session

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

// Control mode is how the Windows build talks to the multiplexer. The parser
// and the escape decoder live in this platform-neutral file on purpose: they
// carry all the logic that can actually be wrong, and a decoder compiled only
// for the OS nobody develops on is a decoder that rots silently. Only the
// process wiring is behind a build tag (control_mode_windows.go).
//
// WHY control mode at all — do not "simplify" this back to a plain attach:
// psmux's ordinary attach client demands a real console. Measured on Windows,
// `psmux attach-session` with stdout redirected to a pipe writes 44 bytes and
// exits immediately:
//
//	tmux 3.3.7
//	psmux 3.3.7 (05cc5d4 2026-07-20)
//
// That banner — not the session — is what the terminal pane showed. Setting
// TERM=xterm-256color changes nothing. `psmux -CC attach-session` over the same
// pipes produces the real escape stream, because control mode is designed for a
// program on the other end rather than a terminal.

// controlPrefixOutput introduces a pane data line: `%output %<pane> <payload>`.
const controlPrefixOutput = "%output "

// decodeOctalEscapes turns a control-mode %output payload into the raw bytes a
// PTY would have produced.
//
// The multiplexer escapes a byte as a backslash followed by EXACTLY three octal
// digits; a literal backslash is \134. Everything else — including the
// individual bytes of a multi-byte UTF-8 character — travels raw. Verified
// against a live stream: a box-drawing character arrives as the raw bytes
// e2 94 80, while tab/CR/LF/ESC arrive as \011 \015 \012 \033.
//
// This is deliberately byte-oriented, never rune-oriented. Decoding as runes
// would corrupt exactly the payloads that matter: a raw UTF-8 sequence split
// across a chunk boundary, or a byte that is not valid UTF-8 on its own, is
// perfectly legal here and must survive untouched.
func decodeOctalEscapes(payload []byte) []byte {
	// The decoded form is never longer than the input, so one allocation of
	// len(payload) is always enough and never has to grow.
	out := make([]byte, 0, len(payload))

	for i := 0; i < len(payload); {
		c := payload[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		// A backslash that is not followed by three octal digits is not an
		// escape the multiplexer produced. Emit it literally rather than
		// dropping it: silently swallowing a byte corrupts the display, and
		// passing an unknown sequence through is the recoverable failure.
		if i+3 < len(payload) && isOctalDigit(payload[i+1]) && isOctalDigit(payload[i+2]) && isOctalDigit(payload[i+3]) {
			out = append(out, (payload[i+1]-'0')<<6|(payload[i+2]-'0')<<3|(payload[i+3]-'0'))
			i += 4
			continue
		}
		out = append(out, c)
		i++
	}
	return out
}

func isOctalDigit(b byte) bool { return b >= '0' && b <= '7' }

// parseOutputLine splits a control-mode line into its pane id and decoded
// payload. ok is false for every other notification (%begin, %end, %exit,
// %window-add, %session-changed, the leading DCS …), which are consumed rather
// than forwarded — they are protocol chatter, and passing them to xterm.js
// would paint protocol text into the user's terminal.
func parseOutputLine(line []byte) (pane string, data []byte, ok bool) {
	if !bytes.HasPrefix(line, []byte(controlPrefixOutput)) {
		return "", nil, false
	}
	rest := line[len(controlPrefixOutput):]

	// `%output %1 <payload>`: the pane id runs to the next space. The payload
	// itself may be empty, and may contain spaces, so split exactly once.
	sp := bytes.IndexByte(rest, ' ')
	if sp < 0 {
		// A pane id with no payload at all is still a well-formed %output.
		return string(rest), nil, len(rest) > 0
	}
	return string(rest[:sp]), decodeOctalEscapes(rest[sp+1:]), true
}

// controlModeReader turns the control-mode notification stream into the flat
// byte stream xterm.js expects.
type controlModeReader struct {
	lines *bufio.Reader
	// pending holds decoded bytes not yet handed to Read: one %output line can
	// be far larger than the caller's buffer.
	pending []byte

	// Kept for the diagnostic on stream end: which notification came last, and
	// how many arrived. A terminal that stops updating looks identical from the
	// UI whether the client exited or simply went quiet — these tell them apart.
	notifications    int
	lastNotification string
}

// logNotification records a protocol line, and logs the ones that explain a
// terminal going silent. Only %exit and %error are worth a line each; the rest
// are counted so the summary on stream end has something to report.
func (c *controlModeReader) logNotification(line []byte) {
	c.notifications++
	c.lastNotification = string(line)
	if bytes.HasPrefix(line, []byte("%exit")) || bytes.HasPrefix(line, []byte("%error")) {
		debugf("[control] %s", line)
	}
}

func newControlModeReader(r io.Reader) *controlModeReader {
	return &controlModeReader{lines: bufio.NewReaderSize(r, 64*1024)}
}

// primeWithScreen seeds the reader with an initial screenful, delivered ahead of
// anything the live stream produces.
//
// Control mode reports CHANGES, not state: attaching to a session whose screen
// was already painted yields %begin/%end/%window-add/%session-changed and then
// silence, with the first %output arriving only when something in the pane next
// redraws. Measured against a live agent session — six protocol lines, zero
// %output, until a keystroke was injected. For a UI that is sitting at an idle
// prompt that means an indefinitely blank terminal, which is exactly the empty
// window this fixes.
//
// The snapshot comes from capture-pane -e (escape sequences retained), so
// colours and box drawing survive; it is written into pending so the very first
// Read hands back the screen before any live update.
func (c *controlModeReader) primeWithScreen(screen []byte) {
	if len(screen) == 0 {
		return
	}
	c.pending = append(append([]byte(nil), screen...), c.pending...)
}

// Read returns the decoded pane output, and nothing else.
//
// Long lines: bufio.Scanner is deliberately NOT used here. Its default token
// limit is 64KB and it stops silently once a line exceeds it — and %output
// lines genuinely do reach several kilobytes and beyond when a full-screen UI
// repaints. bufio.Reader.ReadBytes has no such ceiling: the 64KB size above is
// only the read buffer, not a maximum token, so a longer line is assembled
// across refills instead of truncated. A dropped line is a corrupted terminal,
// so the failure mode has to be impossible, not merely unlikely.
func (c *controlModeReader) Read(p []byte) (int, error) {
	for len(c.pending) == 0 {
		line, err := c.lines.ReadBytes('\n')
		if len(line) > 0 {
			// The stream is CRLF-terminated; strip both so a stray CR never
			// reaches the terminal as a carriage return of its own.
			line = bytes.TrimRight(line, "\r\n")
			if _, data, ok := parseOutputLine(line); ok {
				c.pending = data
			} else {
				// Protocol chatter is consumed rather than forwarded, which
				// also means a client dying is invisible from the outside: the
				// terminal simply stops updating while input still works. Under
				// --debug the notifications are logged, so a silent death can
				// be told apart from a stall — %exit means the multiplexer let
				// go, no %exit means something else did.
				c.logNotification(line)
			}
		}
		if err != nil {
			// Flush whatever the final partial line held before reporting the
			// error, otherwise the last screen update is lost on a clean exit.
			if len(c.pending) > 0 {
				break
			}
			debugf("[control] stream ended after %d notifications, last=%q: %v",
				c.notifications, c.lastNotification, err)
			return 0, err
		}
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

// keystrokeArgs builds the argv for delivering arbitrary input bytes to a pane.
//
// Keystrokes deliberately do NOT go down the control-mode stdin channel, even
// though that channel is already open and carries every other command. That
// channel is a text command parser, and psmux mangles payloads on it: sending
// `echo A"B'C-Dé` arrived as `echoAB\C-D` + c3 83 c2 a9 — spaces and quotes
// swallowed, and é (c3 a9) re-encoded per code point. Its `-H` (hex) flag,
// which would sidestep the parser, does not exist in psmux at all: it is
// undocumented (`psmux --help` lists only -l, -p and -t) and the hex digits are
// echoed into the pane as literal text, which is exactly the stray "1b 5b 3c …"
// users saw on screen.
//
// Passing the bytes as an argv element instead skips the parser entirely —
// Go's exec does not go through a shell, so nothing re-quotes or re-encodes
// them. Measured on psmux 3.3.7: quote, hyphen and é all arrive intact.
//
// One byte does not survive it: an apostrophe is swallowed by psmux itself.
// Sweeping every printable ASCII byte through this exact path showed 0x27 to
// be the ONLY casualty — `abc'def` arrives as `abcdef` — so it is routed
// around separately (see keystrokeCommands).
func keystrokeArgs(pane string, data []byte) []string {
	args := make([]string, 0, 5)
	args = append(args, "send-keys")
	if pane != "" {
		args = append(args, "-t", pane)
	}
	// -l is "literal": no key-name parsing, so ESC sequences and UTF-8 pass
	// through as the bytes they are.
	args = append(args, "-l", string(data))
	return args
}

// keystrokeCommands splits input into the sequence of send-keys argv lists that
// delivers it, because Enter cannot travel as a literal byte.
//
// xterm.js sends CR (0x0d) when the user presses Enter, but under -l that byte
// is inserted into the command line as a character instead of submitting it.
// Measured on psmux: `send-keys -l "echo CRTESZT\r"` left the prompt sitting at
// `echo CRTESZT` unexecuted, and only a following `send-keys Enter` ran it. A
// terminal where Enter does nothing is not a terminal, so every CR becomes its
// own key-name command, and the literal runs between them keep byte fidelity.
//
// LF (0x0a) is deliberately NOT treated this way. A terminal sends CR for
// Enter; a bare LF arriving as data (bracketed paste, a here-doc) must stay a
// byte, or pasted multi-line text would execute line by line.
//
// An apostrophe needs the same detour for a different reason: psmux drops 0x27
// from a -l payload outright. It travels as the key name `0x27` instead, which
// psmux does accept — that hex-name form is undocumented but real, and it is
// the counterpart to the -H FLAG, which does not exist.
//
// Why not send everything as 0x-names, which would be uniform: that form
// re-encodes bytes per code point, so é (c3 a9) arrives as Ã©. The two routes
// have exactly complementary blind spots, hence the split — -l for byte
// fidelity, 0x27 for the one byte -l loses.
func keystrokeCommands(pane string, data []byte) [][]string {
	var cmds [][]string
	// flush emits the literal run accumulated before a byte needing its own
	// command, skipping empty runs so consecutive specials produce no blanks.
	flush := func(run []byte) {
		if len(run) > 0 {
			cmds = append(cmds, keystrokeArgs(pane, run))
		}
	}
	start := 0
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\r':
			flush(data[start:i])
			cmds = append(cmds, enterArgs(pane))
			start = i + 1
		case '\'':
			flush(data[start:i])
			cmds = append(cmds, hexNameArgs(pane, '\''))
			start = i + 1
		}
	}
	flush(data[start:])
	return cmds
}

// hexNameArgs sends a single byte by its `0x<hex>` key name, for bytes that a
// -l payload cannot carry. Like enterArgs it omits -l: the name must be parsed.
func hexNameArgs(pane string, b byte) []string {
	args := make([]string, 0, 4)
	args = append(args, "send-keys")
	if pane != "" {
		args = append(args, "-t", pane)
	}
	return append(args, fmt.Sprintf("0x%02x", b))
}

// enterArgs sends Enter by key name — the only form that submits the line.
// Note the absence of -l: the name has to be parsed, not taken literally.
func enterArgs(pane string) []string {
	args := make([]string, 0, 4)
	args = append(args, "send-keys")
	if pane != "" {
		args = append(args, "-t", pane)
	}
	return append(args, "Enter")
}
