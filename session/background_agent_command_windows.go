//go:build windows

package session

import "os/exec"

// CommandContext terminates the direct process on Windows. WaitDelay bounds
// inherited output handles; a Windows job object would be needed to own an
// arbitrary installer-style descendant tree, which this read-only Claude probe
// is not expected to create.
func configureClaudeBackgroundCommand(_ *exec.Cmd) {}
