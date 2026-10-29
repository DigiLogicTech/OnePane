//go:build windows

package privatekeyfile

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func descriptor(directory bool) (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := user.User.Sid.String()
	return descriptorForSID(sid)
}

func descriptorForSID(sid string) (*windows.SECURITY_DESCRIPTOR, error) {
	if sid == "" {
		return nil, fmt.Errorf("bootstrap key owner SID unavailable")
	}
	// Each key gets an explicit creation DACL. Do not propagate directory
	// ACEs to unrelated legacy files or a foreign hard-link target.
	owner, err := windows.StringToSid(sid)
	if err != nil {
		return nil, fmt.Errorf("bootstrap key owner SID invalid: %w", err)
	}
	grants := "D:P(A;;FA;;;SY)(A;;FA;;;BA)"
	// Windows merges duplicate grants when applying a DACL. LocalSystem is
	// already present as SY; adding it again makes verification reject the
	// kernel's equivalent, canonical ACL on every installed service startup.
	if numeric := owner.String(); numeric != "S-1-5-18" && numeric != "S-1-5-32-544" {
		grants += "(A;;FA;;;" + owner.String() + ")"
	}
	return windows.SecurityDescriptorFromString(grants)
}

func openKey(path string, create bool) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	disposition := uint32(windows.OPEN_EXISTING)
	access := uint32(windows.GENERIC_READ | windows.READ_CONTROL | windows.WRITE_DAC)
	var sa *windows.SecurityAttributes
	if create {
		sd, e := descriptor(false)
		if e != nil {
			return nil, e
		}
		sa = &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
		disposition = windows.CREATE_NEW
		access |= windows.GENERIC_WRITE
	}
	h, err := windows.CreateFile(p, access, windows.FILE_SHARE_READ, sa, disposition, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open private key", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(h), path)
	if err = checkLinks(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func checkLinks(f *os.File) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || info.NumberOfLinks != 1 {
		return fmt.Errorf("bootstrap key cannot be a reparse point, directory or hard link")
	}
	return nil
}
func protectDirectory(path string) error { return directorySecurity(path, true) }
func checkDirectory(path string) error   { return directorySecurity(path, false) }
func directorySecurity(path string, protect bool) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("bootstrap key directory cannot be a reparse point")
	}
	if !protect {
		return nil
	}
	return protectHandle(h, true)
}
func protectFile(f *os.File) error { return protectHandle(windows.Handle(f.Fd()), false) }
func verifyFile(f *os.File) error  { return verifyHandle(windows.Handle(f.Fd()), false) }
func protectHandle(h windows.Handle, directory bool) error {
	sd, err := descriptor(directory)
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return fmt.Errorf("private key DACL unavailable: %v", err)
	}
	if err = windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return err
	}
	return verifyHandle(h, directory)
}

var aclEntries = regexp.MustCompile(`\([^)]*\)`)

// File.Sync flushes the created key before it is returned. This does not claim
// independent hardware/power-loss qualification of the filesystem journal.
func syncDirectory(string) error { return nil }

func verifyHandle(h windows.Handle, directory bool) error {
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("bootstrap key DACL is absent or inherited")
	}
	expected, err := descriptor(directory)
	if err != nil {
		return err
	}
	actualACEs := aclEntries.FindAllString(strings.ToUpper(sd.String()), -1)
	expectedACEs := aclEntries.FindAllString(strings.ToUpper(expected.String()), -1)
	sort.Strings(actualACEs)
	sort.Strings(expectedACEs)
	if strings.Join(actualACEs, "\n") != strings.Join(expectedACEs, "\n") {
		return fmt.Errorf("bootstrap key DACL contains unexpected grants")
	}
	return nil
}
