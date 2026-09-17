//go:build windows

package main

import "os/exec"

// killWholeGroup does nothing here.
//
// The helper only ever runs on the server, which is Linux or macOS — this file
// exists so the package still compiles when the whole repository is built on a
// Windows machine, which the CI does.
func killWholeGroup(command *exec.Cmd) {}
