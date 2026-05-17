package config

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

const (
	moveFileReplaceExisting = 0x1
)

// setFilePerm sets a restrictive DACL on Windows: the current user gets
// full control; Everyone else is denied access.  This mirrors the intent
// of 0600 on POSIX.
func setFilePerm(path string, _ os.FileMode) error {
	// Get a security descriptor: owner = full control, others = deny.
	// We use a simple SDDL string.
	//
	// Common well-known SIDs:
	//   BA = Built-in Administrators (S-1-5-32-544)
	//   SY = Local System          (S-1-5-18)
	//   WD = Everyone              (S-1-1-0)
	//   CO = Creator Owner         (S-1-3-0)
	sddl := "D:P(A;;FA;;;BA)(A;;FA;;;SY)(A;;FA;;;CO)(D;;FA;;;WD)"
	// D:P  = DACL, protected (no inheritance from parent)
	// (A;;FA;;;BA)  = Allow FullAccess to Built-in Admins
	// (A;;FA;;;SY)  = Allow FullAccess to Local System
	// (A;;FA;;;CO)  = Allow FullAccess to Creator Owner
	// (D;;FA;;;WD)  = Deny  FullAccess to Everyone

	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("config: SDDL parse: %w", err)
	}

	// Extract DACL from security descriptor.
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("config: extract DACL: %w", err)
	}

	// Apply to file using SetNamedSecurityInfo.
	err = windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, // owner – unchanged
		nil, // group – unchanged
		dacl,
		nil, // SACL – unchanged
	)
	if err != nil {
		return fmt.Errorf("config: windows set ACL: %w", err)
	}
	return nil
}

// atomicRename uses MoveFileEx with MOVEFILE_REPLACE_EXISTING for
// atomic file replacement on Windows.
func atomicRename(src, dst string) error {
	srcPtr, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return fmt.Errorf("config: atomicRename src: %w", err)
	}
	dstPtr, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return fmt.Errorf("config: atomicRename dst: %w", err)
	}

	err = windows.MoveFileEx(srcPtr, dstPtr, moveFileReplaceExisting)
	if err != nil {
		return fmt.Errorf("config: MoveFileEx: %w", err)
	}
	return nil
}
