//go:build windows

package passwords

import "os/exec"

// stopWholeProcessGroup leaves Windows to the default: the process is ended, and
// WaitDelay bounds any child still holding the output.
func stopWholeProcessGroup(*exec.Cmd) {}
