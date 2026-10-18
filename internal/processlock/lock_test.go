package processlock

import (
	"context"
	"bufio"
	"strings"
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

func TestLockHolderChild(t *testing.T) {
	root := os.Getenv("ONEPANE_LOCK_TEST_HOLDER")
	if root == "" { return }
	held, err := Acquire(root)
	if err != nil { t.Fatalf("child cannot acquire lock: %v", err) }
	defer held.Close()
	if _, err := os.Stdout.WriteString("ONEPANE_LOCK_HELD\n"); err != nil { t.Fatal(err) }
	// Parent terminates this helper without running defers to simulate a
	// daemon crash while a model is active. No PID or TTL cleanup is needed.
	select {}
}

func TestKernelReleasesInstanceLockAfterProcessCrash(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockHolderChild$", "-test.v")
	cmd.Env = append(os.Environ(), "ONEPANE_LOCK_TEST_HOLDER="+root)
	pipe, err := cmd.StdoutPipe()
	if err != nil { t.Fatal(err) }
	if err := cmd.Start(); err != nil { t.Fatal(err) }
	reader := bufio.NewReader(pipe)
	ready := false
	for !ready {
		line, err := reader.ReadString('\n')
		if err != nil { _ = cmd.Process.Kill(); _ = cmd.Wait(); t.Fatalf("child startup: %v", err) }
		ready = strings.Contains(line, "ONEPANE_LOCK_HELD")
	}
	if _, err := Acquire(root); !errors.Is(err, ErrBusy) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("live child must still own lock: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil { t.Fatal(err) }
	_ = cmd.Wait()
	if ctx.Err()!=nil { t.Fatalf("child timed out: %v",ctx.Err()) }
	taken, err := Acquire(root)
	if err != nil { t.Fatalf("crashed child retained stale lock: %v", err) }
	defer taken.Close()
}
