// Package main provides Windows-specific startup helpers for the unet daemon.
// This file is only compiled on Windows (GOOS=windows).

//go:build windows

package main

import (
	"fmt"
	"os"
	"unsafe"

	"log/slog"

	"golang.org/x/sys/windows"
)

const waitTimeout uint32 = 0x00000102 // WAIT_TIMEOUT

func checkPrivilegesWindows() {
	var sid *windows.SID
	// Create a well-known SID for the Administrators group.
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		slog.Error("failed to allocate admin SID", "error", err)
		fmt.Fprintln(os.Stderr, "Error: could not determine administrator status.")
		os.Exit(1)
	}
	defer windows.FreeSid(sid)

	// Check whether the current process token contains the admin SID.
	token := windows.GetCurrentProcessToken()
	defer token.Close()

	member, err := token.IsMember(sid)
	if err != nil {
		slog.Error("failed to check token membership", "error", err)
		fmt.Fprintln(os.Stderr, "Error: could not determine administrator status.")
		os.Exit(1)
	}

	if !member {
		slog.Error("not running as administrator")
		fmt.Fprintln(os.Stderr, "Error: unet requires administrator privileges. Right-click and 'Run as administrator'.")
		os.Exit(1)
	}
	slog.Debug("running with administrator privileges")
}

func acquireLockWindows() func() {
	// Create a named mutex that is visible across sessions.
	// Use a unique name to avoid collisions.
	mutexName := "Global\\unet-single-instance"

	// We use CreateMutex via windows API to get ownership semantics.
	mutex, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(mutexName))
	if err != nil {
		slog.Error("failed to create mutex", "name", mutexName, "error", err)
		fmt.Fprintf(os.Stderr, "Error: could not create single-instance lock: %v\n", err)
		os.Exit(1)
	}

	// WaitForSingleObject checks if the mutex was acquired.
	event, err := windows.WaitForSingleObject(mutex, 0)
	if err != nil {
		slog.Error("failed to wait on mutex", "error", err)
		fmt.Fprintf(os.Stderr, "Error: could not acquire single-instance lock: %v\n", err)
		os.Exit(1)
	}

	if event == waitTimeout {
		// Mutex is held by another process.
		slog.Error("another instance is already running (named mutex held)")
		fmt.Fprintln(os.Stderr, "Error: another instance of unet is already running.")
		os.Exit(1)
	}

	// Suppress unused-import for unsafe (used by windows API internals).
	_ = unsafe.Sizeof(uintptr(0))

	slog.Debug("acquired named mutex lock", "name", mutexName)

	return func() {
		windows.ReleaseMutex(mutex)
		windows.CloseHandle(mutex)
		slog.Debug("released named mutex lock")
	}
}
