//go:build !windows

package config

import (
	"os"
)

func setFilePerm(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}

func atomicRename(src, dst string) error {
	return os.Rename(src, dst)
}
