//go:build linux

package dictation

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// PulseAudioSource represents a PulseAudio/PipeWire source with friendly name
type PulseAudioSource struct {
	Name        string // Internal name (e.g., alsa_input.usb-...)
	Description string // Friendly description (e.g., "Audio Adapter (Unitek Y-247A) Monó")
}

// getPulseAudioSources returns a map of PulseAudio source names to their friendly descriptions
// This works with both PulseAudio and PipeWire (which provides PulseAudio compatibility)
// pactlTimeout bounds every PulseAudio query.
//
// pactl blocks indefinitely when the sound server is not answering — a stopped
// or wedged daemon, a session without one. GetInputDevices calls it on the way
// into the Dictation settings tab, so an unbounded call froze the whole window:
// the tab is opened by someone whose audio is already misbehaving, which is
// exactly when the daemon is least likely to reply.
//
// Generous enough that a busy machine still answers, short enough that a dead
// daemon costs a noticeable pause rather than the app.
const pactlTimeout = 3 * time.Second

// pactlCommand builds a bounded pactl invocation. The caller must call cancel.
func pactlCommand(args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), pactlTimeout)
	return exec.CommandContext(ctx, "pactl", args...), cancel
}

func getPulseAudioSources() map[string]string {
	result := make(map[string]string)

	// Run pactl to get source list with descriptions
	cmd, cancel := pactlCommand("list", "sources")
	defer cancel()
	cmd.Env = append(cmd.Environ(), "LANG=C") // Force English output for consistent parsing
	output, err := cmd.Output()
	if err != nil {
		return result
	}

	// Parse output to extract source names and descriptions
	// Format:
	// Source #62
	//     Name: alsa_input.pci-0000_0d_00.4.pro-input-0
	//     Description: Starship/Matisse HD Audio Controller Pro
	lines := strings.Split(string(output), "\n")
	var currentName string

	nameRegex := regexp.MustCompile(`^\s*Name:\s*(.+)$`)
	descRegex := regexp.MustCompile(`^\s*Description:\s*(.+)$`)

	for _, line := range lines {
		if matches := nameRegex.FindStringSubmatch(line); len(matches) > 1 {
			currentName = matches[1]
		} else if matches := descRegex.FindStringSubmatch(line); len(matches) > 1 && currentName != "" {
			description := matches[1]
			// Skip monitor sources (they're output monitors, not inputs)
			if !strings.HasPrefix(description, "Monitor of ") {
				result[currentName] = description
			}
			currentName = ""
		}
	}

	return result
}

// getALSACardInfo returns a map of ALSA card numbers to their identifiers
// by reading /proc/asound/cards
func getALSACardInfo() map[string]string {
	result := make(map[string]string)

	// Bounded like the rest. /proc is normally instant, but a wedged sound
	// driver can leave a read on it blocking, and this runs on the same path
	// into the settings tab.
	cmd, cancel := audioCommand("cat", "/proc/asound/cards")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return result
	}

	// Parse format:
	//  1 [Generic        ]: HDA-Intel - HD-Audio Generic
	//  2 [Device         ]: USB-Audio - USB Audio Device
	cardRegex := regexp.MustCompile(`^\s*(\d+)\s+\[(\w+)\s*\]`)
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		if matches := cardRegex.FindStringSubmatch(line); len(matches) > 2 {
			cardNum := matches[1]
			cardName := matches[2]
			result[cardNum] = cardName
		}
	}

	return result
}

