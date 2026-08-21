//go:build windows

package updater

import "golang.org/x/sys/windows"

func replaceUpdateManifestFile(oldPath, newPath string) error {
	return windows.Rename(oldPath, newPath)
}
