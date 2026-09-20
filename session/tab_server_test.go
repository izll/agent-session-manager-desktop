package session

import (
	"context"
	"os"
	"strings"
	"testing"
)

// A tab with no server of its own runs where its session runs. Every tab
// stored before tabs could be placed individually is in that state, so this is
// also the migration: an absent field reads as "same machine as the session".
func TestATabWithoutAServerFollowsItsSession(t *testing.T) {
	local := FollowedWindow{Index: 1}
	if got := local.RunsOn(""); got != "" {
		t.Errorf("a tab in a local session was placed on %q", got)
	}
	if got := local.RunsOn("srv1"); got != "srv1" {
		t.Errorf("a tab in a remote session was placed on %q, not its session's server", got)
	}

	pinned := FollowedWindow{Index: 2, ServerID: "srv2"}
	if got := pinned.RunsOn("srv1"); got != "srv2" {
		t.Errorf("a tab pinned to a server was placed on %q", got)
	}
}

// The bug this prevents: a window index identifies a window within ONE
// multiplexer. Deleting the tab that is window 2 on a server must not take the
// local tab that is also window 2.
//
// Indexes are kept apart (see remoteWindowIndexBase) so this cannot normally
// arise, but the matching is what makes the guarantee, and a stale descriptor
// or an imported session can still produce a collision.
func TestDeletingATabOnOneMachineLeavesTheOtherMachinesAlone(t *testing.T) {
	inst := &Instance{
		ID: "s1",
		FollowedWindows: []FollowedWindow{
			{Index: 2, Name: "local work"},
			{Index: 2, Name: "server work", ServerID: "srv1"},
		},
	}

	inst.removeWindowMetadataOn(2, "srv1")

	if len(inst.FollowedWindows) != 1 {
		t.Fatalf("expected one tab to survive, got %+v", inst.FollowedWindows)
	}
	if inst.FollowedWindows[0].Name != "local work" {
		t.Errorf("the wrong tab was removed: %+v", inst.FollowedWindows[0])
	}
}

