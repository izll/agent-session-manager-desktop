package dictation

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// AudioMuteManager handles system audio muting during recording
type AudioMuteManager struct {
	savedMuteState bool
	wasMuted       bool
}

// NewAudioMuteManager creates a new AudioMuteManager
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
		fmt.Printf("⚠️ Failed to get current mute state: %v\n", err)
		// Continue anyway, don't fail the recording
		currentState = false
	}

	m.savedMuteState = currentState
	m.wasMuted = true

	// Mute the output
	err = m.SetMuteState(true)
	if err != nil {
		fmt.Printf("⚠️ Failed to mute output: %v\n", err)
		return err
	}

	fmt.Println("🔇 Output muted")
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
		fmt.Printf("⚠️ Failed to restore mute state: %v\n", err)
		return err
	}

	m.wasMuted = false
	fmt.Println("🔊 Output restored")
	return nil
}

// Linux-specific mute functions using pactl (PulseAudio)
func (m *AudioMuteManager) getLinuxMuteState() (bool, error) {
	cmd := exec.Command("pactl", "get-sink-mute", "@DEFAULT_SINK@")
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

	cmd := exec.Command("pactl", "set-sink-mute", "@DEFAULT_SINK@", muteValue)
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("pactl set-sink-mute failed: %w", err)
	}

	return nil
}

// macOS-specific mute functions
func (m *AudioMuteManager) getMacOSMuteState() (bool, error) {
	cmd := exec.Command("osascript", "-e", "output muted of (get volume settings)")
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

	cmd := exec.Command("osascript", "-e", fmt.Sprintf("set volume %s output muted", muteValue))
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("osascript set volume failed: %w", err)
	}

	return nil
}
