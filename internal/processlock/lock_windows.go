//go:build windows

package processlock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(f *os.File) error {
	var overlap windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlap)
}
func unlockFile(f *os.File) error {
	var overlap windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlap)
}
func isLockBusy(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
