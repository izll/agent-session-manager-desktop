package main

import (
	"context"
	"log"
	"runtime"
	"sync"
	"time"

	"asmgr-desktop/session"
)

// Saved commands run "in the background" have no tab. The app keeps track of them
// only to tell the user when one fails: a command that worked goes without a word.

// BackgroundCommandFailure tells the interface a background command failed.
type BackgroundCommandFailure struct {
	Name     string `json:"name"`
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
}

type backgroundJob struct {
	name string
	inst *session.Instance
	job  session.BackgroundJob
	// unanswered counts the checks in a row tmux could not be asked.
	unanswered int
}

const backgroundJobPoll = 2 * time.Second

// backgroundUnansweredLimit: five minutes of a server that cannot be asked. Its
// connection is not coming back soon, and the job is let go without a word.
const backgroundUnansweredLimit = 150

type backgroundJobs struct {
	mu   sync.Mutex
	jobs []backgroundJob
}

// runCommandInBackground starts a command with no tab, where the given tab works.
// It answers -1, or, on Windows where it gets a tab after all, that tab's index.
func (a *App) runCommandInBackground(sessionID string, fromWindow int, name, text, expectedProjectID string) (int, error) {
	inst, err := a.storage.GetInstance(sessionID)
	if err != nil {
		return -1, err
	}
	serverID, _ := inst.TabPlacement(fromWindow)
	if session.BackgroundNeedsATab(runtime.GOOS, serverID) {
		return a.runCommandInNewTab(sessionID, fromWindow, name, text, "", expectedProjectID)
	}
	// A tab on another server than its session's: dial it as a new tab would.
	if serverID != "" && serverID != inst.ServerID {
		connection, connErr := a.connectionFor(serverID)
		if connErr != nil {
			return -1, connErr
		}
		session.SetTabExecutor(inst.ID, serverID, connection.executor)
	}
	done, err := a.beginExpectedProjectMutation(expectedProjectID)
	if err != nil {
		return -1, err
	}
	defer done()
	job, err := inst.StartBackgroundCommand(a.lifecycleContext(), fromWindow, name, text)
	if err != nil {
		return -1, err
	}
	a.watchBackgroundJob(backgroundJob{name: name, inst: inst, job: job})
	return -1, nil
}

func (a *App) watchBackgroundJob(job backgroundJob) {
	a.background.mu.Lock()
	defer a.background.mu.Unlock()
	for _, known := range a.background.jobs {
		if known.job.Name == job.job.Name && known.job.Server == job.job.Server {
			return
		}
	}
	a.background.jobs = append(a.background.jobs, job)
}

// adoptBackgroundJobs watches again the jobs on this machine that were started
// before the app last quit: one that failed meanwhile is still reported.
func (a *App) adoptBackgroundJobs() {
	local := &session.Instance{} // no server: this machine's tmux
	for _, job := range session.FindBackgroundJobs(a.lifecycleContext(), session.LocalExecutor) {
		a.watchBackgroundJob(backgroundJob{name: job.Label, inst: local, job: job})
	}
}

func (a *App) backgroundJobCount() int {
	a.background.mu.Lock()
	defer a.background.mu.Unlock()
	return len(a.background.jobs)
}

// checkBackgroundJobs forgets the commands that ended, and reports the ones that
// failed, once each.
func (a *App) checkBackgroundJobs() {
	a.background.mu.Lock()
	jobs := append([]backgroundJob(nil), a.background.jobs...)
	a.background.mu.Unlock()
	ctx := a.lifecycleContext()
	ended := map[string]bool{}
	unanswered := map[string]bool{}
	for _, job := range jobs {
		state := job.inst.BackgroundJobState(ctx, job.job)
		if state.Unknown {
			unanswered[job.job.Name] = true
			continue
		}
		if !state.Finished {
			continue
		}
		ended[job.job.Name] = true
		// A pane kept by remain-on-exit outlives a success too.
		job.inst.ForgetBackgroundJob(ctx, job.job)
		if state.Failed {
			log.Printf("[commands] %q failed in the background with exit code %d", job.name, state.ExitCode)
			a.reportBackgroundFailure(BackgroundCommandFailure{Name: job.name, ExitCode: state.ExitCode, Output: state.Output})
		}
	}
	a.background.mu.Lock()
	defer a.background.mu.Unlock()
	kept := a.background.jobs[:0]
	for _, job := range a.background.jobs {
		if ended[job.job.Name] {
			continue
		}
		if !unanswered[job.job.Name] {
			job.unanswered = 0
		} else if job.unanswered++; job.unanswered > backgroundUnansweredLimit {
			log.Printf("[commands] gave up on %q in the background: its server does not answer", job.name)
			continue
		}
		kept = append(kept, job)
	}
	a.background.jobs = kept
}

func (a *App) reportBackgroundFailure(failure BackgroundCommandFailure) {
	if a.backgroundFailed != nil {
		a.backgroundFailed(failure)
		return
	}
	emitEvent(a.ctx, "commands:background-failed", failure)
}

// watchBackgroundJobs runs for as long as the app does.
func (a *App) watchBackgroundJobs(ctx context.Context) {
	a.adoptBackgroundJobs()
	ticker := time.NewTicker(backgroundJobPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.backgroundJobCount() > 0 {
				a.checkBackgroundJobs()
			}
		}
	}
}
