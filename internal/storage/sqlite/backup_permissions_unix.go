//go:build !windows

package sqlite

import (
	"fmt"
	"os"
)

// Private migration snapshots cannot be readable by group or other users.
// On Unix, the OS permission bits are authoritative.
func secureBackupFile(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restrict migration snapshot permissions: %w", err)
	}
	return verifyPrivateBackupFile(path)
}

func verifyPrivateBackupFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("migration snapshot must be a private regular file: mode=%v", info.Mode())
	}
	return nil
}
