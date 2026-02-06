//go:build windows

package dictation

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procSendInput    = user32.NewProc("SendInput")
)

const (
	INPUT_KEYBOARD = 1
	KEYEVENTF_UNICODE = 0x0004
	KEYEVENTF_KEYUP   = 0x0002
)

type KEYBDINPUT struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type INPUT struct {
	inputType uint32
	ki        KEYBDINPUT
	padding   [8]byte  // Padding to ensure correct struct size
}

// typeUnicodeChar types a single Unicode character using Windows SendInput
func typeUnicodeChar(char rune) error {
	// Key down event
	var inputDown INPUT
	inputDown.inputType = INPUT_KEYBOARD
	inputDown.ki.wVk = 0
	inputDown.ki.wScan = uint16(char)
	inputDown.ki.dwFlags = KEYEVENTF_UNICODE
	inputDown.ki.time = 0
	inputDown.ki.dwExtraInfo = 0

	// Key up event
	var inputUp INPUT
	inputUp.inputType = INPUT_KEYBOARD
	inputUp.ki.wVk = 0
	inputUp.ki.wScan = uint16(char)
	inputUp.ki.dwFlags = KEYEVENTF_UNICODE | KEYEVENTF_KEYUP
	inputUp.ki.time = 0
	inputUp.ki.dwExtraInfo = 0

	// Send both events
	inputs := []INPUT{inputDown, inputUp}

	ret, _, _ := procSendInput.Call(
		uintptr(2),
		uintptr(unsafe.Pointer(&inputs[0])),
		uintptr(unsafe.Sizeof(inputs[0])),
	)

	if ret == 0 {
		return syscall.GetLastError()
	}

	return nil
}

// typeTextWindows types text using Windows SendInput API with Unicode support
func typeTextWindows(text string) error {
	for _, char := range text {
		if err := typeUnicodeChar(char); err != nil {
			return err
		}
		time.Sleep(5 * time.Millisecond)  // Small delay between characters
	}
	return nil
}
