//go:build !darwin

package updater

import "fmt"

func atomicExchangeBundle(_, _ string) error {
	return fmt.Errorf("atomic application bundle exchange is unavailable on this platform")
}
