//go:build !windows

package config

import (
	"fmt"
	"os"
)

// checkOwnerOnly reports whether path is readable by the owner only:
// no group/other permission bits set (FR-008.003 POSIX side).
func checkOwnerOnly(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	perm := fi.Mode().Perm()
	return perm&0o077 == 0, nil
}

// tightenOwnerOnly chmods path to 0600 (FR-008.002).
func tightenOwnerOnly(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod 600 %s: %w", path, err)
	}
	return nil
}
