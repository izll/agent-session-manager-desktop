package session

import (
	"strings"
	"testing"
)

// codexApprovalPane is a real Codex approval prompt, captured from a running
// session. The generic "allow once" / "do you want to proceed" wording the
// detector originally looked for appears nowhere in it, which is why these
// questions went unnoticed.
const codexApprovalPane = `
• Running proxy_uri=$(sed -n 's/.*proxyUrl.*/\1/p' /var/www/Service.php | head -n 1)
  │ for target_url in 'http://example.invalid/'; do
  │   echo "URL: $target_url"
  │ … +2 lines


  Would you like to run the following command?

  Thread: Agent (019f9e24)

  Environment: local

  Reason: Engedélyezed a nyilvános JavaScript fájlok lekérését?

  $ curl -sS -o /tmp/a.js 'https://example.invalid/a.js' && wc -c /tmp/a.js

› 1. Yes, proceed (y)
  2. Yes, and don't ask again for commands that start with ` + "`wc -c`" + ` (p)
  3. No, and tell Codex what to do differently (esc)

  Press enter to confirm or esc to cancel or o to open thread
`

func TestCodexApprovalPromptIsWaiting(t *testing.T) {
	lines := strings.Split(codexApprovalPane, "\n")
	got := detectCodexActivity(lines, agentPatterns[AgentCodex])
	if got != ActivityWaiting {
		t.Errorf("a Codex approval prompt was detected as %v, want waiting", got)
	}
}

// A turn in flight must stay "busy" — the approval patterns must not fire on
// ordinary output.
func TestCodexWorkingIsBusy(t *testing.T) {
	pane := `
• Explored repository layout
  │ reading src/main.go

• Working (12s · esc to interrupt)
`
	got := detectCodexActivity(strings.Split(pane, "\n"), agentPatterns[AgentCodex])
	if got != ActivityBusy {
		t.Errorf("a working Codex was detected as %v, want busy", got)
	}
}

// A finished turn is idle: neither the approval nor the busy markers are left
// on screen.
func TestCodexIdleAfterAnswer(t *testing.T) {
	pane := `
• Ran wc -c /tmp/a.js
  │ 1234 /tmp/a.js

• Done.

  gpt-5.6 high · ~/projects/example
`
	got := detectCodexActivity(strings.Split(pane, "\n"), agentPatterns[AgentCodex])
	if got != ActivityIdle {
		t.Errorf("a finished Codex turn was detected as %v, want idle", got)
	}
}

// The word "proceed" inside ordinary output must not be mistaken for a
// prompt — only the actual approval wording counts.
func TestCodexProseIsNotWaiting(t *testing.T) {
	pane := `
• I will proceed with the refactor once the tests pass.
  │ No approval is needed for this step.

  gpt-5.6 high · ~/projects/example
`
	got := detectCodexActivity(strings.Split(pane, "\n"), agentPatterns[AgentCodex])
	if got == ActivityWaiting {
		t.Error("ordinary prose was mistaken for an approval prompt")
	}
}
