//go:build windows

package main

import (
	"os/exec"
	"time"
)

func configureBackgroundAgentCommand(cmd *exec.Cmd) {
	// CommandContext terminates the direct process on Windows. WaitDelay also
	// closes inherited pipes if a descendant keeps them open after that exit.
	cmd.WaitDelay = time.Second
}
