//go:build windows

package dictation

import (
	"os/exec"
	"time"
)

func configureKeyboardCommand(cmd *exec.Cmd) {
	// CommandContext terminates the direct helper. WaitDelay also closes its
	// pipes if a descendant inherited them and survives independently.
	cmd.WaitDelay = time.Second
}
