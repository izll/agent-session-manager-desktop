//go:build !windows

package session

import "os/exec"

// Only Windows allocates a console for child processes of a GUI app.
func hideConsoleWindow(cmd *exec.Cmd) {}
