//go:build linux

package serviceinstall

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func storageTestRoot(t *testing.T) (string, int) {
	t.Helper()
	root := t.TempDir()
	fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(fd) })
	return root, fd
}

func TestSystemdStorageWalkRejectsLinksAndPreservesExistingOwnership(t *testing.T) {
	root, fd := storageTestRoot(t)
	uid, gid := os.Geteuid(), os.Getegid()
	owned, err := walkStorage(fd, "srv/onepane/data", uid, gid, uid, true)
	if err != nil {
		t.Fatal(err)
	}
	syscall.Close(owned)
	if err = os.WriteFile(filepath.Join(root, "srv/onepane/data", "existing"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("onepane", filepath.Join(root, "srv/linked")); err != nil {
		t.Fatal(err)
	}
	if linked, e := walkStorage(fd, "srv/linked/data", uid, gid, uid, true); e == nil {
		syscall.Close(linked)
		t.Fatal("symlink ancestor accepted")
	}
	if err = os.Mkdir(filepath.Join(root, "unsafe"), 0777); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(root, "unsafe"), 0777); err != nil {
		t.Fatal(err)
	}
	if unsafe, e := walkStorage(fd, "unsafe/data", uid, gid, uid, true); e == nil {
		syscall.Close(unsafe)
		t.Fatal("world-writable ancestor accepted")
	}
	if _, e := os.Stat(filepath.Join(root, "unsafe/data")); !os.IsNotExist(e) {
		t.Fatal("created storage below unsafe ancestor")
	}
	if raw, e := os.ReadFile(filepath.Join(root, "srv/onepane/data/existing")); e != nil || string(raw) != "preserve" {
		t.Fatal("existing data changed")
	}
}

func TestSystemdStorageDropInOnlyReplacesOwnedManagedFile(t *testing.T) {
	root, fd := storageTestRoot(t)
	uid := os.Geteuid()
	if err := writeManagedDropIn(fd, "storage.conf", []byte(storageMarker+"[Service]\nReadWritePaths=/srv/onepane\n"), uid); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedDropIn(fd, "storage.conf", []byte(storageMarker+"[Service]\nReadWritePaths=/mnt/onepane\n"), uid); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manual.conf"), []byte("# administrator customization\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedDropIn(fd, "manual.conf", []byte(storageMarker), uid); err == nil {
		t.Fatal("administrator drop-in overwritten")
	}
	if err := os.Symlink("manual.conf", filepath.Join(root, "linked.conf")); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedDropIn(fd, "linked.conf", []byte(storageMarker), uid); err == nil {
		t.Fatal("linked drop-in overwritten")
	}
	if err := syscall.Mkfifo(filepath.Join(root, "fifo.conf"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedDropIn(fd, "fifo.conf", []byte(storageMarker), uid); err == nil {
		t.Fatal("FIFO drop-in accepted")
	}
	if err := os.Link(filepath.Join(root, "storage.conf"), filepath.Join(root, "hardlink.conf")); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedDropIn(fd, "storage.conf", []byte(storageMarker), uid); err == nil {
		t.Fatal("hardlinked drop-in replaced")
	}
	if raw, e := os.ReadFile(filepath.Join(root, "manual.conf")); e != nil || !strings.Contains(string(raw), "administrator") {
		t.Fatal("manual customization changed")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatal("staged drop-in leaked")
		}
	}
}

func TestSystemdStorageConfigurationRequiresBoundedUnlinkedTrustedFile(t *testing.T) {
	root, fd := storageTestRoot(t)
	uid := os.Geteuid()
	if err := os.Mkdir(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "config/main.yaml")
	if err := os.WriteFile(file, []byte("storage:\n  data_dir: /srv/onepane\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if raw, _, err := trustedConfig(fd, "config/main.yaml", uid); err != nil || !strings.Contains(string(raw), "/srv/onepane") {
		t.Fatalf("trusted configuration unavailable: %v", err)
	}
	if err := os.Symlink("main.yaml", filepath.Join(root, "config/linked.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := trustedConfig(fd, "config/linked.yaml", uid); err == nil {
		t.Fatal("linked configuration accepted")
	}
	if err := syscall.Mkfifo(filepath.Join(root, "config/fifo.yaml"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := trustedConfig(fd, "config/fifo.yaml", uid); err == nil {
		t.Fatal("FIFO configuration accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "config/oversized.yaml"), []byte(strings.Repeat("x", maxConfigBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := trustedConfig(fd, "config/oversized.yaml", uid); err == nil {
		t.Fatal("oversized configuration accepted")
	}
	if err := os.Chmod(filepath.Join(root, "config"), 0777); err != nil {
		t.Fatal(err)
	}
	if _, _, err := trustedConfig(fd, "config/main.yaml", uid); err == nil {
		t.Fatal("writable configuration ancestor accepted")
	}
}
