package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFakeRecorder plants an executable named `name` in dir that appends its
// arguments to logPath and exits 1. Windows will not execute an extensionless
// /bin/sh script, so there it writes a .cmd batch file instead — PATHEXT makes
// `npx` resolve to `npx.cmd` the same way a real npm shim does.
func writeFakeRecorder(t *testing.T, dir, name, logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		script := "@echo off\r\necho %* >> \"" + logPath + "\"\r\nexit /b 1\r\n"
		writeExecutable(t, filepath.Join(dir, name+".cmd"), script)
		return
	}
	writeExecutable(t, filepath.Join(dir, name), "#!/bin/sh\necho \"$@\" >> "+logPath+"\nexit 1\n")
}

// writeFakeBlocker plants an executable named `name` in dir that records its
// start in startedPath and then blocks, so the caller can cancel it.
func writeFakeBlocker(t *testing.T, dir, name, startedPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// cd out of the temp tree first: a running process holds its working
		// directory open, and Windows will not remove a directory anything is
		// using — t.TempDir()'s cleanup then fails the test that just passed.
		//
		// Loop over short pings rather than one long one. Killing the batch
		// interpreter does not kill the ping it is waiting on, so a single
		// 30-second ping outlives the test; a one-second one is gone almost
		// immediately. ping is the batch idiom for sleeping — timeout needs a
		// real console and fails under a redirected stdin.
		script := "@echo off\r\ncd /d \"%SystemRoot%\"\r\n" +
			"echo started > \"" + startedPath + "\"\r\n" +
			"for /l %%i in (1,1,30) do ping -n 2 127.0.0.1 > nul\r\n"
		writeExecutable(t, filepath.Join(dir, name+".cmd"), script)
		return
	}
	writeExecutable(t, filepath.Join(dir, name),
		"#!/bin/sh\necho started > '"+startedPath+"'\nexec sleep 30\n")
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
