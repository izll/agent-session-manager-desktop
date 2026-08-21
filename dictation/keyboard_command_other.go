//go:build !linux && !darwin && !windows

package dictation

import (
	"os/exec"
	"time"
)

func configureKeyboardCommand(cmd *exec.Cmd) {
	cmd.WaitDelay = time.Second
}
