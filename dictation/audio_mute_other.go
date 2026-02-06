//go:build !windows

package dictation

import (
	"fmt"
)

// Windows-specific functions are not implemented on non-Windows platforms
// These are stubs that will never be called due to the platform checks in audio_mute.go

func (m *AudioMuteManager) getWindowsMuteState() (bool, error) {
	return false, fmt.Errorf("Windows mute not supported on this platform")
}

func (m *AudioMuteManager) setWindowsMuteState(mute bool) error {
	return fmt.Errorf("Windows mute not supported on this platform")
}
