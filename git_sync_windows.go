//go:build windows

package main

import (
	"os/exec"
	"strconv"

	"asmgr-desktop/session"
)

// detachFromTerminal needs no terminal work on Windows: git already runs with
// CREATE_NO_WINDOW (session.HideConsoleWindow), so there is no console for a
// prompt to appear on, and the prompts themselves are switched off by
// networkGitEnv.
//
// Stopping it does need care. Killing git.exe alone left git-remote-http
// running, still talking to the server and holding the repository open;
// taskkill /T takes the whole tree with it.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		session.HideConsoleWindow(kill)
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
