//go:build windows

package mobile

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Windows reports a taken port with its socket error, not the POSIX one.
func addressInUse(err error) bool { return errors.Is(err, windows.WSAEADDRINUSE) }
