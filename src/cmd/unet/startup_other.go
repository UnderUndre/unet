//go:build !windows

package main

import (
	"fmt"
	"os"
	"unsafe"
)

// Stub implementations for non-Windows platforms.
// The real implementations live in startup_windows.go (build-tagged windows).

func checkPrivilegesWindows() {
	// Should never be called on non-Windows; guard just in case.
	fmt.Fprintln(os.Stderr, "Error: Windows privilege check called on non-Windows platform.")
	os.Exit(1)
}

func acquireLockWindows() func() {
	// Should never be called on non-Windows.
	fmt.Fprintln(os.Stderr, "Error: Windows lock called on non-Windows platform.")
	os.Exit(1)
	// Unreachable, but keeps the compiler happy.
	return func() {}
}

// Suppress unused import (unsafe is used in the Windows build).
var _ = unsafe.Sizeof(uintptr(0))
