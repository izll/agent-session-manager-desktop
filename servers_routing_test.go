package main

import (
	"os"
	"strings"
	"testing"
)

// The freeze this prevents: every panel in the window stopped responding.
//
// Routing runs inside Storage.GetInstance, under the storage lock that the
// sidebar poller, the tab bar and every terminal attach also go through.
// Building an SSH connection there held that lock for as long as the server
// took to answer — and for the full dial timeout when it did not answer at
// all, which is what froze the app.
//
// The rule this keeps: the routing called from session loading may use
// connections that already exist, and may ask for missing ones in the
// background, but it must not dial.
func TestSessionRoutingDoesNotDialUnderTheStorageLock(t *testing.T) {
	source, err := os.ReadFile("servers_pool.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	body := functionBody(t, text, "func (a *App) routeSessionCommands(")
	if strings.Contains(body, "a.connectionFor(") {
		t.Error("routeSessionCommands dials; it runs under the storage lock and " +
			"will freeze every panel while the server is slow or unreachable")
	}

	// The non-blocking path has to actually route, or a session on a server
	// would never reach it.
	ready := functionBody(t, text, "func (a *App) routeUsingReadyConnections(")
	if !strings.Contains(ready, "a.servers.get(") {
		t.Error("routing no longer uses the connections already open")
	}
	if strings.Contains(ready, "a.connectionFor(") {
		t.Error("the non-blocking routing dials after all")
	}
}

// Background dials are deduplicated. Routing runs once per session on every
// sidebar poll, so without this a single unreachable server would accumulate
// one goroutine per session per pass, each holding a dial timeout.
func TestBackgroundConnectsAreDeduplicated(t *testing.T) {
	source, err := os.ReadFile("servers_pool.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	body := functionBody(t, text, "func (a *App) connectInBackground(")
	if !strings.Contains(body, "LoadOrStore") {
		t.Error("connectInBackground does not deduplicate; an unreachable server " +
			"would collect a dial per session per poll")
	}
	if !strings.Contains(body, "go func()") {
		t.Error("connectInBackground is not actually off the caller's goroutine")
	}
}

// functionBody returns the source between a function's opening line and the
// closing brace at column zero.
func functionBody(t *testing.T, source, signature string) string {
	t.Helper()
	at := strings.Index(source, signature)
	if at < 0 {
		t.Fatalf("%s is gone; this test needs rewriting", signature)
	}
	rest := source[at:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("could not find the end of %s", signature)
	}
	return rest[:end]
}

// Building a connection must not happen while the project mutation lock is
// held. That lock is exclusive and every mutating method in the app waits on
// it, so a dial underneath it froze all of them for as long as the server took
// — up to the dial timeout plus the helper install.
func TestCreatingARemoteTabConnectsBeforeTakingTheLock(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	// CreateTabWithWorktree holds the body; the others are wrappers that pass
	// no conversation to resume and no worktree.
	body := functionBody(t, text, "func (a *App) CreateTabWithWorktree(")
	dialAt := strings.Index(body, "a.connectionFor(")
	lockAt := strings.Index(body, "a.beginExpectedProjectMutation(")

	if dialAt < 0 || lockAt < 0 {
		t.Fatal("CreateTabWithWorktree no longer connects or no longer locks; rewrite this test")
	}
	if dialAt > lockAt {
		t.Error("CreateTabWithWorktree dials after taking the mutation lock; " +
			"every other mutating method in the app will wait on the server")
	}
}

// The sidebar sweep is what the pollers share. One session on a slow server
// must not withhold the whole tick, and an unreachable server must not collect
// one goroutine per session per tick.
func TestTheSidebarSweepIsBoundedAndCapped(t *testing.T) {
	source, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	body := functionBody(t, text, "func (a *App) getSidebarUpdates(")
	if !strings.Contains(body, "sidebarSweepBudget") {
		t.Error("the sidebar sweep has no overall deadline; one slow session " +
			"withholds the tick from every other session")
	}
	if !strings.Contains(body, "maxConcurrentSidebarSessions") {
		t.Error("the sidebar sweep has no concurrency cap")
	}
}

// Three readers used to run a full sweep each, on their own timers, alongside
// the emitter doing the same thing every second. They read the emitter's last
// result instead.
func TestOtherReadersDoNotStartTheirOwnSweep(t *testing.T) {
	appSource, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	body := functionBody(t, strings.ReplaceAll(string(appSource), "\r\n", "\n"),
		"func (a *App) GetSidebarUpdates(")
	if !strings.Contains(body, "lastSidebarSnapshot") {
		t.Error("GetSidebarUpdates starts a sweep rather than serving the last one")
	}

	watcher, err := os.ReadFile("notifications.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(watcher), "a.getSidebarUpdates(ctx)") {
		t.Error("the attention watcher runs its own full sweep of every session")
	}
}

// A server whose agents are not on its PATH is handled without asking.
//
// An agent installed in ~/.local/bin is invisible to a non-interactive SSH
// shell, so every tab using it failed to start with an error telling the user
// to copy a directory into a settings field. The program can already see where
// the agent is — finding it and then asking is work it can do itself.
func TestAConnectionFindsAgentsTheServersPathMisses(t *testing.T) {
	source, err := os.ReadFile("servers_pool.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	body := functionBody(t, text, "func (a *App) connectionFor(")

	if !strings.Contains(body, "remote.DiscoverAgentPath(") {
		t.Fatal("a connection no longer looks for agents off the server's PATH; " +
			"tabs will fail to start and the user will be asked to find them")
	}

	// Only when nothing is configured: a server set up deliberately must not
	// have its setting second-guessed.
	discoverAt := strings.Index(body, "remote.DiscoverAgentPath(")
	guard := body[:discoverAt]
	if !strings.Contains(guard, `strings.TrimSpace(extraPath) == ""`) {
		t.Error("discovery runs even when a PATH is configured, so it can " +
			"override a deliberate choice")
	}

	// And the result is kept, or every connection pays for the search again.
	if !strings.Contains(body, "rememberDiscoveredPath(") {
		t.Error("a discovered PATH is not stored, so it is searched for on every connection")
	}
}

// Discovery must not overwrite a path the user set.
func TestARememberedPathDoesNotOverwriteAConfiguredOne(t *testing.T) {
	source, err := os.ReadFile("servers_pool.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")

	body := functionBody(t, text, "func (a *App) rememberDiscoveredPath(")
	if !strings.Contains(body, `strings.TrimSpace(list.Servers[index].ExtraPath) == ""`) {
		t.Error("a discovered path can overwrite one the user configured")
	}
}
