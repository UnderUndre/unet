//go:build windows

package traylogic

import (
	"log/slog"

	"golang.org/x/sys/windows"
)

var (
	singletonHandle windows.Handle
	singletonMutex  = "Local\\unet-tray-singleton"
)

// AcquireSingletonLock attempts to acquire a named mutex. Returns false
// if another tray instance is already running (F6 from review).
func AcquireSingletonLock() bool {
	var err error
	singletonHandle, err = windows.CreateMutex(nil, true, windows.StringToUTF16Ptr(singletonMutex))
	if err != nil {
		slog.Warn("singleton: mutex create failed", "error", err)
		return true // Allow startup on error.
	}

	// ERROR_ALREADY_EXISTS = 183
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		slog.Error("singleton: another tray instance is running")
		return false
	}

	slog.Debug("singleton: lock acquired")
	return true
}

// ReleaseSingletonLock releases the singleton mutex.
func ReleaseSingletonLock() {
	if singletonHandle != 0 {
		windows.CloseHandle(singletonHandle)
		singletonHandle = 0
	}
}
