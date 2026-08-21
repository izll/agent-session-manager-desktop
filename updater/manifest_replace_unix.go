//go:build !windows

package updater

import "os"

func replaceUpdateManifestFile(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
