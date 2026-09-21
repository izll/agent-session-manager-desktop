package session

import (
	"os"
	"strings"
	"testing"
)

// The app names the conversation an agent should open — "--session-id <uuid>"
// for a fresh one — and a user's extra arguments are appended after it. Type
// your own "--resume 3433" and the agent is handed both, which Claude refuses
// outright:
//
//	Error: --session-id can only be used with --continue or --resume
//	       if --fork-session is also specified.
//
// The tab then failed to start, with the reason erased by the dead pane.
func TestAUserResumeFlagReplacesTheGeneratedOne(t *testing.T) {
	if !ExtraArgsSetConversation("--resume 3433 --dangerously-skip-permissions") {
		t.Error("a user's --resume was not recognised, so the app adds its own " +
			"--session-id alongside it and the agent refuses both")
	}
}

// The forms a user actually types, including the ones the CLIs accept as
// shorthand or with "=".
func TestEveryWayOfNamingAConversationIsRecognised(t *testing.T) {
	for _, extra := range []string{
		"--resume 3433",
		"--resume=3433",
		"-r 3433",
		"--continue",
		"-c",
		"--session-id 11111111-2222-3333-4444-555555555555",
		"--conversation abc",
		"--fork-session",
		"--model opus --resume 3433",
	} {
		if !ExtraArgsSetConversation(extra) {
			t.Errorf("not recognised as choosing a conversation: %q", extra)
		}
	}
}

// Anything else must leave the app's own flag alone — silently dropping it
// would lose the conversation id the session is tracked by.
func TestOrdinaryExtraArgsDoNotSuppressTheGeneratedID(t *testing.T) {
	for _, extra := range []string{
		"",
		"--dangerously-skip-permissions",
		"--model opus --verbose",
		// A value that merely contains the word must not count.
		"--append-system-prompt 'resume the work'",
	} {
		if ExtraArgsSetConversation(extra) {
			t.Errorf("wrongly treated as choosing a conversation: %q", extra)
		}
	}
}

// Every place that adds the flag has to ask, or the fault comes back on
// whichever path was missed — session start, tab start, restart, new tab, fork.
func TestEverySiteThatAddsTheFlagChecksTheExtraArgs(t *testing.T) {
	source, err := os.ReadFile("instance.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	adds := strings.Count(text, "config.SessionIDFlag,")
	asks := strings.Count(text, "ExtraArgsSetConversation(")

	if asks < adds {
		t.Errorf("the conversation flag is added in %d places but only %d ask "+
			"whether the user already named one", adds, asks)
	}
}

// A window takes its scrollback limit when it is created and never looks
// again. Setting the limit on the session afterwards — as this did — therefore
// reached every window except the one the agent runs in, which kept the stock
// 10000 lines while every tab opened later got 50000.
//
// Measured on a live multiplexer: with the option applied to an existing
// session, window 0 reported 10000 and a window created afterwards reported
// 50000.
func TestScrollbackIsSetBeforeTheSessionExists(t *testing.T) {
	source, err := os.ReadFile("instance.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	setAt := strings.Index(text, `"set-option", "-g", "history-limit"`)
	createAt := strings.Index(text, `[]string{"new-session", "-d", "-s", sessionName`)

	if setAt < 0 {
		t.Fatal("the scrollback is no longer set on the multiplexer server, so " +
			"the session's first window keeps the default")
	}
	if createAt < 0 {
		t.Fatal("session creation moved; this test needs rewriting")
	}
	if setAt > createAt {
		t.Error("the scrollback is set after the session is created, which is " +
			"too late for the window the agent runs in")
	}
}
