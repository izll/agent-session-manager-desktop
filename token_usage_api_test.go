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
	writeClaudeUsage(t, home, usageConversation, "/somewhere/else", 42)
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

func TestATabsConversationNamesTheTab(t *testing.T) {
	app, home := tokenUsageTestApp(t)
	inst := &session.Instance{ID: "s1", Name: "API", Path: "/work/api", FollowedWindows: []session.FollowedWindow{
		{Index: 2, Name: "review", ResumeSessionID: usageConversation},
	}}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	writeClaudeUsage(t, home, usageConversation, "/work/api", 1)
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
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	writeClaudeUsage(t, home, usageConversation, "/somewhere/else", 42)
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
}
