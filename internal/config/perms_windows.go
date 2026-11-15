//go:build windows

package config

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// Windows ACL enforcement (FR-008.002/.003): a secret-bearing file is
// owner-only when its DACL grants access ONLY to the current user, SYSTEM
// (SY), and Administrators (BA). Any allow-ACE for Everyone (WD), Builtin
// Users (BU), Authenticated Users (AU), or any other principal is the Windows
// equivalent of group/world-readable bits.

// sddlAllowAbbrev are the SDDL account abbreviations that keep the file
// owner-only-equivalent (besides the owner's own SID string).
var sddlAllowAbbrev = map[string]bool{
	"SY": true, // SYSTEM
	"BA": true, // Administrators
	"OW": true, // owner rights
}

// checkOwnerOnly inspects the file DACL via GetNamedSecurityInfo and returns
// false when any allow-ACE grants access beyond the owner allow-list.
func checkOwnerOnly(path string) (bool, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, fmt.Errorf("GetNamedSecurityInfo(%s): %w", path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return false, fmt.Errorf("SECURITY_DESCRIPTOR.Owner(%s): %w", path, err)
	}
	if owner == nil {
		return false, nil
	}
	ownerSID := owner.String()

	sddl := sd.String()
	dacl, _, err := sd.DACL()
	if err != nil {
		return false, fmt.Errorf("SECURITY_DESCRIPTOR.DACL(%s): %w", path, err)
	}
	if dacl == nil {
		return false, nil // no DACL at all = inherited, unprotected
	}
	return sddlAllowListed(sddl, ownerSID)
}

// sddlAllowListed parses the SDDL string and verifies every allow-ACE
// ("(A;...;sid)") targets only the owner or the SYSTEM/BA/OW abbreviations.
// Deny ACEs are irrelevant to the readability question.
func sddlAllowListed(sddl, ownerSID string) (bool, error) {
	for _, group := range sddlAceGroups(sddl) {
		inner := strings.TrimSuffix(strings.TrimPrefix(group, "("), ")")
		if !strings.HasPrefix(inner, "A;") {
			continue // not an access-allowed ACE (D=deny, AU=audit, …)
		}
		fields := strings.Split(inner, ";")
		if len(fields) < 6 {
			continue // malformed; do not fail the check on syntax we don't recognize
		}
		sid := strings.TrimSpace(fields[5])
		if sid == ownerSID || sddlAllowAbbrev[sid] {
			continue
		}
		return false, nil
	}
	return true, nil
}

// sddlAceGroups extracts "(...)" ACE groups from an SDDL string.
func sddlAceGroups(sddl string) []string {
	var groups []string
	depth := 0
	start := -1
	for i, r := range sddl {
		switch r {
		case '(':
			if depth == 0 {
				start = i
			}
			depth++
		case ')':
			depth--
			if depth == 0 && start >= 0 {
				groups = append(groups, sddl[start:i+1])
				start = -1
			}
		}
	}
	return groups
}

// tightenOwnerOnly applies an owner-only DACL (FR-008.002): full access for
// the current user, SYSTEM and Administrators; nothing inherited, nothing else.
func tightenOwnerOnly(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("GetNamedSecurityInfo(%s): %w", path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("SECURITY_DESCRIPTOR.Owner(%s): %w", path, err)
	}
	if owner == nil {
		return fmt.Errorf("cannot resolve owner of %s", path)
	}
	sddl := fmt.Sprintf("D:(A;;FA;;;%s)(A;;FA;;;SY)(A;;FA;;;BA)", owner.String())
	newSD, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("SecurityDescriptorFromString: %w", err)
	}
	dacl, _, err := newSD.DACL()
	if err != nil {
		return fmt.Errorf("SECURITY_DESCRIPTOR.DACL: %w", err)
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
