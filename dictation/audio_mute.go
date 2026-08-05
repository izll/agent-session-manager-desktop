package dictation

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// AudioMuteManager handles system audio muting during recording
type AudioMuteManager struct {
	savedMuteState bool
	wasMuted       bool
}

// NewAudioMuteManager creates a new AudioMuteManager
// audioTimeout bounds the mixer commands.
//
// These run while starting and stopping a recording, so a sound server that has
// stopped answering would hang the toggle rather than the query that noticed —
// and the user is left with a hotkey that does nothing. Short, because muting
// is a local operation that either works at once or is not going to.
const audioTimeout = 3 * time.Second

// audioCommand builds a bounded mixer invocation. The caller must call cancel.
func audioCommand(name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), audioTimeout)
	return exec.CommandContext(ctx, name, args...), cancel
}

func NewAudioMuteManager() *AudioMuteManager {
	return &AudioMuteManager{
		savedMuteState: false,
		wasMuted:       false,
	}
}

// GetMuteState returns the current mute state of the system
func (m *AudioMuteManager) GetMuteState() (bool, error) {
	switch runtime.GOOS {
	case "linux":
		return m.getLinuxMuteState()
	case "windows":
		return m.getWindowsMuteState()
	case "darwin":
		return m.getMacOSMuteState()
	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// SetMuteState sets the mute state of the system
func (m *AudioMuteManager) SetMuteState(mute bool) error {
	switch runtime.GOOS {
	case "linux":
		return m.setLinuxMuteState(mute)
	case "windows":
		return m.setWindowsMuteState(mute)
	case "darwin":
		return m.setMacOSMuteState(mute)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// MuteOutput mutes the system audio and saves the previous state
func (m *AudioMuteManager) MuteOutput() error {
	// Get current mute state
	currentState, err := m.GetMuteState()
	if err != nil {
		logToFile("⚠️ Failed to get current mute state: %v\n", err)
		// Continue anyway, don't fail the recording
		currentState = false
	}

	m.savedMuteState = currentState
	m.wasMuted = true

	// Mute the output
	err = m.SetMuteState(true)
	if err != nil {
		logToFile("⚠️ Failed to mute output: %v\n", err)
		return err
	}

	logToFile("🔇 Output muted" + "\n")
	return nil
}

// UnmuteOutput restores the previous mute state
func (m *AudioMuteManager) UnmuteOutput() error {
	if !m.wasMuted {
		return nil // Nothing to restore
	}

	// Restore previous mute state
	err := m.SetMuteState(m.savedMuteState)
	if err != nil {
		logToFile("⚠️ Failed to restore mute state: %v\n", err)
		return err
	}

	m.wasMuted = false
	logToFile("🔊 Output restored" + "\n")
	return nil
}

// Linux-specific mute functions using pactl (PulseAudio)
func (m *AudioMuteManager) getLinuxMuteState() (bool, error) {
	cmd, cancel := audioCommand("pactl", "get-sink-mute", "@DEFAULT_SINK@")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("pactl command failed: %w", err)
	}

	outputStr := strings.ToLower(string(output))
	// Output format: "Mute: yes" or "Mute: no"
	return strings.Contains(outputStr, "yes") || strings.Contains(outputStr, "igen"), nil
}

func (m *AudioMuteManager) setLinuxMuteState(mute bool) error {
	muteValue := "0"
	if mute {
		muteValue = "1"
	}

	cmd, cancel := audioCommand("pactl", "set-sink-mute", "@DEFAULT_SINK@", muteValue)
	defer cancel()
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("pactl set-sink-mute failed: %w", err)
	}

	return nil
}

// macOS-specific mute functions
func (m *AudioMuteManager) getMacOSMuteState() (bool, error) {
	cmd, cancel := audioCommand("osascript", "-e", "output muted of (get volume settings)")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("osascript command failed: %w", err)
	}

	outputStr := strings.TrimSpace(string(output))
	return outputStr == "true", nil
}

func (m *AudioMuteManager) setMacOSMuteState(mute bool) error {
	muteValue := "without"
	if mute {
		muteValue = "with"
	}

	cmd, cancel := audioCommand("osascript", "-e", fmt.Sprintf("set volume %s output muted", muteValue))
	defer cancel()
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("osascript set volume failed: %w", err)
	}

	return nil
}
