package dictation

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type privacyPopup struct{ text string }

func (p *privacyPopup) AppendText(text string) { p.text += text }
func (p *privacyPopup) DeleteChars(count int)  {}
func (p *privacyPopup) GetText() string        { return p.text }
func (p *privacyPopup) SetText(text string)    { p.text = text }

func TestTypedTextIsNotPersistedInDictationLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // os.UserHomeDir reads this on Windows
	t.Setenv("XDG_CONFIG_HOME", dir)
	resetLoggingState(t)
	if err := InitLogging(true); err != nil {
		t.Fatal(err)
	}
	ApplyLoggingSettings(true, true)

	const secret = "round-nine-private-dictation"
	keyboard := &KeyboardSimulatorSimple{}
	keyboard.SetPopupHandler(&privacyPopup{})
	if err := keyboard.TypeText(secret); err != nil {
		t.Fatal(err)
	}
	flushLog(t)
	if body := readDictationLog(t); strings.Contains(body, secret) {
		t.Fatalf("dictation log persisted typed content: %q", body)
	}
}

func TestKeyboardCommandHasTimeAndOutputBounds(t *testing.T) {
	switch os.Getenv("ASMGR_KEYBOARD_HELPER") {
	case "leaf":
		// Block on a pipe the parent holds instead of sleeping, so this process
		// goes away the moment the parent is killed. A fixed sleep outlives it,
		// and on Windows a running .exe cannot be deleted — the leftover
		// grandchild made `go test` fail cleaning its own build cache, long
		// after every test had passed.
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	case "hang":
		child := exec.Command(os.Args[0], "-test.run=^TestKeyboardCommandHasTimeAndOutputBounds$")
		child.Env = append(os.Environ(), "ASMGR_KEYBOARD_HELPER=leaf")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		// The grandchild inherits this end of the pipe; it closes when this
		// process dies, which unblocks and ends the grandchild.
		stdin, pipeErr := child.StdinPipe()
		if pipeErr == nil {
			defer stdin.Close()
		}
		_ = child.Start()
		// This process is the one runKeyboardCommand must time out on, so it
		// does have to block for longer than the timeout. It is killed by the
		// context; only the grandchild needed a way out.
		time.Sleep(30 * time.Second)
		return
	}
	oldTimeout := keyboardCommandTimeout
	keyboardCommandTimeout = 100 * time.Millisecond
	t.Cleanup(func() { keyboardCommandTimeout = oldTimeout })
	t.Setenv("ASMGR_KEYBOARD_HELPER", "hang")

	started := time.Now()
	if _, err := runKeyboardCommand(os.Args[0], "-test.run=^TestKeyboardCommandHasTimeAndOutputBounds$"); err == nil {
		t.Fatal("hanging keyboard helper unexpectedly succeeded")
	}
	// The helpers run this very test binary. Windows will not delete a running
	// .exe, so any helper still winding down when the test ends makes `go test`
	// fail clearing its own build cache — after every test has already passed.
	// runKeyboardCommand's context kill does not wait for the tree to go away,
	// so give it a moment here rather than leaving it to chance.
	time.Sleep(250 * time.Millisecond)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("keyboard helper ignored timeout for %v", elapsed)
	}

	var output keyboardCommandOutput
	payload := []byte(strings.Repeat("x", keyboardCommandOutputLimit+1024))
	if n, err := output.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("bounded writer = (%d, %v), want (%d, nil)", n, err, len(payload))
	}
	if len(output.data) != keyboardCommandOutputLimit || !output.truncated {
		t.Fatalf("output bound = %d truncated=%v", len(output.data), output.truncated)
	}
}

func TestSpeechAPIResponseIsBounded(t *testing.T) {
	if _, err := readSpeechAPIResponse(strings.NewReader(strings.Repeat("x", maxSpeechAPIResponseBytes+1))); err == nil {
		t.Fatal("oversized speech API response was accepted")
	}
	if body, err := readSpeechAPIResponse(strings.NewReader("ok")); err != nil || string(body) != "ok" {
		t.Fatalf("small response = %q, %v", body, err)
	}
}

func TestSensitiveTracingPatternsStayOutOfSource(t *testing.T) {
	files := []string{"speech_recognizer.go", "streaming_recognizer.go", "keyboard_sim_simple.go", "hotkey_manager.go"}
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		for _, forbidden := range []string{
			"Transcript: '%s'", "Full transcript: '%s'", "FINAL: '%s'",
			"Response body: %s", "Key pressed: rawcode=", "Typing: '%s'",
			"Screen now has: '%s'", "Screen has (typed): '%s'",
			"Ctrl pressed", "Alt pressed", "Shift pressed",
		} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s still contains sensitive trace pattern %q", name, forbidden)
			}
		}
	}
}
