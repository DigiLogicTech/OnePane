package processlock

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestInstanceLockOwnershipAndCrashSafeReacquisition(t *testing.T) {
	root := t.TempDir()
	first, err := Acquire(root)
	if err != nil { t.Fatal(err) }
	second, err := Acquire(root)
	if !errors.Is(err, ErrBusy) || second != nil {
		t.Fatalf("second ownership must be denied: lock=%v err=%v", second, err)
	}
	if err := first.Close(); err != nil { t.Fatal(err) }
	if err := first.Close(); err != nil { t.Fatalf("Close must be idempotent: %v", err) }
	recovered, err := Acquire(root)
	if err != nil { t.Fatalf("stale lock file must not block recovery: %v", err) }
	defer recovered.Close()
	independent, err := Acquire(t.TempDir())
	if err != nil { t.Fatalf("different data root should remain usable: %v", err) }
	defer independent.Close()
}

func TestInstanceLockRefusesNonregularPath(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "state", "harnessd.instance.lock")
	if err := os.MkdirAll(lockPath, 0o700); err != nil { t.Fatal(err) }
	if _, err := Acquire(root); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("directory lock path was accepted: %v", err)
	}
}

func TestLockProbeChild(t *testing.T) {
	root := os.Getenv("ONEPANE_LOCK_TEST_CHILD")
	if root == "" { return }
	lock, err := Acquire(root)
	if lock != nil {
		_ = lock.Close()
		t.Fatal("child acquired parent-held data-root lock")
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("child should observe busy parent lock: %v", err)
	}
}

func TestIndependentProcessCannotClaimSameRoot(t *testing.T) {
	root := t.TempDir()
	owner, err := Acquire(root)
	if err != nil { t.Fatal(err) }
	defer owner.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockProbeChild$", "-test.v")
	cmd.Env = append(os.Environ(), "ONEPANE_LOCK_TEST_CHILD="+root)
	output, err := cmd.CombinedOutput()
	if err != nil { t.Fatalf("independent child owner check failed: %v: %s", err, output) }
	if ctx.Err() != nil { t.Fatalf("child OS lock probe exceeded deadline: %v", ctx.Err()) }
}
