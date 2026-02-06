//go:build !linux

package dictation

// getBetterDeviceName returns a more user-friendly device name
// On non-Linux platforms, just return the original name
func getBetterDeviceName(portAudioName string) string {
	return portAudioName
}

// PulseSource represents an audio source (stub for non-Linux)
type PulseSource struct {
	Name        string
	Description string
}

// GetPulseAudioInputDevices returns empty list on non-Linux platforms
func GetPulseAudioInputDevices() []PulseSource {
	return nil
}

// SetPulseAudioDefaultSource is a no-op on non-Linux platforms
func SetPulseAudioDefaultSource(sourceName string) error {
	return nil
}

// GetPulseAudioDefaultSource returns empty string on non-Linux platforms
func GetPulseAudioDefaultSource() string {
	return ""
}
