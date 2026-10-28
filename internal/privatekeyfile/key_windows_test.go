//go:build windows

package privatekeyfile

import (
	"bytes"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestWindowsPrivateKeyDescriptorRoundTripServiceAccounts(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, sid := range []string{"S-1-5-18", "S-1-5-32-544", user.User.Sid.String()} {
		t.Run(sid, func(t *testing.T) {
			f, err := openKey(filepath.Join(t.TempDir(), "descriptor.key"), true)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			h := windows.Handle(f.Fd())
			original, err := descriptor(false)
			if err != nil {
				t.Fatal(err)
			}
			originalACL, _, err := original.DACL()
			if err != nil {
				t.Fatal(err)
			}
			// Keep the granted handle open and restore access before cleanup;
			// the SYSTEM-only test DACL must not strand the test runner's file.
			defer func() {
				if err := windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, originalACL, nil); err != nil {
					t.Error(err)
				}
			}()
			expected, err := descriptorForSID(sid)
			if err != nil {
				t.Fatal(err)
			}
			acl, _, err := expected.DACL()
			if err != nil {
				t.Fatal(err)
			}
			if err := windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
				t.Fatal(err)
			}
			actual, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
			if err != nil {
				t.Fatal(err)
			}
			want := aclEntries.FindAllString(strings.ToUpper(expected.String()), -1)
			got := aclEntries.FindAllString(strings.ToUpper(actual.String()), -1)
			sort.Strings(want)
			sort.Strings(got)
			if strings.Join(want, "\n") != strings.Join(got, "\n") {
				t.Fatalf("Windows changed the intended service-account grants: expected %v, got %v", want, got)
			}
		})
	}
}

func TestWindowsBootstrapKeyRepairsBroadLegacyACLAndProtectsStagingBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	payload := bytes.Repeat([]byte{19}, 32)
	for _, p := range []string{path, path + ".new"} {
		if err := os.WriteFile(p, payload, 0600); err != nil {
			t.Fatal(err)
		}
		sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
		if err != nil {
			t.Fatal(err)
		}
		acl, _, err := sd.DACL()
		if err != nil {
			t.Fatal(err)
		}
		if err = windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := Check(path); err == nil {
		t.Fatal("Everyone-readable legacy key reported private")
	}
	got, err := Ensure(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("existing key changed during ACL repair")
	}
	for _, p := range []string{path, path + ".new"} {
		if err := Check(p); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(b, payload) {
			t.Fatal("legacy key material was lost")
		}
	}
}

func TestWindowsBootstrapHardlinkRejectionDoesNotRetargetACL(t *testing.T) {
	target := filepath.Join(t.TempDir(), "unrelated.key")
	alias := filepath.Join(t.TempDir(), "alias.key")
	if err := os.WriteFile(target, bytes.Repeat([]byte{23}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(target, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Link(target, alias); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if _, err = Ensure(alias); err == nil {
		t.Fatal("foreign hard link was accepted")
	}
	after, err := windows.GetNamedSecurityInfo(target, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if before.String() != after.String() {
		t.Fatal("rejecting a hard link changed the unrelated target's ACL")
	}
}
