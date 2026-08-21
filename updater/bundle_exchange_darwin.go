//go:build darwin

package updater

import "golang.org/x/sys/unix"

func atomicExchangeBundle(installed, staged string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, installed, unix.AT_FDCWD, staged, unix.RENAME_SWAP)
}
