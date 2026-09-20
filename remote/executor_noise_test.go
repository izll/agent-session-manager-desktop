package remote

import "testing"

// Commands run through a login shell, because that is the only way an agent
// under ~/.local/bin is on the PATH at all — and a login shell's startup files
// write to stderr. The first line of stderr is therefore often not the
// failure.
//
// The bug: a tab that could not be created reported
//
//	failed to recreate window 100: tmux new-window failed on srv-web:
//	mesg: ttyname failed: Helytelen ioctl hívás az eszköznek
//
// while the real reason — "no server running" — sat on the line below and
// never reached the user.
func TestTheShellsOwnNoiseIsNotReportedAsTheFailure(t *testing.T) {
	stderr := "mesg: ttyname failed: Helytelen ioctl hívás az eszköznek\n" +
		"no server running on /tmp/tmux-0/default\n"

	if got := firstLine(stderr); got != "no server running on /tmp/tmux-0/default" {
		t.Errorf("reported %q instead of the real failure", got)
	}
}

// The message is localised — it arrives in whatever language the server is set
// to — so the match is on the emitter, not on the wording.
func TestNoiseIsRecognisedWhateverLanguageItIsIn(t *testing.T) {
	for _, line := range []string{
		"mesg: ttyname failed: Inappropriate ioctl for device",
		"mesg: ttyname failed: Helytelen ioctl hívás az eszköznek",
		"stty: 'standard input': Inappropriate ioctl for device",
		"bash: cannot set terminal process group (-1): Inappropriate ioctl for device",
	} {
		if !isLoginShellNoise(line) {
			t.Errorf("not recognised as shell noise: %q", line)
		}
	}
}

// A real failure must never be mistaken for noise.
func TestRealFailuresAreNotSwallowed(t *testing.T) {
	for _, line := range []string{
		"can't find session: asm_claude_x",
		"no server running on /tmp/tmux-0/default",
		"can't find window 100",
		"duplicate session: asm_claude_x",
	} {
		if isLoginShellNoise(line) {
			t.Errorf("a real failure was treated as noise: %q", line)
		}
	}
}

// With nothing but noise there is still something to say, rather than an
// empty message.
func TestNothingButNoiseStillReportsSomething(t *testing.T) {
	if got := firstLine("mesg: ttyname failed\n", ""); got != "no output" {
		t.Errorf("got %q", got)
	}
}
