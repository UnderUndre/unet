//go:build !windows

package config

import "os"

func loosePerms(path string) error { return os.Chmod(path, 0o644) }
func tightPerms(path string) error { return os.Chmod(path, 0o600) }
