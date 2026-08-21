//go:build windows

package main

import "os/exec"

// The GBM probe is skipped on Windows; keep the shared call site buildable.
func configureGPUProbeCommand(_ *exec.Cmd) {}
