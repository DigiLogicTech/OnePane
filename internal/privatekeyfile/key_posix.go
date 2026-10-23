//go:build !windows

package privatekeyfile

import (
	"fmt"
	"os"
	"syscall"
)

func openKey(path string, create bool) (*os.File, error) {
	flags := os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if create {
		flags = os.O_RDWR | os.O_CREATE | os.O_EXCL
	}
	return os.OpenFile(path, flags, 0600)
}
func checkLinks(f *os.File) error {
	v, err := f.Stat()
	if err != nil {
		return err
	}
	if st, ok := v.Sys().(*syscall.Stat_t); ok && st.Nlink != 1 {
		return fmt.Errorf("bootstrap key has multiple hard links")
	}
	return nil
}
func protectDirectory(path string) error { return checkDirectory(path) }
func checkDirectory(path string) error {
	v, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !v.IsDir() || v.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("bootstrap key directory must not be a symlink")
	}
	return nil
}
func verifyFile(f *os.File) error {
	v, err := f.Stat()
	if err != nil {
		return err
	}
	if v.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("bootstrap key requires owner-only permissions")
	}
	return nil
}
func protectFile(f *os.File) error { return verifyFile(f) }
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
