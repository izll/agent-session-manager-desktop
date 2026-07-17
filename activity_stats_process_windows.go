//go:build windows

package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

const statsWindowsStillActive = 259

func statsProcessRunning(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Access denied still proves that the process exists. Treat other
		// lookup failures as a process that has already exited.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		// A process whose state cannot be queried must not have its writer
		// lock stolen; the safe failure mode is read-only statistics.
		return true
	}
	return exitCode == statsWindowsStillActive
}
