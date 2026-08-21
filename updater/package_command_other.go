//go:build !linux

package updater

import "os/exec"

func configurePackageCommand(_ *exec.Cmd) {}
