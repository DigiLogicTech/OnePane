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

func TestSystemdStorageContainersDefaultsNeverReplaceExistingCustomization(t *testing.T) {
	root, fd := storageTestRoot(t)
	uid, gid := os.Geteuid(), os.Getegid()
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- ensureContainersConfig(fd, uid, gid) }()
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(root, "containers.conf")
	if raw, err := os.ReadFile(file); err != nil || string(raw) != defaultContainersConfig {
		t.Fatalf("missing complete defaults: %v", err)
	}
	if err := os.WriteFile(file, []byte("# retained operator settings\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = ensureContainersConfig(fd, uid, gid); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(file)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("existing configuration inode replaced")
	}
	if raw, e := os.ReadFile(file); e != nil || string(raw) != "# retained operator settings\n" {
		t.Fatal("existing configuration overwritten")
	}
	if err = os.Rename(file, filepath.Join(root, "selected.conf")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("selected.conf", file); err != nil {
		t.Fatal(err)
	}
	if err = ensureContainersConfig(fd, uid, gid); err != nil {
		t.Fatal(err)
	}
	if target, e := os.Readlink(file); e != nil || target != "selected.conf" {
		t.Fatal("existing configuration link replaced")
	}
	if raw, e := os.ReadFile(filepath.Join(root, "selected.conf")); e != nil || string(raw) != "# retained operator settings\n" {
		t.Fatal("linked customization modified")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatal("default staging file leaked")
		}
	}
}

func TestSystemdStorageBootstrapPublishPreservesSelectedConfiguration(t *testing.T) {
	root, fd := storageTestRoot(t)
	uid, gid := os.Geteuid(), os.Getegid()
	original := []byte("storage:\n  data_dir: /mnt/retained-data\nlocal_ai:\n  model_pool_path: /mnt/retained-models\n")
	if err := publishNewConfig(fd, "config.yaml", original, uid, gid, 0640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filepath.Join(root, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode().Perm() != 0640 {
		t.Fatal("new bootstrap configuration lacks selected privacy mode")
	}
	if err = publishNewConfig(fd, "config.yaml", []byte("storage:\n  data_dir: /srv/replacement\n"), uid, gid, 0600); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filepath.Join(root, "config.yaml"))
	if err != nil || !os.SameFile(before, after) || after.Mode().Perm() != 0640 {
		t.Fatal("existing bootstrap configuration identity/permissions replaced")
	}
	raw, err := os.ReadFile(filepath.Join(root, "config.yaml"))
	if err != nil || string(raw) != string(original) {
		t.Fatal("selected configuration overwritten")
	}
	cfg, err := parseConfig(raw)
	if err != nil || cfg.Storage.DataDir != "/mnt/retained-data" || cfg.LocalAI.ModelPoolPath != "/mnt/retained-models" {
		t.Fatalf("selected roots not retained: %v", err)
	}
}

func TestSystemdStorageMutationsRequireRootBeforeAccountLookup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("non-root denial requires an unprivileged test process")
	}
	file := filepath.Join(t.TempDir(), "must-not-exist.yaml")
	if err := InitializeSystemdConfiguration(file, "/srv/onepane/data", "/srv/onepane/models"); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatal("unprivileged initialization was not denied")
	}
	if _, err := ConfigureSystemdStorage(file); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatal("unprivileged configuration was not denied")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("denied mode created a configuration")
	}
}
