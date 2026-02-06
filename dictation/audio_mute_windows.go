//go:build windows

package dictation

import (
	"fmt"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// getWindowsMuteState gets the current mute state using Windows Core Audio API
func (m *AudioMuteManager) getWindowsMuteState() (bool, error) {
	// Initialize COM (ignore error if already initialized)
	ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)

	// Get device enumerator
	var mmde *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &mmde); err != nil {
		return false, fmt.Errorf("CoCreateInstance failed: %w", err)
	}
	defer mmde.Release()

	// Get default audio endpoint (render device)
	var mmd *wca.IMMDevice
	if err := mmde.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &mmd); err != nil {
		return false, fmt.Errorf("GetDefaultAudioEndpoint failed: %w", err)
	}
	defer mmd.Release()

	// Activate audio endpoint volume interface
	var aev *wca.IAudioEndpointVolume
	if err := mmd.Activate(wca.IID_IAudioEndpointVolume, wca.CLSCTX_ALL, nil, &aev); err != nil {
		return false, fmt.Errorf("Activate IAudioEndpointVolume failed: %w", err)
	}
	defer aev.Release()

	// Get mute state
	var muted bool
	if err := aev.GetMute(&muted); err != nil {
		return false, fmt.Errorf("GetMute failed: %w", err)
	}

	return muted, nil
}

// setWindowsMuteState sets the mute state using Windows Core Audio API
func (m *AudioMuteManager) setWindowsMuteState(mute bool) error {
	// Initialize COM (ignore error if already initialized)
	ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)

	// Get device enumerator
	var mmde *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &mmde); err != nil {
		return fmt.Errorf("CoCreateInstance failed: %w", err)
	}
	defer mmde.Release()

	// Get default audio endpoint (render device)
	var mmd *wca.IMMDevice
	if err := mmde.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &mmd); err != nil {
		return fmt.Errorf("GetDefaultAudioEndpoint failed: %w", err)
	}
	defer mmd.Release()

	// Activate audio endpoint volume interface
	var aev *wca.IAudioEndpointVolume
	if err := mmd.Activate(wca.IID_IAudioEndpointVolume, wca.CLSCTX_ALL, nil, &aev); err != nil {
		return fmt.Errorf("Activate IAudioEndpointVolume failed: %w", err)
	}
	defer aev.Release()

	// Set mute state
	if err := aev.SetMute(mute, nil); err != nil {
		return fmt.Errorf("SetMute failed: %w", err)
	}

	return nil
}
