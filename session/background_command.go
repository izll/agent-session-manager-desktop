package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// A saved command run "in the background": with no tab at all. It runs in a tmux
// session of its own that the app never shows, on the machine and in the folder of
// the tab it was started from, and goes away when it is done. The user hears of it
// only when it fails.
//
// A session of its own rather than a window of the app's session: a window there is
// what the app shows as a tab.

// backgroundJobPrefix names the hidden sessions. Not "_gui_": the terminal's mirror
// sessions are named that way, and cleaned up as orphans.
const backgroundJobPrefix = "asm_bg_"

// backgroundExitTitle is the pane title a failed command leaves, with its exit code.
// A title rather than a line of output: what the command prints can say anything.
const backgroundExitTitle = "asmgr-bg-exit:"

// backgroundLabelOption keeps the command's name on its session, so a job started
// before the app last quit can still be reported by name.
const backgroundLabelOption = "@asmgr_bg_name"

// backgroundOutputLines is how much of a failure's output is kept.
const backgroundOutputLines = 15

// BackgroundJob is a command started in the background.
type BackgroundJob struct {
	Name   string
	Server string
	// Label is the saved command's name.
	Label string
}

// BackgroundJobState is how a background command stands.
type BackgroundJobState struct {
	Finished bool
	Failed   bool
	// Unknown: tmux could not be asked (a dropped connection, a timeout).
	Unknown  bool
	ExitCode int
	// Output is the last lines it printed, for a failure.
	Output string
}

// BackgroundNeedsATab: psmux, the Windows tmux, runs no POSIX shell, and the
// script below is one. There a background command still gets a tab.
func BackgroundNeedsATab(goos, serverID string) bool {
	return goos == "windows" && serverID == ""
}

func backgroundJobName() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return backgroundJobPrefix + hex.EncodeToString(buf)
}

// backgroundBytes spells text as decimal byte values. tmux hands the line to the
// user's default-shell, and no quoting reads the same in sh and in fish (where \\
// and \' are escapes inside single quotes); a list of numbers needs none.
func backgroundBytes(text string) string {
	values := make([]string, len(text))
	for at := 0; at < len(text); at++ {
		values[at] = strconv.Itoa(int(text[at]))
	}
	return strings.Join(values, " ")
}

// backgroundScript runs the command as a tab would — the user's shell, its rc files,
// in the tab's folder — but with no input, since nobody can answer it. A failure keeps
// the session, so what it printed can still be read, and marks it in the pane title.
// Stopped by Ctrl+C or a TERM is not a failure. A missing folder is: tmux would start
// the pane in the home folder instead.
//
// The line holds no single quote and no backslash pair, so sh, bash, zsh and fish
// all read it the same.
func backgroundScript(workDir, text string) string {
	decode := `LC_ALL=C awk "BEGIN{for(i=1;i<ARGC;i++)printf \"%c\",ARGV[i]+0}"`
	script := "d=$(" + decode + " " + backgroundBytes(workDir) + ")\n" +
		"c=$(" + decode + " " + backgroundBytes(text) + ")\n" +
		"s=${SHELL:-/bin/sh}\n" +
		"case ${s##*/} in bash|zsh|fish) set -- -ilc ;; *) set -- -c ;; esac\n" +
		"if cd -- \"$d\"; then \"$s\" \"$@\" \"$c\" </dev/null; st=$?; else st=1; fi\n" +
		"case $st in 0|130|143) exit 0 ;; esac\n" +
		"printf \"\\033]2;" + backgroundExitTitle + "%s\\007\" \"$st\"\n" +
		"exec sleep 600"
	return "exec sh -c '" + script + "'"
}

// StartBackgroundCommand runs a command with no tab, where the given tab works.
func (i *Instance) StartBackgroundCommand(ctx context.Context, fromWindow int, label, text string) (BackgroundJob, error) {
	_, workDir := i.TabPlacement(fromWindow)
	if workDir == "" {
		workDir = i.Path
	}
	job := BackgroundJob{Name: backgroundJobName(), Server: i.serverForWindow(fromWindow), Label: label}
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	executor := i.execOn(job.Server)
	err := executor.Run(commandCtx, "new-session", "-d", "-s", job.Name, "-c", workDir,
		"-x", "200", "-y", "50", backgroundScript(workDir, text))
	if err != nil {
		return BackgroundJob{}, fmt.Errorf("could not start the command: %w", err)
	}
	// Best effort: without it a leftover job is reported without its name.
	_ = executor.Run(commandCtx, "set-option", "-t", "="+job.Name+":", backgroundLabelOption, label)
	return job, nil
}

