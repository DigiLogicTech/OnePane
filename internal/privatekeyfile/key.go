// Package privatekeyfile owns stable bootstrap key files. It never replaces a
// valid key, and never treats Unix mode bits as a Windows access-control list.
package privatekeyfile

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func Ensure(path string) ([]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := checkDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	// Retain but protect a stale staging file from the historical Vault writer.
	if err := protectLegacy(path + ".new"); err != nil {
		return nil, err
	}
	f, err := openKey(path, true)
	created := err == nil
	if errors.Is(err, os.ErrExist) {
		f, err = openKey(path, false)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err = checkIdentity(f); err != nil {
		return nil, err
	}
	if err = protectDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err = protectFile(f); err != nil {
		return nil, err
	}
	if created {
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if _, err = f.Write(key); err != nil {
			return nil, err
		}
		if err = f.Sync(); err != nil {
			return nil, err
		}
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	}
	key, err := io.ReadAll(io.LimitReader(f, 33))
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("bootstrap key must contain exactly 32 bytes; existing content retained")
	}
	if err = checkIdentity(f); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if created {
		if err = syncDirectory(filepath.Dir(path)); err != nil {
			return nil, err
		}
	}
	return key, nil
}

// Check observes privacy without repairing it, including in platform tests.
func Check(path string) error {
	f, err := openKey(path, false)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = checkIdentity(f); err != nil {
		return err
	}
	return verifyFile(f)
}

func protectLegacy(path string) error {
	f, err := openKey(path, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if err = checkIdentity(f); err != nil {
		return err
	}
	return protectFile(f)
}

func checkIdentity(f *os.File) error {
	opened, err := f.Stat()
	if err != nil {
		return err
	}
	named, err := os.Lstat(f.Name())
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, named) {
		return fmt.Errorf("bootstrap key is not the same regular file")
	}
	return checkLinks(f)
}
