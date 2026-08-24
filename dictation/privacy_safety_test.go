package dictation

import (
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
		time.Sleep(30 * time.Second)
		return
	case "hang":
		child := exec.Command(os.Args[0], "-test.run=^TestKeyboardCommandHasTimeAndOutputBounds$")
		child.Env = append(os.Environ(), "ASMGR_KEYBOARD_HELPER=leaf")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		_ = child.Start()
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