// BackgroundJobState reads how a background command stands.
func (i *Instance) BackgroundJobState(ctx context.Context, job BackgroundJob) BackgroundJobState {
	return readBackgroundJob(ctx, i.execOn(job.Server), job)
}

func readBackgroundJob(ctx context.Context, executor Executor, job BackgroundJob) BackgroundJobState {
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	target := "=" + job.Name + ":"
	pane, err := executor.Output(commandCtx, "display-message", "-p", "-t", target,
		"#{session_name}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_title}")
	fields := strings.SplitN(strings.TrimRight(string(pane), "\n"), "\t", 4)
	// A target that is gone can be answered for another session: only its own name
	// makes the answer its own.
	if err != nil || fields[0] != job.Name {
		// Gone, or tmux could not be asked: only a listing that answers without it
		// says it ended — and with no failure, since a failure keeps its session.
		sessions, listErr := executor.Output(commandCtx, "list-sessions", "-F", "#{session_name}")
		if listErr != nil {
			return BackgroundJobState{Unknown: true}
		}
		for _, name := range strings.Split(string(sessions), "\n") {
			if name == job.Name {
				return BackgroundJobState{Unknown: true}
			}
		}
		return BackgroundJobState{Finished: true}
	}
	for len(fields) < 4 {
		fields = append(fields, "")
	}
	exitCode := 0
	if code, failed := strings.CutPrefix(fields[3], backgroundExitTitle); failed {
		exitCode, _ = strconv.Atoi(code)
	} else if fields[1] == "1" {
		// Ended, and kept by remain-on-exit: the script exits 0 on success.
		if exitCode, _ = strconv.Atoi(fields[2]); exitCode == 0 {
			return BackgroundJobState{Finished: true}
		}
	} else {
		return BackgroundJobState{}
	}
	output, _ := executor.Output(commandCtx, "capture-pane", "-p", "-J", "-S", "-200", "-t", target)
	return BackgroundJobState{Finished: true, Failed: true, ExitCode: exitCode, Output: lastPrintedLines(string(output))}
}

// lastPrintedLines keeps the end of what a command printed, without the line tmux
// adds to a pane kept after it ended.
func lastPrintedLines(output string) string {
	var printed []string
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "Pane is dead") {
			printed = append(printed, line)
		}
	}
	for len(printed) > 0 && strings.TrimSpace(printed[len(printed)-1]) == "" {
		printed = printed[:len(printed)-1]
	}
	if len(printed) > backgroundOutputLines {
		printed = printed[len(printed)-backgroundOutputLines:]
	}
	return strings.Join(printed, "\n")
}

// ForgetBackgroundJob ends what is left of a background command.
func (i *Instance) ForgetBackgroundJob(ctx context.Context, job BackgroundJob) {
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	_ = i.execOn(job.Server).Run(commandCtx, "kill-session", "-t", "="+job.Name)
}

// FindBackgroundJobs lists the background commands on a tmux server, with their
// names: the ones started before the app last quit are tracked from nowhere else.
func FindBackgroundJobs(ctx context.Context, executor Executor) []BackgroundJob {
	commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
	defer cancel()
	output, err := executor.Output(commandCtx, "list-sessions", "-F", "#{session_name}\t#{"+backgroundLabelOption+"}")
	if err != nil {
		return nil
	}
	var jobs []BackgroundJob
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		name, label, _ := strings.Cut(line, "\t")
		if strings.HasPrefix(name, backgroundJobPrefix) {
			jobs = append(jobs, BackgroundJob{Name: name, Label: label})
		}
	}
	return jobs
}

// IsBackgroundJobSession tells a hidden background session from the user's own.
func IsBackgroundJobSession(name string) bool {
	return strings.HasPrefix(name, backgroundJobPrefix)
}
