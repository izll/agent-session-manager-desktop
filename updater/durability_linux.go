//go:build linux

package updater

import "os"

func syncUpdateHandle(file *os.File) error { return file.Sync() }
