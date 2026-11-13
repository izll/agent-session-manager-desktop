//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asmgr-desktop/session"
)

// isolatedTmuxServer points the app at a tmux server of this test's own.
func isolatedTmuxServer(t *testing.T) func(args ...string) ([]byte, error) {
	t.Helper()
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	dir := t.TempDir()
	shim := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nexec '" + real + "' -S '" + filepath.Join(dir, "s") + "' -f /dev/null \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	previous := session.TmuxBinary()
	t.Setenv("TMUX", "")
	session.SetTmuxBinary(shim)
	t.Cleanup(func() {
		_ = exec.Command(shim, "kill-server").Run()
		session.SetTmuxBinary(previous)
	})
	return func(args ...string) ([]byte, error) {
		return exec.Command(shim, args...).Output()
	}
}

// End to end on a real server: only the marked tab whose process ended goes.
func TestAnEndedTabGoesAndTheOthersStay(t *testing.T) {
	tmux := isolatedTmuxServer(t)
	app := passwordTestApp(t)
	inst := &session.Instance{
		ID: "closing", Name: "closing", Path: t.TempDir(),
		FollowedWindows: []session.FollowedWindow{
			{ID: "ended", Index: 1, Agent: session.AgentTerminal, CloseOnExit: session.TabCloseCommand},
			{ID: "running", Index: 2, Agent: session.AgentTerminal, CloseOnExit: session.TabCloseCommand},
			{ID: "unmarked", Index: 3, Agent: session.AgentTerminal},
		},
	}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	name := inst.TmuxSessionName()
	if _, err := tmux("new-session", "-d", "-s", name, "sleep 60"); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("set-option", "-g", "remain-on-exit", "on"); err != nil {
		t.Fatal(err)
	}
	for index, command := range map[string]string{"1": "true", "2": "sleep 60", "3": "true"} {
		if _, err := tmux("new-window", "-d", "-t", name+":"+index, command); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for !inst.TabProcessExited(1) || !inst.TabProcessExited(3) {
		if time.Now().After(deadline) {
			t.Fatal("the commands did not end")
		}
		time.Sleep(20 * time.Millisecond)
	}

	app.closeExitedTabs()

	saved, err := app.storage.GetInstance("closing")
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, window := range saved.FollowedWindows {
		left = append(left, window.ID)
	}
	if strings.Join(left, ",") != "running,unmarked" {
		t.Fatalf("left = %v", left)
	}
	windows, err := tmux("list-windows", "-t", name, "-F", "#{window_index}")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(strings.Fields(string(windows)), ","); got != "0,2,3" {
		t.Fatalf("windows = %s", got)
	}
}

// Another project is open: its tabs are not reachable, and wait until it is again.
func TestATabIsNotClosedFromAnotherProject(t *testing.T) {
	app := passwordTestApp(t)
	app.closeExitedTab("closing", session.TabToClose{ID: "x", Index: 1}, "another-project")
}

// The index is reused after the user closes a tab: the ID decides.
func TestATabReusingAnIndexIsLeftAlone(t *testing.T) {
	app := passwordTestApp(t)
	inst := &session.Instance{ID: "reuse", Name: "reuse", Path: t.TempDir(), FollowedWindows: []session.FollowedWindow{{ID: "new", Index: 1, Agent: session.AgentTerminal}}}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	app.closeExitedTab("reuse", session.TabToClose{ID: "old", Index: 1}, app.storage.GetActiveProjectID())
	// The same tab, moved to another index since it was looked at.
	app.closeExitedTab("reuse", session.TabToClose{ID: "new", Index: 9}, app.storage.GetActiveProjectID())
	saved, err := app.storage.GetInstance("reuse")
	if err != nil || len(saved.FollowedWindows) != 1 {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
}

func backgroundTestApp(t *testing.T) (*App, func(args ...string) ([]byte, error), *session.Instance) {
	t.Helper()
	tmux := isolatedTmuxServer(t)
	app := passwordTestApp(t)
	inst := &session.Instance{ID: "cmds", Name: "cmds", Path: t.TempDir(), Status: session.StatusRunning}
	if err := app.storage.AddInstance(inst); err != nil {
		t.Fatal(err)
	}
	if _, err := tmux("new-session", "-d", "-s", inst.TmuxSessionName(), "sleep 60"); err != nil {
		t.Fatal(err)
	}
	return app, tmux, inst
}

func runSaved(t *testing.T, app *App, command, runIn string) int {
	t.Helper()
	id, err := app.SaveCommand("", "Fájl megnyitás", command, "", "", true, runIn)
	if err != nil {
		t.Fatal(err)
	}
	index, err := app.RunCommand(id, "cmds", 0, nil, app.storage.GetActiveProjectID())
	if err != nil {
		t.Fatal(err)
	}
	return index
}

// In the background means no tab at all, in the folder of the tab it came from.
func TestABackgroundCommandOpensNoTab(t *testing.T) {
	app, _, inst := backgroundTestApp(t)
	if index := runSaved(t, app, "printf ok > marker", session.RunInBackground); index != -1 {
		t.Fatalf("index = %d", index)
	}
	saved, _ := app.storage.GetInstance("cmds")
	if len(saved.FollowedWindows) != 0 {
		t.Fatalf("tabs = %+v", saved.FollowedWindows)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if data, err := os.ReadFile(filepath.Join(inst.Path, "marker")); err == nil && string(data) == "ok" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the command did not run")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A failure is reported once, with what it printed, and leaves nothing behind.
func TestAFailedBackgroundCommandIsReportedOnce(t *testing.T) {
	app, tmux, _ := backgroundTestApp(t)
	var reports []BackgroundCommandFailure
	app.backgroundFailed = func(failure BackgroundCommandFailure) { reports = append(reports, failure) }
	runSaved(t, app, "echo boom; exit 4", session.RunInBackground)
	deadline := time.Now().Add(5 * time.Second)
	for len(reports) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no report")
		}
		app.checkBackgroundJobs()
		time.Sleep(50 * time.Millisecond)
	}
	if len(reports) != 1 || reports[0].Name != "Fájl megnyitás" || reports[0].ExitCode != 4 || !strings.Contains(reports[0].Output, "boom") {
		t.Fatalf("reports = %+v", reports)
	}
	app.checkBackgroundJobs()
	if len(reports) != 1 {
		t.Fatalf("reported again: %+v", reports)
	}
	if sessions, _ := tmux("list-sessions", "-F", "#{session_name}"); strings.Contains(string(sessions), "asm_bg_") {
		t.Fatalf("left behind: %s", sessions)
	}
}

// One that worked is just forgotten.
func TestASuccessfulBackgroundCommandIsForgotten(t *testing.T) {
	app, _, _ := backgroundTestApp(t)
	var reports []BackgroundCommandFailure
	app.backgroundFailed = func(failure BackgroundCommandFailure) { reports = append(reports, failure) }
	runSaved(t, app, "true", session.RunInBackground)
	deadline := time.Now().Add(5 * time.Second)
	for app.backgroundJobCount() > 0 {
		if time.Now().After(deadline) {
			t.Fatal("still watched")
		}
		app.checkBackgroundJobs()
		time.Sleep(50 * time.Millisecond)
	}
	if len(reports) != 0 {
		t.Fatalf("reports = %+v", reports)
	}
}

// A new tab is still a tab, with the command's output in it.
func TestANewTabCommandStillOpensItsTab(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	app, _, _ := backgroundTestApp(t)
	index := runSaved(t, app, "true", session.RunInNewTab)
	saved, _ := app.storage.GetInstance("cmds")
	if record := saved.GetFollowedWindowRecord(index); record == nil || record.CloseOnExit != "" {
		t.Fatalf("record = %+v", record)
	}
}

// A tmux that cannot be asked (a dropped connection) is no answer: the job stays
// watched, and only a long silence lets it go — without a report either way.
func TestAnUnanswerableBackgroundJobIsKeptForAWhile(t *testing.T) {
	app, _, inst := backgroundTestApp(t)
	var reports []BackgroundCommandFailure
	app.backgroundFailed = func(failure BackgroundCommandFailure) { reports = append(reports, failure) }
	app.background.jobs = []backgroundJob{{name: "Távoli", inst: inst, job: session.BackgroundJob{Name: "asm_bg_x", Server: "nowhere"}}}
	app.checkBackgroundJobs()
	if app.backgroundJobCount() != 1 {
		t.Fatal("dropped after one unanswered check")
	}
	for range backgroundUnansweredLimit {
		app.checkBackgroundJobs()
	}
	if app.backgroundJobCount() != 0 || len(reports) != 0 {
		t.Fatalf("jobs = %d, reports = %+v", app.backgroundJobCount(), reports)
	}
}

// A job started before the app last quit is still reported, by its name.
func TestALeftoverBackgroundJobIsWatchedAgain(t *testing.T) {
	app, _, inst := backgroundTestApp(t)
	var reports []BackgroundCommandFailure
	app.backgroundFailed = func(failure BackgroundCommandFailure) { reports = append(reports, failure) }
	if _, err := inst.StartBackgroundCommand(context.Background(), 0, "Régi", "exit 6"); err != nil {
		t.Fatal(err)
	}
	app.adoptBackgroundJobs()
	app.adoptBackgroundJobs()
	if app.backgroundJobCount() != 1 {
		t.Fatalf("jobs = %d", app.backgroundJobCount())
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(reports) == 0 && time.Now().Before(deadline) {
		app.checkBackgroundJobs()
		time.Sleep(50 * time.Millisecond)
	}
	if len(reports) != 1 || reports[0].Name != "Régi" || reports[0].ExitCode != 6 {
		t.Fatalf("reports = %+v", reports)
	}
}

// Picked while another project was being switched to: it does not run at all.
func TestABackgroundCommandForAnotherProjectDoesNotRun(t *testing.T) {
	app, _, _ := backgroundTestApp(t)
	id, err := app.SaveCommand("", "Build", "true", "", "", true, session.RunInBackground)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.RunCommand(id, "cmds", 0, nil, "some-other-project"); err == nil {
		t.Fatal("ran for another project")
	}
	if app.backgroundJobCount() != 0 {
		t.Fatal("started anyway")
	}
}

// Under remain-on-exit a command that worked leaves a dead pane: it is cleared too.
func TestASuccessfulBackgroundCommandLeavesNothingUnderRemainOnExit(t *testing.T) {
	app, tmux, _ := backgroundTestApp(t)
	if _, err := tmux("set-option", "-g", "remain-on-exit", "on"); err != nil {
		t.Fatal(err)
	}
	runSaved(t, app, "true", session.RunInBackground)
	deadline := time.Now().Add(5 * time.Second)
	for app.backgroundJobCount() > 0 && time.Now().Before(deadline) {
		app.checkBackgroundJobs()
		time.Sleep(50 * time.Millisecond)
	}
	if sessions, _ := tmux("list-sessions", "-F", "#{session_name}"); strings.Contains(string(sessions), "asm_bg_") {
		t.Fatalf("left behind: %s", sessions)
	}
}
