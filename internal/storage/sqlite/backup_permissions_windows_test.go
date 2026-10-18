//go:build windows

package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestMigrationBackupWindowsRejectsBroadACLAndRestrictsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-private.bak")
	if err := os.WriteFile(path, []byte("test only"), 0o600); err != nil {
		t.Fatal(err)
	}
	broad, err := windows.SecurityDescriptorFromString("D:P(A;;GR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := broad.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if err := verifyPrivateBackupFile(path); err == nil {
		t.Fatal("broad Everyone DACL was accepted as private")
	}
	if err := secureBackupFile(path); err != nil {
		t.Fatal(err)
	}
	if err := verifyPrivateBackupFile(path); err != nil {
		t.Fatalf("protected service-only snapshot ACL did not survive: %v", err)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("snapshot no longer readable by its service identity: %v", err)
	}
}
