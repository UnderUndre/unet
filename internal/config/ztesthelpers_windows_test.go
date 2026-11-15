//go:build windows

package config

import "golang.org/x/sys/windows"

func loosePerms(path string) error {
	// Everyone = WD: the Windows analog of world-readable (FR-008.003).
	sd, err := windows.SecurityDescriptorFromString("D:(A;;FA;;;WD)(A;;FA;;;SY)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func tightPerms(path string) error { return tightenOwnerOnly(path) }
