// Package processlock enforces one active harnessd per durable data root.
// The lock is held by the OS, not a timeout or PID record: slow local model
// inference cannot accidentally lose ownership, and a crash releases it.
package processlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrBusy = errors.New("another OnePane backend owns this data root")
var ErrUnsafePath = errors.New("unsafe OnePane instance lock file")

type Lock struct {
	mu sync.Mutex
	file *os.File
}

// Acquire must precede opening, migrating or recovering the durable SQLite
// database. Do not remove or rename the lock path while a backend is live.
func Acquire(dataRoot string) (*Lock, error) {
	if strings.TrimSpace(dataRoot) == "" {
		return nil, fmt.Errorf("%w: empty data root", ErrUnsafePath)
	}
	state := filepath.Join(dataRoot, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		return nil, fmt.Errorf("create OnePane state lock directory: %w", err)
	}
	path := filepath.Join(state, "harnessd.instance.lock")
	if err := ensureRegularOrAbsent(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open OnePane instance lock: %w", err)
	}
	closeFailed := func(err error) (*Lock, error) {
		_ = f.Close()
		return nil, err
	}
	if err := lockFile(f); err != nil {
		if isLockBusy(err) {
			return closeFailed(ErrBusy)
		}
		return closeFailed(fmt.Errorf("acquire OS instance lock: %w", err))
	}
	// Reject a replaced file or symbolic link: two unrelated inodes must
	// never be accepted as ownership of the same data-root lock.
	info, err := os.Lstat(path)
	if err != nil {
		_ = unlockFile(f)
		return closeFailed(fmt.Errorf("verify OnePane instance lock path: %w", err))
	}
	held, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, held) {
		_ = unlockFile(f)
		return closeFailed(ErrUnsafePath)
	}
	return &Lock{file: f}, nil
}

// Close releases the kernel lock without deleting the file. Unlinking a
// live lock file would permit a new process to lock a different inode.
func (l *Lock) Close() error {
	if l == nil { return nil }
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil { return nil }
	f := l.file
	l.file = nil
	unlockErr := unlockFile(f)
	closeErr := f.Close()
	return errors.Join(unlockErr, closeErr)
}

func ensureRegularOrAbsent(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) { return nil }
	if err != nil { return fmt.Errorf("inspect OnePane instance lock: %w", err) }
	if !info.Mode().IsRegular() { return ErrUnsafePath }
	return nil
}
