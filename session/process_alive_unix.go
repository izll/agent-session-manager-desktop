//go:build !windows

package session

import (
	"os"
	"syscall"
)

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// ProcessAlive is processAlive for callers outside this package.
func ProcessAlive(pid int) bool { return processAlive(pid) }
