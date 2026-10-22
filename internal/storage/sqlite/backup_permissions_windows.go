//go:build windows

package sqlite

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// Windows os.Chmod(0600) does not constrain the NTFS DACL. A migration
// snapshot can contain API keys, model metadata and Project contents: grant
// full access only to this service identity, SYSTEM and Administrators.
// Explicitly protect the DACL from permissive parent-directory inheritance.
// Failure is fatal to migration/recovery, rather than claiming a private
// backup while leaving it accessible to unrelated local principals.
func secureBackupFile(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("read service identity for snapshot ACL: %w", err)
	}
	sid := user.User.Sid.String()
	if sid == "" {
		return fmt.Errorf("snapshot ACL requires a service identity SID")
	}
	sd, err := windows.SecurityDescriptorFromString(
		"D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;" + sid + ")")
	if err != nil {
		return fmt.Errorf("build protected snapshot ACL: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return fmt.Errorf("read protected snapshot ACL: %v", err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("apply protected snapshot ACL: %w", err)
	}
	return verifyPrivateBackupFile(path)
}

func verifyPrivateBackupFile(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION)
	if err != nil || sd == nil {
		return fmt.Errorf("inspect snapshot DACL: %v", err)
	}
	control, _, err := sd.Control()
	if err != nil {
		return fmt.Errorf("inspect snapshot DACL protection: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("snapshot DACL absent or inherits parent permissions")
	}
	// Detect the three common broad read principals whether the SDDL
	// formatter emits aliases or numeric SIDs. We only add the exact service
	// SID, SYSTEM and local Administrators in secureBackupFile.
	sddl := strings.ToUpper(sd.String())
	for _, principal := range []string{
		";;;WD)", ";;;AU)", ";;;BU)",
		";;;S-1-1-0)", ";;;S-1-5-11)", ";;;S-1-5-32-545)",
	} {
		if strings.Contains(sddl, principal) {
			return fmt.Errorf("migration snapshot ACL contains broad principal")
		}
	}
	return nil
}
