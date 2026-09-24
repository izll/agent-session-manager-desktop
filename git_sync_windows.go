//go:build windows

package main

import "os/exec"

// detachFromTerminal has nothing to do on Windows: git already runs with
// CREATE_NO_WINDOW (session.HideConsoleWindow), so there is no console for a
// prompt to appear on, and the prompts themselves are switched off by
// networkGitEnv.
func detachFromTerminal(cmd *exec.Cmd) {}