// Restarting a tab has to find the descriptor for the tab on that machine, or
// it would restart another machine's tab with this one's command.
func TestRestartPicksTheTabOnTheRightMachine(t *testing.T) {
	windows := []FollowedWindow{
		{Index: 3, Agent: AgentTerminal, Name: "local"},
		{Index: 3, Agent: AgentClaude, Name: "remote", ServerID: "srv1"},
	}

	idx, _, err := selectFollowedWindowForRestart(windows, 3, "", "srv1")
	if err != nil {
		t.Fatal(err)
	}
	if idx != 1 || windows[idx].Name != "remote" {
		t.Errorf("restart chose %d (%+v), not the tab on srv1", idx, windows[idx])
	}

	idx, _, err = selectFollowedWindowForRestart(windows, 3, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if idx != 0 || windows[idx].Name != "local" {
		t.Errorf("restart chose %d (%+v), not the local tab", idx, windows[idx])
	}
}

// Indexes are what everything addresses a tab by — the UI, the terminal
// socket, the status poller. Tabs on a server take theirs from a range no
// local tab is given, so one session spanning two machines never has two tabs
// answering to the same number.
func TestRemoteTabsTakeIndexesLocalTabsNeverGet(t *testing.T) {
	inst := &Instance{
		ID: "s1",
		FollowedWindows: []FollowedWindow{
			{Index: 1}, {Index: 2}, {Index: 3},
		},
	}

	first := inst.nextRemoteWindowIndex("srv1")
	if first < remoteWindowIndexBase {
		t.Fatalf("a tab on a server was given index %d, inside the local range", first)
	}

	inst.FollowedWindows = append(inst.FollowedWindows,
		FollowedWindow{Index: first, ServerID: "srv1"})

	second := inst.nextRemoteWindowIndex("srv1")
	if second == first {
		t.Errorf("the same index %d was handed out twice", second)
	}

	// A second server gets its own band, so two servers cannot collide either.
	other := inst.nextRemoteWindowIndex("srv2")
	if other == first || other == second {
		t.Errorf("two servers were given the same index %d", other)
	}
}

// A session reaching two machines keeps a route to each at once: its own, and
// one per server its tabs sit on.
func TestASessionKeepsARoutePerMachineItReaches(t *testing.T) {
	sessionSide := &recordingExecutor{}
	tabSide := &recordingExecutor{}

	SetExecutor("s1", sessionSide)
	SetTabExecutor("s1", "srv2", tabSide)
	t.Cleanup(func() {
		ClearExecutor("s1")
		ClearTabExecutor("s1", "srv2")
	})

	inst := &Instance{ID: "s1", ServerID: "srv1"}

	if err := inst.tmuxRunOn("srv1", "has-session", "-t", "a"); err != nil {
		t.Fatal(err)
	}
	if err := inst.tmuxRunOn("srv2", "has-session", "-t", "b"); err != nil {
		t.Fatal(err)
	}

	if len(sessionSide.seen()) != 1 || sessionSide.seen()[0][2] != "a" {
		t.Errorf("the session's own command went astray: %v", sessionSide.seen())
	}
	if len(tabSide.seen()) != 1 || tabSide.seen()[0][2] != "b" {
		t.Errorf("the tab's command did not reach its own server: %v", tabSide.seen())
	}
	if inst.execOn("") != LocalExecutor {
		t.Error("a tab on this computer was routed to a server")
	}
}

// The bug this covers: tabs on a server were missing from the tab bar.
//
// GetWindowList asked one machine — the session's own — and a window index
// only means something inside one multiplexer, so every remote tab was simply
// absent from the listing and therefore from the bar.
func TestTabsOnAServerAreListedToo(t *testing.T) {
	local := &recordingExecutor{output: []byte("0:main:1:0\n1:notes:0:0\n")}
	remote := &recordingExecutor{output: []byte("100:db work:1:0\n")}

	SetExecutor("s1", local)
	SetTabExecutor("s1", "srv1", remote)
	t.Cleanup(func() {
		ClearExecutor("s1")
		ClearTabExecutor("s1", "srv1")
	})

	inst := &Instance{
		ID:     "s1",
		Status: StatusRunning,
		// The session itself runs on srv-less "local", so its own listing goes
		// through the session executor.
		ServerID: "",
		FollowedWindows: []FollowedWindow{
			{Index: 1, Name: "notes"},
			{Index: 100, Name: "db work", ServerID: "srv1"},
		},
	}
	windows := inst.GetWindowList()

	byIndex := make(map[int]WindowInfo, len(windows))
	for _, window := range windows {
		byIndex[window.Index] = window
	}
	if _, found := byIndex[100]; !found {
		t.Fatalf("the tab on the server is missing from the list: %+v", windows)
	}
	if _, found := byIndex[1]; !found {
		t.Errorf("a local tab was lost: %+v", windows)
	}
	if byIndex[100].Name != "db work" {
		t.Errorf("the remote tab came back as %+v", byIndex[100])
	}
	// Alive, not a placeholder: the server answered for this tab, and the
	// difference between "listed by its machine" and "filled in because its
	// machine said nothing" is exactly what the fallback would hide.
	if byIndex[100].Dead {
		t.Error("the server answered for this tab, yet it came back as dead — " +
			"its machine was not asked, and the placeholder covered for it")
	}

	// The server was actually consulted.
	if len(remote.seen()) == 0 {
		t.Error("the tab's own server was never asked for its windows")
	}
}

// A server that does not answer must not make its tabs disappear. The work is
// still running on the far side; only the connection is missing.
func TestATabSurvivesAServerThatDoesNotAnswer(t *testing.T) {
	local := &recordingExecutor{output: []byte("0:main:1:0\n")}

	SetExecutor("s2", local)
	t.Cleanup(func() { ClearExecutor("s2") })

	inst := &Instance{
		ID:     "s2",
		Status: StatusRunning,
		FollowedWindows: []FollowedWindow{
			{Index: 100, Name: "db work", Agent: AgentClaude, ServerID: "unreachable"},
		},
	}

	windows := inst.GetWindowList()

	var found *WindowInfo
	for at := range windows {
		if windows[at].Index == 100 {
			found = &windows[at]
		}
	}
	if found == nil {
		t.Fatalf("the tab vanished when its server went quiet: %+v", windows)
	}
	if !found.Dead {
		t.Error("a tab whose machine did not answer should be shown as dead, not as live")
	}
	if found.Name != "db work" {
		t.Errorf("the placeholder lost the tab's name: %+v", found)
	}
}

// Windows come back ordered by index whichever machine answered first, so the
// bar does not reshuffle between polls.
func TestWindowsAreOrderedByIndex(t *testing.T) {
	local := &recordingExecutor{output: []byte("2:second:0:0\n0:first:1:0\n")}
	SetExecutor("s3", local)
	t.Cleanup(func() { ClearExecutor("s3") })

	inst := &Instance{
		ID:              "s3",
		Status:          StatusRunning,
		FollowedWindows: []FollowedWindow{{Index: 100, Name: "remote", ServerID: "srv9"}},
	}

	windows := inst.GetWindowList()
	for at := 1; at < len(windows); at++ {
		if windows[at-1].Index > windows[at].Index {
			t.Fatalf("windows are out of order: %+v", windows)
		}
	}
}

// The bug this covers: a session's own windows disappeared from the tab bar as
// soon as it had a tab on a server.
//
// The multiplexer session on the server is created with a placeholder window
// to keep it alive, and that window belongs to no tab. Listed alongside the
// session's own windows it collided on index 0 — two machines, same number —
// so the server's idle shell replaced the session's agent in the bar, and the
// real tabs were left with no listing of their own and drawn as dead.
func TestAServersPlaceholderWindowDoesNotDisplaceTheSessionsOwn(t *testing.T) {
	own := &recordingExecutor{output: []byte("0:claude:1:0\n1:cmd:0:0\n")}
	server := &recordingExecutor{output: []byte("0:bash:0:0\n101:db work:1:0\n")}

	SetExecutor("s4", own)
	SetTabExecutor("s4", "srvB", server)
	t.Cleanup(func() {
		ClearExecutor("s4")
		ClearTabExecutor("s4", "srvB")
	})

	inst := &Instance{
		ID:       "s4",
		Status:   StatusRunning,
		ServerID: "srvA",
		FollowedWindows: []FollowedWindow{
			{Index: 1, Name: "cmd"},
			{Index: 101, Name: "db work", ServerID: "srvB"},
		},
	}

	byIndex := make(map[int]WindowInfo)
	for _, window := range inst.GetWindowList() {
		if existing, clash := byIndex[window.Index]; clash {
			t.Fatalf("two machines both claimed index %d: %q and %q",
				window.Index, existing.Name, window.Name)
		}
		byIndex[window.Index] = window
	}

	if byIndex[0].Name != "claude" {
		t.Errorf("the session's own main window was replaced by %+v", byIndex[0])
	}
	if byIndex[1].Dead {
		t.Error("a live local tab was marked dead")
	}
	if byIndex[101].Name != "db work" || byIndex[101].Dead {
		t.Errorf("the tab on the server came back as %+v", byIndex[101])
	}
}

// A tab whose window is gone is rebuilt rather than refused.
//
// The descriptor still holds everything needed — agent, arguments, directory,
// the conversation to resume — so refusing with "window not found" left the
// user with a tab they could see, could not start, and had to delete. Windows
// go missing for ordinary reasons: killed on the server, or created before the
// multiplexer was told to keep dead panes.
func TestARestartRebuildsATabWhoseWindowIsGone(t *testing.T) {
	// list-windows answers with the main window only, so the tab's own window
	// is missing; new-window then reports the index it was given.
	executor := newScriptedExecutor()
	// The main window only: the tab's own window is missing. new-window then
	// reports the index it was given.
	executor.answers["list-windows"] = "0\n"
	executor.answers["new-window"] = "3\n"

	SetExecutor("s5", executor)
	t.Cleanup(func() { ClearExecutor("s5") })

	inst := &Instance{
		ID:       "s5",
		Status:   StatusRunning,
		Path:     "/work",
		ServerID: "srvA",
		Agent:    AgentClaude,
		FollowedWindows: []FollowedWindow{
			{Index: 3, Name: "db work", Agent: AgentTerminal},
		},
	}

	if err := inst.RestartWindowWithResume(3, ""); err != nil {
		t.Fatalf("restarting a tab whose window is gone failed: %v", err)
	}

	if len(executor.commandsNamed("new-window")) == 0 {
		t.Error("the missing window was not recreated")
	}
	if len(executor.commandsNamed("respawn-pane")) != 0 {
		t.Error("a missing window was respawned rather than created")
	}
}

// A tab on this computer is always reachable; one on a server is reachable
// only while its route exists.
func TestReachabilityFollowsTheRoute(t *testing.T) {
	inst := &Instance{
		ID: "s6",
		FollowedWindows: []FollowedWindow{
			{Index: 1},
			{Index: 100, ServerID: "srvX"},
		},
	}

	if !inst.WindowReachable(1) {
		t.Error("a tab on this computer was reported unreachable")
	}
	if inst.WindowReachable(100) {
		t.Error("a tab was reported reachable with no route to its server")
	}

	SetTabExecutor("s6", "srvX", &recordingExecutor{})
	t.Cleanup(func() { ClearTabExecutor("s6", "srvX") })

	if !inst.WindowReachable(100) {
		t.Error("a tab with an open route was still reported unreachable")
	}
}

// shellScriptedExecutor answers RunShell as well, for the agent-presence check.
type shellScriptedExecutor struct {
	scriptedExecutor
	shellExit int
	// foundIn is the directory the off-PATH search should report, if any.
	foundIn string
	// userID is what `id -u` answers; "0" is root.
	userID string
}

func (s *shellScriptedExecutor) RunShell(ctx context.Context, dir string, args ...string) ([]byte, []byte, int, error) {
	// `id -u` decides whether the server logs in as root.
	if len(args) == 2 && args[0] == "id" && args[1] == "-u" {
		return []byte(s.userID + "\n"), nil, 0, nil
	}
	// The off-PATH search runs `sh -c "[ -x DIR/cmd ] && echo DIR"`; answer for
	// whichever directory the test placed the command in.
	if len(args) == 3 && args[0] == "sh" && s.foundIn != "" &&
		strings.Contains(args[2], s.foundIn) {
		return []byte(s.foundIn + "\n"), nil, 0, nil
	}
	if len(args) == 3 && args[0] == "sh" {
		// No directory was planted, so the answer is whatever the test set:
		// this same shape serves the off-PATH search and the conversation
		// probe.
		return nil, nil, s.shellExit, nil
	}
	return nil, nil, s.shellExit, nil
}

// An agent that is not on the server's PATH must be reported before the tab is
// created.
//
// Otherwise the window is made, the command is not found, the pane exits at
// once, and the multiplexer replaces the shell's "command not found" with the
// words "Pane is dead" — so the tab shows a failure with its only explanation
// erased.
func TestAMissingAgentOnTheServerIsReportedBeforeTheTabIsMade(t *testing.T) {
	executor := &shellScriptedExecutor{shellExit: 1} // command -v finds nothing
	executor.answers = map[string]string{}
	executor.failWith = map[string]error{}

	SetTabExecutor("s7", "srvX", executor)
	t.Cleanup(func() { ClearTabExecutor("s7", "srvX") })

	inst := &Instance{ID: "s7", Status: StatusRunning, Path: "/work"}

	_, err := inst.NewAgentWindowOn("srvX", "claude tab", AgentClaude, "", "", "/srv/work")
	if err == nil {
		t.Fatal("a tab was created for an agent the server does not have")
	}
	// Reported as a translation key so the user reads it in their own
	// language; the frontend resolves it and fills in the values.
	if !strings.HasPrefix(err.Error(), "error.agentNotOnServerPath|") {
		t.Errorf("the error is not a translation key: %v", err)
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("the error does not name the agent: %v", err)
	}

	// And no window was made.
	if len(executor.commandsNamed("new-window")) != 0 {
		t.Error("the window was created despite the missing agent")
	}
}

// An agent that IS there must not be blocked by the check.
func TestAnAgentPresentOnTheServerStartsNormally(t *testing.T) {
	executor := &shellScriptedExecutor{shellExit: 0} // command -v finds it
	executor.answers = map[string]string{"new-window": "100\n"}
	executor.failWith = map[string]error{}

	SetTabExecutor("s8", "srvX", executor)
	t.Cleanup(func() { ClearTabExecutor("s8", "srvX") })

	inst := &Instance{ID: "s8", Status: StatusRunning, Path: "/work"}

	if _, err := inst.NewAgentWindowOn("srvX", "claude tab", AgentClaude, "", "", "/srv/work"); err != nil {
		t.Fatalf("an agent that is installed was refused: %v", err)
	}
	if len(executor.commandsNamed("new-window")) == 0 {
		t.Error("no window was created for an agent that is present")
	}
}

// An agent that is installed but invisible to the server's PATH must be named
// with its directory.
//
// The user is looking at this error, not at the server manager. "not on the
// PATH — add the directory in the settings" describes the problem; naming the
// directory hands them the answer.
func TestTheErrorNamesWhereTheAgentActuallyIs(t *testing.T) {
	executor := &shellScriptedExecutor{shellExit: 1, foundIn: "$HOME/.local/bin"}
	executor.answers = map[string]string{}
	executor.failWith = map[string]error{}

	SetTabExecutor("s9", "srvX", executor)
	t.Cleanup(func() { ClearTabExecutor("s9", "srvX") })

	inst := &Instance{ID: "s9", Status: StatusRunning, Path: "/work"}

	_, err := inst.NewAgentWindowOn("srvX", "claude tab", AgentClaude, "", "", "/srv/work")
	if err == nil {
		t.Fatal("a tab was created for an agent the server cannot run")
	}
	if !strings.HasPrefix(err.Error(), "error.agentFoundOffPath|") {
		t.Errorf("the error is not a translation key: %v", err)
	}
	// The values the message needs travel with the key, so the text itself
	// can be written in any language.
	if !strings.Contains(err.Error(), ".local/bin") {
		t.Errorf("the error does not carry where the agent is: %v", err)
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("the error does not carry the agent name: %v", err)
	}
}

// Claude and Cursor refuse --dangerously-skip-permissions when the session
// logs in as root, by design: the flag removes the confirmations that stop an
// agent doing damage, and root is where that damage is unbounded.
//
// Passed anyway, the agent prints the refusal and exits, so the tab comes back
// as a pane that died for no visible reason. Dropping the flag starts the tab.
func TestAutoYesIsDroppedWhenTheServerLogsInAsRoot(t *testing.T) {
	executor := &shellScriptedExecutor{shellExit: 0, userID: "0"}
	executor.answers = map[string]string{"new-window": "100\n"}
	executor.failWith = map[string]error{}

	SetTabExecutor("s10", "srvX", executor)
	t.Cleanup(func() { ClearTabExecutor("s10", "srvX") })

	inst := &Instance{ID: "s10", Status: StatusRunning, Path: "/work", AutoYes: true}

	if _, err := inst.NewAgentWindowOn("srvX", "claude tab", AgentClaude, "", "", "/srv/work"); err != nil {
		t.Fatalf("the tab was not created: %v", err)
	}

	for _, command := range executor.commandsNamed("new-window") {
		for _, arg := range command {
			if strings.Contains(arg, "dangerously-skip-permissions") {
				t.Fatalf("the flag the agent refuses as root was passed anyway: %v", command)
			}
		}
	}
}

// A server that does not log in as root keeps the flag: dropping it silently
// would take away something the user asked for.
func TestAutoYesSurvivesOnANonRootServer(t *testing.T) {
	executor := &shellScriptedExecutor{shellExit: 0, userID: "1000"}
	executor.answers = map[string]string{"new-window": "100\n"}
	executor.failWith = map[string]error{}

	SetTabExecutor("s11", "srvX", executor)
	t.Cleanup(func() { ClearTabExecutor("s11", "srvX") })

	inst := &Instance{ID: "s11", Status: StatusRunning, Path: "/work", AutoYes: true}

	if _, err := inst.NewAgentWindowOn("srvX", "claude tab", AgentClaude, "", "", "/srv/work"); err != nil {
		t.Fatalf("the tab was not created: %v", err)
	}

	var passed bool
	for _, command := range executor.commandsNamed("new-window") {
		for _, arg := range command {
			if strings.Contains(arg, "dangerously-skip-permissions") {
				passed = true
			}
		}
	}
	if !passed {
		t.Error("auto-yes was dropped on a server that would have accepted it")
	}
}

// A conversation has to exist on the machine the tab runs on.
//
// The local check reads this computer's ~/.claude, which for a tab on a server
// is the wrong disk: an id from here does not exist there, the agent is asked
// to resume it anyway, and answers "no conversation found" instead of running.
func TestAConversationIsLookedForWhereTheTabRuns(t *testing.T) {
	// The probe fails: the conversation is not on the server.
	absent := &shellScriptedExecutor{shellExit: 1}
	absent.answers = map[string]string{}
	absent.failWith = map[string]error{}

	SetTabExecutor("s12", "srvX", absent)
	t.Cleanup(func() { ClearTabExecutor("s12", "srvX") })

	inst := &Instance{ID: "s12"}

	if inst.resumeIDExistsOnServer("srvX", AgentClaude, "937b76f9-0fa2-40c3-b5df-8f29faf0a7a9") {
		t.Error("a conversation absent from the server was reported as present")
	}

	// And when it is there, it is used.
	present := &shellScriptedExecutor{shellExit: 0}
	present.answers = map[string]string{}
	present.failWith = map[string]error{}

	SetTabExecutor("s13", "srvY", present)
	t.Cleanup(func() { ClearTabExecutor("s13", "srvY") })

	other := &Instance{ID: "s13"}
	if !other.resumeIDExistsOnServer("srvY", AgentClaude, "937b76f9-0fa2-40c3-b5df-8f29faf0a7a9") {
		t.Error("a conversation present on the server was reported as missing")
	}
}

// An id that could not be checked is left alone: the agent decides, as it does
// locally. Refusing on a failed check would throw away a usable conversation.
func TestAnUncheckableConversationIsLeftToTheAgent(t *testing.T) {
	inst := &Instance{ID: "s14"}

	// No route to that server at all.
	if !inst.resumeIDExistsOnServer("srvZ", AgentClaude, "some-id") {
		t.Error("an unreachable server was taken as proof the conversation is gone")
	}
	// An agent whose storage layout is not known here.
	if !inst.resumeIDExistsOnServer("srvZ", AgentAider, "some-id") {
		t.Error("an agent we cannot check for was refused")
	}
}

// A syntactically impossible id never reaches a command line.
func TestAnUnsafeConversationIdIsRefused(t *testing.T) {
	inst := &Instance{ID: "s15"}
	if inst.resumeIDExistsOnServer("srvZ", AgentClaude, "../../etc/passwd") {
		t.Error("an unsafe id was accepted")
	}
}

// The status line for a tab on a server never moved.
//
// Two reasons, both the same mistake: the liveness gate asked whether the
// SESSION's machine had the multiplexer session — which for a local session
// with a remote tab is this computer, where it does not exist — and the pane
// capture went through the session's executor rather than the tab's. So the
// reading was skipped before it began, and would have read the wrong machine
// if it had not been.
func TestAStatusLineIsReadFromTheTabsOwnMachine(t *testing.T) {
	// The session runs here; the tab is on a server.
	server := &recordingExecutor{output: []byte("· Thinking… (5s)\n")}

	SetTabExecutor("s16", "srvX", server)
	t.Cleanup(func() { ClearTabExecutor("s16", "srvX") })

	inst := &Instance{
		ID:     "s16",
		Status: StatusRunning,
		FollowedWindows: []FollowedWindow{
			{Index: 100, Name: "db work", Agent: AgentClaude, ServerID: "srvX"},
		},
	}

	inst.GetStatusInfoForWindowContext(context.Background(), 100, AgentClaude)

	var askedHasSession, askedCapture bool
	for _, command := range server.seen() {
		switch command[0] {
		case "has-session":
			askedHasSession = true
		case "capture-pane":
			askedCapture = true
		}
	}
	if !askedHasSession {
		t.Error("the tab's own machine was not asked whether it is running")
	}
	if !askedCapture {
		t.Error("the pane was not captured from the machine the tab runs on")
	}
}

// A tab on a server has no local mirror session to look for: mirrors are made
// by the terminal handler on this computer, and a remote tab attaches over SSH
// without one. Searching for one costs a round trip per poll and finds
// nothing.
func TestNoMirrorIsSearchedForOnAServer(t *testing.T) {
	server := &recordingExecutor{}

	SetTabExecutor("s17", "srvX", server)
	t.Cleanup(func() { ClearTabExecutor("s17", "srvX") })

	inst := &Instance{
		ID:              "s17",
		Status:          StatusRunning,
		FollowedWindows: []FollowedWindow{{Index: 100, ServerID: "srvX"}},
	}

	target := inst.GetCaptureTargetContext(context.Background(), 100)

	if target != "s17:100" {
		t.Errorf("capture target = %q, want the tab's own window", target)
	}
	for _, command := range server.seen() {
		if command[0] == "list-sessions" {
			t.Error("a mirror session was searched for on the server, where none exists")
		}
	}
}

// A view session exists only to show one window. Left behind when the session
// it belonged to is stopped, it lives on in `tmux ls` on the server for as
// long as the machine stays up — an orphan nobody will ever look at again.
//
// Two shapes have to be recognised: the local mirrors the terminal handler
// makes, and the ones a remote attach makes on the server.
func TestStoppingASessionRemovesItsViewsOnEveryMachine(t *testing.T) {
	local := &scriptedExecutor{
		answers:  map[string]string{"list-sessions": "asm_x\nasm_x_gui_0_123\nsomething_else\n"},
		failWith: map[string]error{},
	}
	server := &scriptedExecutor{
		answers:  map[string]string{"list-sessions": "asm_x\nasmgr_view_asm_x_100\nother_users_work\n"},
		failWith: map[string]error{},
	}

	SetExecutor("asm_x", local)
	SetTabExecutor("asm_x", "srvX", server)
	t.Cleanup(func() {
		ClearExecutor("asm_x")
		ClearTabExecutor("asm_x", "srvX")
	})

	inst := &Instance{
		ID:              "asm_x",
		Status:          StatusRunning,
		FollowedWindows: []FollowedWindow{{Index: 100, ServerID: "srvX"}},
	}

	_ = inst.StopContext(context.Background())

	killedOn := func(ex *scriptedExecutor) []string {
		var names []string
		for _, command := range ex.commandsNamed("kill-session") {
			names = append(names, command[len(command)-1])
		}
		return names
	}

	localKills := killedOn(local)
	if !contains(localKills, "asm_x_gui_0_123") {
		t.Errorf("the local mirror was left behind: %v", localKills)
	}

	serverKills := killedOn(server)
	if !contains(serverKills, "asmgr_view_asm_x_100") {
		t.Errorf("the view on the server was left behind: %v", serverKills)
	}
	// Somebody else's session on the same server must never be touched.
	if contains(serverKills, "other_users_work") {
		t.Error("cleanup killed a session that was not ours")
	}
}


// The name a remote attach builds and the prefix the cleanup looks for have to
// agree, or the views are created by one side and never found by the other.
func TestTheViewPrefixMatchesWhatARemoteAttachCreates(t *testing.T) {
	// remote.mirrorNameFor("asm_x:100") produces this; kept in step by hand,
	// so this is the test that catches them drifting apart.
	const whatRemoteCreates = "asmgr_view_asm_x_100"

	prefix := remoteViewPrefix("asm_x")
	if !strings.HasPrefix(whatRemoteCreates, prefix) {
		t.Errorf("cleanup looks for %q, which does not match %q",
			prefix, whatRemoteCreates)
	}
}

// Starting a session on a server must not depend on this computer having a
// multiplexer.
//
// The check exists so a user without tmux is told so plainly rather than
// meeting "exec: no such file" once per command. But it asks about THIS
// machine, and a remote session uses the multiplexer on the server — checked
// by the connection test, and the whole reason for running it there. Refusing
// to start over a program that is never used is refusing for no reason.
//
// Found on a macOS build runner, which has no tmux: every remote lifecycle
// test failed there while passing everywhere tmux happens to be installed.
func TestARemoteSessionDoesNotNeedALocalMultiplexer(t *testing.T) {
	source, err := os.ReadFile("instance.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	at := strings.Index(text, "func (i *Instance) startWithResume(")
	if at < 0 {
		t.Fatal("startWithResume is gone; this test needs rewriting")
	}
	end := strings.Index(text[at:], "\n}\n")
	body := text[at : at+end]

	checkAt := strings.Index(body, "CheckMultiplexer()")
	if checkAt < 0 {
		t.Fatal("the multiplexer check is gone; a user without tmux will meet " +
			"one failure per command instead of being told once")
	}
	if !strings.Contains(body[:checkAt], "!i.IsRemote()") {
		t.Error("a session on a server is refused when THIS computer has no " +
			"multiplexer, which is not the one it would use")
	}
}
