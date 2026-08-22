//go:build darwin

package updater

import (
	"os"

	"golang.org/x/sys/unix"
)

// F_FULLFSYNC also asks the drive to flush its volatile cache. A successful
// updater must not publish bytes that can still disappear on a power failure.
func syncUpdateHandle(file *os.File) error {
	if err := file.Sync(); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return err
	}
	_, err = unix.FcntlInt(file.Fd(), unix.F_FULLFSYNC, 0)
	return err
}
