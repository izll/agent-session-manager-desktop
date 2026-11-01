package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
	"asmgr-desktop/tokenusage"
	"asmgr-desktop/tokenusage/store"
)

const usageConversation = "aaaaaaaa-0000-0000-0000-000000000001"
const usageConversationOld = "bbbbbbbb-0000-0000-0000-000000000002"

// tokenUsageTestApp points the agents' log directories at a temporary home, so
// a scan can never read the real ones.
func tokenUsageTestApp(t *testing.T) (*App, string) {
	t.Helper()
	storage := guardedTestStorage(t) // moves HOME itself
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	app := &App{storage: storage, projectLocked: true, servers: newServerPool()}
	t.Cleanup(app.stopTokenUsage)
	return app, home
}

func writeClaudeUsage(t *testing.T, home, conversation, cwd string, output int64) {
	t.Helper()
	full := filepath.Join(home, ".claude", "projects", "-work", conversation+".jsonl")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"assistant","requestId":"r-%s","timestamp":"2026-10-20T10:00:00Z","cwd":%q,`+
		`"message":{"id":"m-%s","model":"claude-opus-5-5","usage":{"input_tokens":1,"output_tokens":%d}}}`+"\n",
		conversation, cwd, conversation, output)
	if err := os.WriteFile(full, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scanTokenUsageNow(t *testing.T, app *App) {
	t.Helper()
	app.scanTokenUsage(context.Background(), false)
}

func findUsageItem(t *testing.T, report *TokenUsageReport, conversation string) TokenUsageItem {
	t.Helper()
	for _, item := range report.Items {
		if item.Conversation == conversation {
			return item
		}
	}
	t.Fatalf("no item for %s in %+v", conversation, report.Items)
	return TokenUsageItem{}
}

func TestTheReportNamesTheSessionHoldingTheConversation(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	inst := &session.Instance{ID: "s1", Name: "API", Path: "/work/api", ResumeSessionID: usageConversation}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	writeClaudeUsage(t, home, usageConversation, "/work/api/sub", 42)
	scanTokenUsageNow(t, app)

	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	item := findUsageItem(t, report, usageConversation)
	if item.SessionID != "s1" || item.SessionName != "API" || item.Output != 42 || item.Machine != store.LocalMachine {
		t.Fatalf("item = %+v", item)
	}
}

// Found on a real machine: a tab of pbx-go, unused for weeks, stored the id of
// WebERP's largest conversation and took the credit for all of it. The log says
// where a conversation really ran, and a stored id that disagrees is not believed.
func TestAStoredIDFromAnotherDirectoryIsNotBelieved(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	for _, inst := range []*session.Instance{
		{ID: "pbx", Name: "pbx-go", Path: "/work/pbx-go", FollowedWindows: []session.FollowedWindow{{Index: 1, Name: "claude tab", ResumeSessionID: usageConversation}}},
		{ID: "erp", Name: "WebErp", Path: "/work/weberp"},
	} {
		if err := app.storage.AddInstance(inst); err != nil {
			t.Fatal(err)
		}
	}
	writeClaudeUsage(t, home, usageConversation, "/work/weberp", 42)
	scanTokenUsageNow(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversation); item.SessionName != "WebErp" {
		t.Fatalf("credited to %q: %+v", item.SessionName, item)
	}
}

// Seen here too: two sessions storing one id, and the one where it really ran wins
// whichever came first.
func TestOfTwoClaimsTheOneInTheRightDirectoryWins(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	for _, inst := range []*session.Instance{
		{ID: "a", Name: "a first", Path: "/work/a", ResumeSessionID: usageConversation},
		{ID: "z", Name: "z last", Path: "/work/z", ResumeSessionID: usageConversation},
		// A second session in the directory, so only the claim can decide.
		{ID: "z2", Name: "z other", Path: "/work/z"},
	} {
		if err := app.storage.AddInstance(inst); err != nil {
			t.Fatal(err)
		}
	}
	writeClaudeUsage(t, home, usageConversation, "/work/z", 42)
	scanTokenUsageNow(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversation); item.SessionID != "z" {
		t.Fatalf("credited to %q: %+v", item.SessionName, item)
	}
}

// An unrelated directory with no claim and no session reads as what it is.
func TestAWrongClaimWithNoRightfulOwnerLeavesItOutside(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	inst := &session.Instance{ID: "pbx", Name: "pbx-go", Path: "/work/pbx-go", ResumeSessionID: usageConversation}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	writeClaudeUsage(t, home, usageConversation, "/somewhere/else", 42)
	scanTokenUsageNow(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversation); item.SessionID != "" {
		t.Fatalf("credited to %q", item.SessionName)
	}
}

func TestATabsConversationNamesTheTab(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	inst := &session.Instance{ID: "s1", Name: "API", Path: "/work/api", FollowedWindows: []session.FollowedWindow{
		{Index: 2, Name: "review", ResumeSessionID: usageConversation, WorkDir: "/work/sparrow"},
	}}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	// The tab works elsewhere than its session, and its own directory is the test.
	writeClaudeUsage(t, home, usageConversation, "/work/sparrow", 1)
	scanTokenUsageNow(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversation); item.SessionName != "API" || item.TabName != "review" {
		t.Fatalf("item = %+v", item)
	}
}

// A session holds only its current conversation. The one before it is found by
// the directory it ran in.
func TestAnEarlierConversationIsFoundByItsDirectory(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	inst := &session.Instance{ID: "s1", Name: "API", Path: "/work/api", ResumeSessionID: usageConversation}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	writeClaudeUsage(t, home, usageConversationOld, "/work/api", 7)
	scanTokenUsageNow(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversationOld); item.SessionID != "s1" {
		t.Fatalf("item = %+v", item)
	}
}

// Its worktree is still that repository's work.
func TestAWorktreeBelongsToItsRepository(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	if err := app.storage.AddInstance(&session.Instance{ID: "s1", Name: "API", Path: "/work/api"}); err != nil {
		t.Fatal(err)
	}
	writeClaudeUsage(t, home, usageConversationOld, "/work/api/.claude/worktrees/agent-1", 7)
	scanTokenUsageNow(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversationOld); item.SessionID != "s1" {
		t.Fatalf("item = %+v", item)
	}
}

// Two sessions in one directory cannot be told apart, and a guess would be
// wrong half the time.
func TestASharedDirectoryIsNotGuessed(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	for _, id := range []string{"s1", "s2"} {
		if err := app.storage.AddInstance(&session.Instance{ID: id, Name: id, Path: "/work/api"}); err != nil {
			t.Fatal(err)
		}
	}
	writeClaudeUsage(t, home, usageConversationOld, "/work/api", 7)
	scanTokenUsageNow(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversationOld); item.SessionID != "" {
		t.Fatalf("guessed a session: %+v", item)
	}
}

// A session in the home directory must not claim everything under it.
func TestAParentDirectoryDoesNotClaimItsChildren(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	if err := app.storage.AddInstance(&session.Instance{ID: "home", Name: "home", Path: "/home/me"}); err != nil {
		t.Fatal(err)
	}
	writeClaudeUsage(t, home, usageConversationOld, "/home/me/work/api", 7)
	scanTokenUsageNow(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversationOld); item.SessionID != "" {
		t.Fatalf("the home session claimed it: %+v", item)
	}
}

// The same directory on another machine is another machine's work.
func TestADirectoryOnAServerIsNotThisMachines(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	if err := app.storage.AddInstance(&session.Instance{ID: "s1", Name: "API", Path: "/work/api", ServerID: "server-1"}); err != nil {
		t.Fatal(err)
	}
	writeClaudeUsage(t, home, usageConversationOld, "/work/api", 7)
	scanTokenUsageNow(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversationOld); item.SessionID != "" {
		t.Fatalf("attributed across machines: %+v", item)
	}
}

// Sessions of a project other than the active one are named too: the view is
// across all of them.
func TestSessionsOfOtherProjectsAreFound(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	project, err := app.storage.AddProject("Other project")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.storage.SetActiveProject(project.ID); err != nil {
		t.Fatal(err)
	}
	inst := &session.Instance{ID: "s9", Name: "Elsewhere", Path: "/work/other", ResumeSessionID: usageConversation}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	if err := app.storage.SetActiveProject(""); err != nil {
		t.Fatal(err)
	}
	writeClaudeUsage(t, home, usageConversation, "/work/other", 3)
	scanTokenUsageNow(t, app)

	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	item := findUsageItem(t, report, usageConversation)
	if item.SessionName != "Elsewhere" || item.ProjectID != project.ID || item.ProjectName != "Other project" {
		t.Fatalf("item = %+v", item)
	}
}

func TestThePeriodIsAppliedByDay(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	writeClaudeUsage(t, home, usageConversation, "/work/api", 3) // dated 2026-10-20
	scanTokenUsageNow(t, app)
	all, err := app.GetTokenUsage(0)
	if err != nil || len(all.Items) != 1 {
		t.Fatalf("all = %+v, err = %v", all, err)
	}
	// One second before now is not 2026-10-20 unless the clock says so, and the
	// test's own date is far from it; a window of one day must leave it out.
	today, err := app.GetTokenUsage(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(today.Items) != 0 {
		t.Fatalf("today = %+v", today.Items)
	}
}

// A machine that cannot be reached has its error beside its last numbers.
func TestTheReportListsMachinesWithTheirState(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	if err := app.storage.UpdateServers(func(list *session.ServerList) error {
		list.Servers = append(list.Servers, session.Server{ID: "srv-1", Name: "web", Host: "web.example", User: "me"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	usage, err := app.tokenUsageStore()
	if err != nil {
		t.Fatal(err)
	}
	app.recordTokenUsageScan(context.Background(), usage, "me@web.example", fmt.Errorf("error.couldNotReachServer|web|timeout"))

	report, err := app.GetTokenUsage(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Machines) != 2 || !report.Machines[0].Local || report.Machines[1].Name != "web" || report.Machines[1].ID != "me@web.example" ||
		!strings.Contains(report.Machines[1].Error, "couldNotReachServer") {
		t.Fatalf("machines = %+v", report.Machines)
	}
}

// A timer has no business dialling a machine nobody asked for.
func TestABackgroundScanDoesNotConnect(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	if err := app.storage.UpdateServers(func(list *session.ServerList) error {
		list.Servers = append(list.Servers, session.Server{ID: "srv-1", Name: "web", Host: "web.example", User: "me"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	scanTokenUsageNow(t, app)
	usage, err := app.tokenUsageStore()
	if err != nil {
		t.Fatal(err)
	}
	scans, err := usage.Scans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, tried := scans["me@web.example"]; tried {
		t.Fatalf("the server was tried: %+v", scans)
	}
	if _, scanned := scans[store.LocalMachine]; !scanned {
		t.Fatalf("this computer was not scanned: %+v", scans)
	}
}

func TestOnlyOneScanRunsAtATime(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	app.tokenUsage.scanning = true
	app.startTokenUsageScan(false)
	app.tokenUsage.scans.Wait()
	if !app.tokenUsage.scanning {
		t.Fatal("a second scan ran beside the first and cleared its flag")
	}
	app.tokenUsage.scanning = false
}

func TestAStoppedAppStartsNoScan(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	writeClaudeUsage(t, home, usageConversation, "/work/api", 3)
	if _, err := app.tokenUsageStore(); err != nil {
		t.Fatal(err)
	}
	app.tokenUsage.mu.Lock()
	app.tokenUsage.stopped = true
	app.tokenUsage.mu.Unlock()
	app.startTokenUsageScan(false)
	if app.tokenUsageScanning() {
		t.Fatal("a scan started during shutdown")
	}
}

// A timer scan records who holds what too: a session holds only its current
// conversation, and the view may not be opened before it moves on.
func TestAScanRemembersWhoHoldsWhat(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	inst := &session.Instance{ID: "s1", Name: "API", Path: "/work/api", ResumeSessionID: usageConversation}
	// A second session in the directory, so only the remembered id can name it.
	other := &session.Instance{ID: "s2", Name: "Other", Path: "/work/api"}
	for _, each := range []*session.Instance{inst, other} {
		if err := app.storage.AddInstance(each); err != nil {
			t.Fatal(err)
		}
	}
	writeClaudeUsage(t, home, usageConversation, "/work/api", 42)
	scanTokenUsageNow(t, app)

	inst.ResumeSessionID = usageConversationOld
	if err := app.storage.UpdateInstance(inst); err != nil {
		t.Fatal(err)
	}
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversation); item.SessionID != "s1" {
		t.Fatalf("item = %+v", item)
	}
}

// Taking a server out and adding it again gives it a new id, and its logs read
// from the top under that id would be counted twice.
func TestAServersLogsAreKeyedByWhereTheyAre(t *testing.T) {
	first := usageServerKey(&session.Server{ID: "a", User: "Me", Host: "Web.Example", Port: 22})
	again := usageServerKey(&session.Server{ID: "b", Name: "renamed", User: "me", Host: "web.example"})
	if first != again || first != "me@web.example" {
		t.Fatalf("keys = %q, %q", first, again)
	}
	if other := usageServerKey(&session.Server{User: "me", Host: "web.example", Port: 2222}); other == first {
		t.Fatalf("another port is another machine: %q", other)
	}
}

// A tab of a session on a server is matched under that server's key.
func TestADirectoryOnAServerBelongsToItsSession(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	if err := app.storage.UpdateServers(func(list *session.ServerList) error {
		list.Servers = append(list.Servers, session.Server{ID: "srv-1", Name: "web", Host: "web.example", User: "me"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.storage.AddInstance(&session.Instance{ID: "s1", Name: "API", Path: "/work/api", ServerID: "srv-1"}); err != nil {
		t.Fatal(err)
	}
	owned := app.collectTokenUsageOwners()
	if owner := owned.ownerOfDirectory("me@web.example", "/work/api"); owner == nil || owner.SessionID != "s1" {
		t.Fatalf("owner = %+v", owner)
	}
}

// Which tab cannot be told from a directory they share; which session can.
func TestTabsSharingADirectoryNameTheSessionOnly(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	inst := &session.Instance{ID: "s1", Name: "API", Path: "/work/api", FollowedWindows: []session.FollowedWindow{
		{Index: 1, Name: "one", WorkDir: "/work/tabs"},
		{Index: 2, Name: "two", WorkDir: "/work/tabs"},
	}}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	owner := app.collectTokenUsageOwners().ownerOfDirectory(store.LocalMachine, "/work/tabs")
	if owner == nil || owner.SessionID != "s1" || owner.TabName != "" {
		t.Fatalf("owner = %+v", owner)
	}
}

// Storage that failed to open at startup must not take the app down when the
// view is opened.
func TestTheViewSurvivesMissingStorage(t *testing.T) {
	app := &App{servers: newServerPool()}
	if _, err := app.GetTokenUsage(7); err == nil {
		t.Fatal("no error without storage")
	}
	app.scanTokenUsage(context.Background(), true)
}

// A failure to open is tried again next time rather than remembered.
func TestAFailureToOpenIsNotPermanent(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	path := app.storage.TokenUsagePath()
	if err := os.MkdirAll(path, 0o755); err != nil { // a directory where the file goes
		t.Fatal(err)
	}
	if _, err := app.tokenUsageStore(); err == nil {
		t.Fatal("opened a directory as a database")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := app.tokenUsageStore(); err != nil {
		t.Fatalf("still failing after the cause went: %v", err)
	}
}

// A scan that will not stop must not hold the app open.
func TestStoppingDoesNotWaitForeverForAScan(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	app.tokenUsage.scans.Add(1)
	defer app.tokenUsage.scans.Done()
	finished := make(chan struct{})
	go func() {
		app.stopTokenUsage()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(stopTokenUsageWait + 5*time.Second):
		t.Fatal("shutdown waited for a scan that never ends")
	}
}

func addUsageServer(t *testing.T, app *App) {
	t.Helper()
	if err := app.storage.UpdateServers(func(list *session.ServerList) error {
		list.Servers = append(list.Servers, session.Server{ID: "srv-1", Name: "web", Host: "web.example", User: "me"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func machineInReport(t *testing.T, report *TokenUsageReport, id string) TokenUsageMachine {
	t.Helper()
	for _, machine := range report.Machines {
		if machine.ID == id {
			return machine
		}
	}
	t.Fatalf("no machine %s in %+v", id, report.Machines)
	return TokenUsageMachine{}
}

func TestMachinesAreOnUntilTurnedOff(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	addUsageServer(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if !machineInReport(t, report, store.LocalMachine).Enabled || !machineInReport(t, report, "me@web.example").Enabled {
		t.Fatalf("machines = %+v", report.Machines)
	}
}

// Off means not read: this computer's new logs are left alone, and what was
// already read comes back with it.
func TestAMachineTurnedOffIsNotReadOrCounted(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	writeClaudeUsage(t, home, usageConversation, "/work/api", 5)
	scanTokenUsageNow(t, app)

	if err := app.SetTokenUsageMachineEnabled(store.LocalMachine, false); err != nil {
		t.Fatal(err)
	}
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Items) != 0 || machineInReport(t, report, store.LocalMachine).Enabled {
		t.Fatalf("a machine turned off is still shown: %+v", report)
	}

	writeClaudeUsage(t, home, usageConversationOld, "/work/api", 7)
	scanTokenUsageNow(t, app)
	if err := app.SetTokenUsageMachineEnabled(store.LocalMachine, true); err != nil {
		t.Fatal(err)
	}
	report, err = app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Items) != 1 || report.Items[0].Conversation != usageConversation {
		t.Fatalf("read while turned off, or lost what was read: %+v", report.Items)
	}

	scanTokenUsageNow(t, app)
	report, err = app.GetTokenUsage(0)
	if err != nil || len(report.Items) != 2 {
		t.Fatalf("turned back on, it did not carry on: %+v, %v", report, err)
	}
}

// Connecting installs the helper, so a machine turned off must not even be tried,
// by a scan that was asked to connect.
func TestAServerTurnedOffIsNotConnectedTo(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	addUsageServer(t, app)
	if err := app.SetTokenUsageMachineEnabled("me@web.example", false); err != nil {
		t.Fatal(err)
	}
	app.scanTokenUsage(context.Background(), true)
	usage, err := app.tokenUsageStore()
	if err != nil {
		t.Fatal(err)
	}
	scans, err := usage.Scans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if scan, tried := scans["me@web.example"]; tried {
		t.Fatalf("the server was tried: %+v", scan)
	}
}

// slowUsageSource holds a read open until told, as a first read of a big machine
// does for minutes.
type slowUsageSource struct {
	listed  chan struct{}
	release chan struct{}
}

func (s slowUsageSource) List(ctx context.Context) ([]tokenusage.FileInfo, error) {
	close(s.listed)
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return nil, nil
}

func (s slowUsageSource) Read(context.Context, []tokenusage.ReadRequest) ([]tokenusage.FileResult, error) {
	return nil, nil
}

// A first read can take minutes, and that is when someone turns it off.
func TestTurningAMachineOffStopsItsRead(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	usage, err := app.tokenUsageStore()
	if err != nil {
		t.Fatal(err)
	}
	source := slowUsageSource{listed: make(chan struct{}), release: make(chan struct{})}
	defer close(source.release)
	finished := make(chan struct{})
	go func() {
		app.scanTokenUsageMachine(context.Background(), usage, "me@web.example", source)
		close(finished)
	}()
	<-source.listed
	if err := app.SetTokenUsageMachineEnabled("me@web.example", false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the read went on after the machine was turned off")
	}
	scans, err := usage.Scans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if scan, recorded := scans["me@web.example"]; recorded {
		t.Fatalf("being turned off was recorded as a fault: %+v", scan)
	}
}

// Its numbers are still in the totals, so it has to stay something to turn off.
func TestAServerNoLongerListedCanStillBeTurnedOff(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	usage, err := app.tokenUsageStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := usage.RecordScan(context.Background(), "me@gone.example", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if machine := machineInReport(t, report, "me@gone.example"); machine.Name != "me@gone.example" || !machine.Enabled {
		t.Fatalf("machine = %+v", machine)
	}
}

// Remembered from a server, so a conversation running there reads as the session
// it belongs to rather than as a directory. The case that started this: a tab of
// MADisoCAD_v1 working in the sparrow directory, seen from another computer.
func TestAServersSessionNamesReachTheReport(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	usage, err := app.tokenUsageStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const machine = "me@web.example"
	if err := usage.RememberOwners(ctx, []store.Owner{{Machine: machine, Conversation: usageConversation, Directory: "/work/sparrow", ProjectID: machine + "/", SessionID: machine + "/s1", SessionName: "MADisoCAD_v1", TabName: "claude tab"}}); err != nil {
		t.Fatal(err)
	}
	if err := usage.ReplaceDirectoryOwners(ctx, machine, []store.DirectoryOwner{{Machine: machine, Directory: "/work/older", Owner: store.Owner{ProjectID: machine + "/", SessionID: machine + "/s2", SessionName: "Older"}}}); err != nil {
		t.Fatal(err)
	}
	row := func(id, cwd string) tokenusage.FileResult {
		return tokenusage.FileResult{Agent: "claude", Conversation: id, Cursor: tokenusage.Cursor{Offset: 1}, Rows: []tokenusage.Row{{Key: 1 + uint64(len(cwd)), Unix: 1_790_000_000, Model: "m", Cwd: cwd, Output: 9}}}
	}
	if err := usage.Apply(ctx, machine, []store.Applied{
		{FileID: "claude:a", Size: 1, Result: row(usageConversation, "/work/sparrow")},
		{FileID: "claude:b", Size: 1, Result: row(usageConversationOld, "/work/older")},
	}); err != nil {
		t.Fatal(err)
	}
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversation); item.SessionName != "MADisoCAD_v1" || item.TabName != "claude tab" {
		t.Fatalf("named conversation: %+v", item)
	}
	if item := findUsageItem(t, report, usageConversationOld); item.SessionName != "Older" {
		t.Fatalf("conversation found by directory: %+v", item)
	}
}

// A Codex run started from a Claude session's scratchpad is that session's work,
// and its path says which: seen here as dozens of unrelated /tmp rows.
func TestAScratchpadConversationBelongsToItsParent(t *testing.T) {
	const parent = "27632e09-68b7-4baf-92e6-eccba6e8cede"
	for directory, want := range map[string]string{
		"/tmp/claude-1000/-home-me-sparrow/" + parent + "/scratchpad/codex-img": parent,
		"/tmp/claude-1000/-home-me-sparrow/" + parent:                           parent,
		"/home/me/" + parent + "/scratchpad":                                    "",
		"/tmp/claude-1000/-home-me-sparrow/not-a-conversation/scratchpad":       "",
		"/work/api": "",
		"":          "",
	} {
		if got := conversationOfScratchpad(directory); got != want {
			t.Errorf("%q: got %q, want %q", directory, got, want)
		}
	}

	app, home := tokenUsageTestApp(t)
	inst := &session.Instance{ID: "s1", Name: "MADisoCAD_v1", Path: "/work/mad", ResumeSessionID: parent}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	writeClaudeUsage(t, home, parent, "/work/mad", 1)
	writeClaudeUsage(t, home, usageConversationOld, "/tmp/claude-1000/-home-me-sparrow/"+parent+"/scratchpad/codex-img", 7)
	scanTokenUsageNow(t, app)
	report, err := app.GetTokenUsage(0)
	if err != nil {
		t.Fatal(err)
	}
	if item := findUsageItem(t, report, usageConversationOld); item.SessionName != "MADisoCAD_v1" {
		t.Fatalf("item = %+v", item)
	}
}

func TestAProjectLinkReachesTheReport(t *testing.T) {
	app, _ := tokenUsageTestApp(t)
	if err := app.SetTokenUsageProject("session:s1", "  Sparrow  "); err != nil {
		t.Fatal(err)
	}
	report, err := app.GetTokenUsage(7)
	if err != nil {
		t.Fatal(err)
	}
	if report.Links["session:s1"] != "Sparrow" {
		t.Fatalf("links = %+v", report.Links)
	}
	if err := app.SetTokenUsageProject("session:s1", " "); err != nil {
		t.Fatal(err)
	}
	report, err = app.GetTokenUsage(7)
	if err != nil {
		t.Fatal(err)
	}
	if _, linked := report.Links["session:s1"]; linked {
		t.Fatalf("a blank name did not remove the link: %+v", report.Links)
	}
}

// Wiring that only the running app exercises.
func TestTokenUsageIsStartedAndStoppedWithTheApp(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(functionBody(t, string(source), "func (a *App) startup("), "a.startTokenUsageWatcher()") {
		t.Error("startup does not start the usage timer")
	}
	if !strings.Contains(functionBody(t, string(source), "func (a *App) shutdown("), "a.stopTokenUsage()") {
		t.Error("shutdown does not stop the usage scan")
	}

	// Without this a conversation on a server reads as its directory: the names
	// live on that machine.
	api, err := os.ReadFile("token_usage_api.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(functionBody(t, string(api), "func (a *App) scanTokenUsage("), "a.rememberServerOwners(ctx, usage, machine, source)") {
		t.Error("a scan does not ask a server for its session names")
	}
}
