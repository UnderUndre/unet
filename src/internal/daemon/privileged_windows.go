package daemon

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func checkPrivileged() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	token := windows.GetCurrentProcessToken()
	defer token.Close()

	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}

	_ = unsafe.Sizeof(uintptr(0))
	return member
}
