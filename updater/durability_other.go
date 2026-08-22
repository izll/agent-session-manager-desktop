//go:build !linux && !darwin

package updater

import "os"

// Durable rename installation is currently Linux/macOS-only. These stubs keep
// platform-neutral code compilable without claiming Windows multi-file atomicity.
func syncUpdateHandle(file *os.File) error { return file.Sync() }
func syncUpdateDirectory(string) error     { return nil }
func syncUpdateDirectoryTree(string) error { return nil }
