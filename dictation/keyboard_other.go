//go:build !windows

package dictation

import "fmt"

// typeTextWindows is a stub for non-Windows platforms
func typeTextWindows(text string) error {
	return fmt.Errorf("Windows keyboard input not supported on this platform")
}