// matchPortAudioToPulseAudio tries to find a matching PulseAudio description for a PortAudio device name
// PortAudio names look like: "HD-Audio Generic: ALCS1200A Analog (hw:1,0)"
// PulseAudio names look like: "alsa_input.pci-0000_0d_00.4.pro-input-0"
func matchPortAudioToPulseAudio(portAudioName string, pulseSources map[string]string) string {
	// Extract hw:X,Y from PortAudio name if present
	hwRegex := regexp.MustCompile(`\(hw:(\d+),(\d+)\)`)
	hwMatches := hwRegex.FindStringSubmatch(portAudioName)

	if len(hwMatches) > 2 {
		cardNum := hwMatches[1]
		deviceNum := hwMatches[2]

		// Get ALSA card info to help with matching
		alsaCards := getALSACardInfo()
		alsaCardName := alsaCards[cardNum]

		// Try to find matching PulseAudio source
		for pulseName, description := range pulseSources {
			// For USB devices: match by "usb" in both names
			if strings.Contains(strings.ToLower(portAudioName), "usb") &&
				strings.Contains(strings.ToLower(pulseName), "usb") {
				// Further verify by checking if ALSA card name matches
				if alsaCardName != "" && strings.Contains(strings.ToLower(pulseName), strings.ToLower(alsaCardName)) {
					return description
				}
				// If only one USB source, use it
				return description
			}

			// For PCI/onboard devices: match by card characteristics
			if strings.Contains(portAudioName, "HD-Audio") || strings.Contains(portAudioName, "Generic") {
				if strings.Contains(pulseName, "pci-") && strings.Contains(pulseName, "alsa_input") {
					// Try to match by device number (input-0, input-2, etc.)
					inputPattern := fmt.Sprintf("input-%s", deviceNum)
					if strings.Contains(pulseName, inputPattern) {
						return description
					}
					// Also try pro-input pattern
					proInputPattern := fmt.Sprintf("pro-input-%s", deviceNum)
					if strings.Contains(pulseName, proInputPattern) {
						return description
					}
				}
			}
		}
	}

	// Fallback: try keyword matching
	portAudioLower := strings.ToLower(portAudioName)

	for pulseName, description := range pulseSources {
		pulseLower := strings.ToLower(pulseName)

		// Match USB devices by "usb" keyword
		if strings.Contains(portAudioLower, "usb") && strings.Contains(pulseLower, "usb") {
			return description
		}
	}

	return "" // No match found
}

// getBetterDeviceName returns a more user-friendly device name using PulseAudio if available
func getBetterDeviceName(portAudioName string) string {
	pulseSources := getPulseAudioSources()
	if len(pulseSources) == 0 {
		return portAudioName
	}

	if betterName := matchPortAudioToPulseAudio(portAudioName, pulseSources); betterName != "" {
		return betterName
	}

	return portAudioName
}

// PulseSource represents a PulseAudio/PipeWire source for input device selection
type PulseSource struct {
	Name        string // Internal name (e.g., alsa_input.usb-...)
	Description string // Friendly description (e.g., "Audio Adapter (Unitek Y-247A) Monó")
}

// GetPulseAudioInputDevices returns a list of available PulseAudio/PipeWire input devices
// This filters out monitor sources (which are output monitors, not real inputs)
func GetPulseAudioInputDevices() []PulseSource {
	var sources []PulseSource

	// Run pactl to get source list
	cmd, cancel := pactlCommand("list", "sources")
	defer cancel()
	cmd.Env = append(cmd.Environ(), "LANG=C")
	output, err := cmd.Output()
	if err != nil {
		return sources
	}

	lines := strings.Split(string(output), "\n")
	var currentName, currentDesc string
	isMonitor := false

	nameRegex := regexp.MustCompile(`^\s*Name:\s*(.+)$`)
	descRegex := regexp.MustCompile(`^\s*Description:\s*(.+)$`)

	for _, line := range lines {
		if strings.HasPrefix(line, "Source #") {
			// Save previous source if valid
			if currentName != "" && currentDesc != "" && !isMonitor {
				sources = append(sources, PulseSource{
					Name:        currentName,
					Description: currentDesc,
				})
			}
			// Reset for new source
			currentName = ""
			currentDesc = ""
			isMonitor = false
		} else if matches := nameRegex.FindStringSubmatch(line); len(matches) > 1 {
			currentName = matches[1]
			// Check if this is a monitor source
			if strings.Contains(currentName, ".monitor") {
				isMonitor = true
			}
		} else if matches := descRegex.FindStringSubmatch(line); len(matches) > 1 {
			currentDesc = matches[1]
			// Also check description for monitor
			if strings.HasPrefix(currentDesc, "Monitor of ") {
				isMonitor = true
			}
		}
	}

	// Don't forget the last source
	if currentName != "" && currentDesc != "" && !isMonitor {
		sources = append(sources, PulseSource{
			Name:        currentName,
			Description: currentDesc,
		})
	}

	return sources
}

// SetPulseAudioDefaultSource sets the default PulseAudio/PipeWire input source
func SetPulseAudioDefaultSource(sourceName string) error {
	cmd, cancel := pactlCommand("set-default-source", sourceName)
	defer cancel()
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to set default source: %w", err)
	}
	return nil
}

// GetPulseAudioDefaultSource returns the current default PulseAudio/PipeWire input source name
func GetPulseAudioDefaultSource() string {
	cmd, cancel := pactlCommand("get-default-source")
	defer cancel()
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
