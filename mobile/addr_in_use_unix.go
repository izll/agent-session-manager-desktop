//go:build !windows

package mobile

import (
	"errors"
	"syscall"
)

func addressInUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }
