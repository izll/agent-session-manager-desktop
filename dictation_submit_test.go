package main

import (
	"strings"
	"testing"

	"asmgr-desktop/session"
)

// Dictation is not typing: what comes back is often close but not right, and a
// prompt submitted the moment it is transcribed cannot be corrected. The
// setting decides whether the send path presses Enter after the text.

func TestSubmitByDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	storage, err := session.NewStorage()
	if err != nil {
		t.Fatal(err)
	}
	_, _, settings, err := storage.LoadAllWithSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.DictationSendWithoutEnter {
		t.Fatal("dictation should submit unless asked not to — the old behaviour " +
			"is what everyone already has")
	}
}

// The send path has to consult the setting; reading it and ignoring it would
// leave a toggle that does nothing.
func TestSendPathHonoursTheSetting(t *testing.T) {
	src := readTextFile(t, "app.go")
	at := strings.Index(src, "func (a *App) SendPromptToWindow(")
	if at < 0 {
		t.Fatal("SendPromptToWindow is gone")
	}
	body := src[at:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "DictationSendWithoutEnter") {
		t.Fatal("the send path never looks at the setting, so the toggle does nothing")
	}
	if !strings.Contains(body, "SendPromptToWindowWithSubmit") {
		t.Fatal("the send path cannot pass the choice on")
	}
}

// Enter is a separate key event after the text, and skipping it is the whole
// feature — so the instance method must actually branch on it.
func TestInstanceSkipsEnterWhenAsked(t *testing.T) {
	src := readTextFile(t, "session/instance.go")
	at := strings.Index(src, "func (i *Instance) SendPromptToWindowWithSubmit(")
	if at < 0 {
		t.Fatal("SendPromptToWindowWithSubmit is gone")
	}
	body := src[at:]
	if end := strings.Index(body, "\n// IsMainWindowDead"); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "if !submit {") {
		t.Fatal("nothing skips the Enter, so the text is submitted either way")
	}
	enterAt := strings.Index(body, `"send-keys", "-t", sessionName, "Enter"`)
	guardAt := strings.Index(body, "if !submit {")
	if enterAt < 0 || guardAt > enterAt {
		t.Fatal("the guard must come before the Enter, or it never prevents it")
	}
}

// The old entry point keeps its behaviour, so every other caller is unaffected.
func TestTheOriginalMethodStillSubmits(t *testing.T) {
	src := readTextFile(t, "session/instance.go")
	at := strings.Index(src, "func (i *Instance) SendPromptToWindow(text string, windowIdx int) error {")
	if at < 0 {
		t.Fatal("SendPromptToWindow is gone")
	}
	body := src[at : at+300]
	if !strings.Contains(body, "SendPromptToWindowWithSubmit(text, windowIdx, true)") {
		t.Fatal("the original method no longer submits, which changes every other caller")
	}
}
