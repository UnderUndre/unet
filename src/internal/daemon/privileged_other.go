//go:build !windows

package daemon

import "os"

func checkPrivileged() bool {
	return os.Getuid() == 0
}
