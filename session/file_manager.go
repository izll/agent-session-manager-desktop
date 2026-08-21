package session

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// OpenInFileManager shows a directory in the desktop's file manager.
//
// The command differs per platform, and none of them is worth guessing at:
// macOS has `open`, Windows has explorer.exe, and on Linux xdg-open picks
// whatever the desktop has configured (Dolphin, Nautilus, Thunar…) rather than
// this code choosing one.
func OpenInFileManager(dir string) error {
	if dir == "" {
		return fmt.Errorf("error.noDirectory")
	}
	// Check before launching: a file manager handed a path that does not exist
	// either opens the user's home directory or silently does nothing,
	// depending on which one it is. Neither reads as an error.
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("error.directoryNotFound")
	}
	if !info.IsDir() {
		return fmt.Errorf("error.notADirectory")
	}

	cmd := fileManagerCommand(runtime.GOOS, dir)
	HideConsoleWindow(cmd)

	// Start, not Run: a file manager keeps running for as long as its window is
	// open, and waiting for that would block the caller until the user closes
	// it. Errors here are launch failures — a missing xdg-open, mainly.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error.fileManagerFailed")
	}
	// Reaped in the background so the process does not linger as a zombie once
	// the window is closed.
	go func() { _ = cmd.Wait() }()
	return nil
}

func fileManagerCommand(goos, dir string) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.Command("open", dir)
	case "windows":
		// Never route a filesystem path through cmd.exe. Characters such as & and
		// ^ are ordinary valid path characters to this API but shell syntax to
		// `cmd /c start`, which turned opening a crafted directory into command
		// execution. Start+background Wait means explorer.exe's unreliable exit
		// status is ignored already, so the shell workaround is unnecessary.
		return exec.Command("explorer.exe", dir)
	default:
		return exec.Command("xdg-open", dir)
	}
}
