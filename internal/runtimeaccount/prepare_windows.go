//go:build windows

package runtimeaccount

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	disableMaxPrivilege = 0x1
	maxGrantEntries     = 20000
)

type sidAndAttributes struct {
	Sid        *windows.SID
	Attributes uint32
}

func Prepare(cmd *exec.Cmd, roots Roots) (func(), error) {
	// Live restriction is opt-in. The token and ACL code is unproven on a
	// LocalSystem service and can block model or GPU startup. Leave it off
	// until a disposable install sets ONEPANE_RESTRICT_MODEL_RUNTIME=1.
	if os.Getenv("ONEPANE_RESTRICT_MODEL_RUNTIME") != "1" {
		return func() {}, nil
	}
	token, err := Apply(cmd, roots, windowsHost{})
	if err != nil {
		return nil, err
	}
	if token == 0 {
		return func() {}, nil
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Token = syscall.Token(token)
	return func() { _ = windows.CloseHandle(windows.Handle(token)) }, nil
}

type windowsHost struct{}

func (windowsHost) LocalSystem() bool {
	token, err := openProcessToken(windows.TOKEN_QUERY)
	if err != nil {
		return false
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil || user.User.Sid == nil {
		return false
	}
	return user.User.Sid.String() == "S-1-5-18"
}

func (windowsHost) RuntimeSID() (string, error) {
	sid, err := lookupAccount(runtimeSID)
	if err != nil {
		return "", err
	}
	return sid.String(), nil
}

func (windowsHost) UsersSID() (string, error) {
	sid, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		return "", err
	}
	return sid.String(), nil
}

func (windowsHost) EnsureDir(path string) error {
	return os.MkdirAll(path, 0o700)
}

func (windowsHost) Grant(path, sid string, write bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a reparse point", path)
	}
	parsed, err := windows.StringToSid(sid)
	if err != nil {
		return err
	}
	count := 0
	return filepathWalk(path, func(name string, entry fs.DirEntry) error {
		count++
		if count > maxGrantEntries {
			return fmt.Errorf("storage tree is too large to adjust in one launch")
		}
		full := name
		st, err := os.Lstat(full)
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			if full == path {
				return fmt.Errorf("%s is a reparse point", full)
			}
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() && !samePath(full, path) && (strings.EqualFold(entry.Name(), "keys") || strings.EqualFold(entry.Name(), "vault")) {
			return fmt.Errorf("refusing to grant access over %s", full)
		}
		return grantOne(full, parsed, write, entry.IsDir())
	})
}

func (windowsHost) Restrict(runtime, users string) (uintptr, error) {
	runtimeSID, err := windows.StringToSid(runtime)
	if err != nil {
		return 0, err
	}
	usersSID, err := windows.StringToSid(users)
	if err != nil {
		return 0, err
	}
	current, err := openProcessToken(windows.TOKEN_DUPLICATE | windows.TOKEN_QUERY | windows.TOKEN_ASSIGN_PRIMARY)
	if err != nil {
		return 0, err
	}
	defer current.Close()
	restricted, err := createRestrictedToken(current, []*windows.SID{runtimeSID, usersSID})
	if err != nil {
		return 0, err
	}
	return uintptr(restricted), nil
}

func openProcessToken(access uint32) (windows.Token, error) {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), access, &token)
	return token, err
}

func lookupAccount(account string) (*windows.SID, error) {
	name, err := windows.UTF16PtrFromString(account)
	if err != nil {
		return nil, err
	}
	var sidSize, domainSize, use uint32
	err = windows.LookupAccountName(nil, name, nil, &sidSize, nil, &domainSize, &use)
	if sidSize == 0 {
		if err == nil {
			err = errors.New("account SID length unavailable")
		}
		return nil, fmt.Errorf("lookup %s: %w", account, err)
	}
	sidBuf := make([]byte, sidSize)
	domain := make([]uint16, domainSize)
	var domainPtr *uint16
	if domainSize > 0 {
		domainPtr = &domain[0]
	}
	sid := (*windows.SID)(unsafe.Pointer(&sidBuf[0]))
	if err = windows.LookupAccountName(nil, name, sid, &sidSize, domainPtr, &domainSize, &use); err != nil {
		return nil, fmt.Errorf("lookup %s: %w", account, err)
	}
	// Copy into a SID owned by the windows package so the buffer can be freed.
	return windows.StringToSid(sid.String())
}

func createRestrictedToken(existing windows.Token, restrict []*windows.SID) (windows.Token, error) {
	if len(restrict) == 0 {
		return 0, errors.New("restricted token requires a restricting SID")
	}
	sids := make([]sidAndAttributes, len(restrict))
	for i, sid := range restrict {
		sids[i].Sid = sid
	}
	var out windows.Token
	r1, _, callErr := windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken").Call(
		uintptr(existing),
		disableMaxPrivilege,
		0, 0,
		0, 0,
		uintptr(len(sids)),
		uintptr(unsafe.Pointer(&sids[0])),
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		if callErr == nil || callErr == windows.ERROR_SUCCESS {
			callErr = syscall.EINVAL
		}
		return 0, callErr
	}
	return out, nil
}

func grantOne(path string, sid *windows.SID, write, directory bool) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil && !errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) {
		return err
	}
	if dacl != nil && aceAllows(dacl, sid, write) {
		return nil
	}
	mask := windows.ACCESS_MASK(windows.GENERIC_READ | windows.GENERIC_EXECUTE)
	if write {
		mask |= windows.GENERIC_WRITE | windows.DELETE
	}
	inherit := uint32(windows.NO_INHERITANCE)
	if directory {
		inherit = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	updated, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: mask,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inherit,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}, dacl)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, updated, nil)
}

func aceAllows(acl *windows.ACL, sid *windows.SID, write bool) bool {
	for i := uint16(0); i < acl.AceCount; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, uint32(i), &ace); err != nil || ace == nil {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !aceSID.Equals(sid) {
			continue
		}
		mask := ace.Mask
		read := mask&windows.GENERIC_ALL != 0 || mask&windows.GENERIC_READ != 0 || mask&windows.FILE_GENERIC_READ != 0 || mask&windows.FILE_READ_DATA != 0
		if !write {
			if read {
				return true
			}
			continue
		}
		canWrite := mask&windows.GENERIC_ALL != 0 || mask&windows.GENERIC_WRITE != 0 || mask&windows.FILE_GENERIC_WRITE != 0 || mask&windows.FILE_WRITE_DATA != 0
		if read && canWrite {
			return true
		}
	}
	return false
}

func filepathWalk(root string, fn func(string, fs.DirEntry) error) error {
	return walk(root, fn)
}

func walk(root string, fn func(string, fs.DirEntry) error) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	entry := fs.FileInfoToDirEntry(info)
	if err := fn(root, entry); err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	children, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, child := range children {
		path := filepath.Join(root, child.Name())
		if err := walk(path, fn); err != nil {
			if errors.Is(err, fs.SkipDir) {
				continue
			}
			return err
		}
	}
	return nil
}
