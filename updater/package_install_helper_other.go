//go:build !linux

package updater

import (
	"context"
	"fmt"
)

func privilegedPackageHelperArgs(executable, packagePath, trustedChecksum, packageKind string) []string {
	return []string{executable, packagePath, trustedChecksum, packageKind}
}

func HandlePrivilegedPackageInstall([]string) (handled bool, exitCode int) {
	return false, 0
}

func runPrivilegedPackageInstall(context.Context, string, []string, func(func() error) error) ([]byte, error) {
	return nil, fmt.Errorf("privileged package installation is only supported on Linux")
}
