//go:build linux

package serviceinstall

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"
	"syscall"

	"github.com/DigiLogicTech/OnePane/internal/config"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const dropInDirectory = "etc/systemd/system/onepane.service.d"
const dropInName = "10-onepane-storage.conf"
const maxConfigBytes = 64 << 10

const defaultContainersConfig = "[engine]\ncgroup_manager=\"cgroupfs\"\nevents_logger=\"file\"\n\n[containers]\nnetns=\"private\"\nipcns=\"private\"\nutsns=\"private\"\n"

// Install a default only when absent. Publication is atomic and cannot replace
// an administrator/service customization created concurrently. All operations
// stay relative to the already validated dedicated-home directory descriptor.
func ensureContainersConfig(dir, uid, gid int) error {
	return publishNewConfig(dir, "containers.conf", []byte(defaultContainersConfig), uid, gid, 0600)
}

func publishNewConfig(dir int, destination string, raw []byte, uid, gid int, mode os.FileMode) error {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	name := ".onepane-config-" + hex.EncodeToString(nonce[:]) + ".tmp"
	fd, err := syscall.Openat(dir, name, syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer syscall.Unlinkat(dir, name)
	f := os.NewFile(uintptr(fd), name)
	if err = f.Chown(uid, gid); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil {
		return err
	}
	if closed != nil {
		return closed
	}
	err = unix.Renameat2(dir, name, dir, destination, unix.RENAME_NOREPLACE)
	if err == syscall.EEXIST {
		return nil
	} // Includes links: preserve, never follow.
	if err != nil {
		return err
	}
	return syscall.Fsync(dir)
}

// InitializeSystemdConfiguration only creates an absent bootstrap configuration.
// Existing configurations are validated and preserved, including their selected
// storage roots. This mode performs no daemon/database startup or data migration.
func InitializeSystemdConfiguration(configPath, data, models string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("configuration initialization requires explicit root execution")
	}
	_, gid, home, err := serviceIdentity()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(configPath, "/") || path.Clean(configPath) != configPath {
		return fmt.Errorf("configuration requires its exact absolute path")
	}
	root, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(root)
	relative := strings.TrimPrefix(configPath, "/")
	if existing, _, e := trustedConfig(root, relative, 0); e == nil {
		_, err = parseConfig(existing)
		return err
	} else if !os.IsNotExist(e) {
		return e
	}
	cfg := config.Default()
	cfg.Storage.DataDir = data
	cfg.LocalAI.ModelPoolPath = models
	if _, err = PlanStorage(cfg, home); err != nil {
		return err
	}
	if err = config.Validate(cfg); err != nil {
		return err
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	dir, err := trustedDirectory(root, path.Dir(relative), 0, true)
	if err != nil {
		return err
	}
	defer syscall.Close(dir)
	if err = publishNewConfig(dir, path.Base(relative), raw, 0, gid, 0640); err != nil {
		return err
	}
	// Another initializer may have won. Validate what is actually present;
	// never assume our requested defaults replaced an existing configuration.
	current, _, err := trustedConfig(root, relative, 0)
	if err != nil {
		return err
	}
	_, err = parseConfig(current)
	return err
}

func parseConfig(raw []byte) (config.Config, error) {
	cfg := config.Default()
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	return cfg, config.Validate(cfg)
}

func serviceIdentity() (int, int, string, error) {
	u, err := user.Lookup("onepane")
	if err != nil {
		return 0, 0, "", fmt.Errorf("dedicated onepane service account is required: %w", err)
	}
	uid, e := strconv.Atoi(u.Uid)
	gid, e2 := strconv.Atoi(u.Gid)
	if e != nil || e2 != nil || uid <= 0 || gid <= 0 {
		return 0, 0, "", fmt.Errorf("onepane must be an unprivileged service account")
	}
	return uid, gid, u.HomeDir, nil
}

func PreviewSystemdStorage(configPath string) (StoragePlan, error) {
	_, _, home, err := serviceIdentity()
	if err != nil {
		return StoragePlan{}, err
	}
	fd, err := syscall.Open(configPath, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return StoragePlan{}, err
	}
	f := os.NewFile(uintptr(fd), configPath)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return StoragePlan{}, fmt.Errorf("configuration must be a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil || len(raw) > maxConfigBytes {
		return StoragePlan{}, fmt.Errorf("configuration exceeds read bounds")
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		return StoragePlan{}, err
	}
	p, err := PlanStorage(cfg, home)
	if err != nil {
		return p, err
	}
	sum := sha256.Sum256(raw)
	p.ConfigSHA256 = hex.EncodeToString(sum[:])
	return p, nil
}

func parts(value string) []string { return strings.Split(strings.TrimPrefix(value, "/"), "/") }
func dup(fd int) (int, error)     { return syscall.Dup(fd) }

// Every walk uses directory descriptors and O_NOFOLLOW. A service-writable
// ancestor cannot redirect a privileged mkdir/chown through a symlink race.
func walkStorage(root int, relative string, uid, gid, rootOwner int, create bool) (int, error) {
	fd, err := dup(root)
	if err != nil {
		return -1, err
	}
	components := parts(relative)
	for i, component := range components {
		if component == "" || component == "." || component == ".." {
			syscall.Close(fd)
			return -1, fmt.Errorf("invalid storage component")
		}
		next, e := syscall.Openat(fd, component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		made := false
		if e == syscall.ENOENT && create {
			if e = syscall.Mkdirat(fd, component, 0700); e == nil {
				made = true
			} else if e != syscall.EEXIST {
				syscall.Close(fd)
				return -1, e
			}
			next, e = syscall.Openat(fd, component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		}
		syscall.Close(fd)
		if e != nil {
			return -1, fmt.Errorf("storage directory is unavailable or linked: %w", e)
		}
		fd = next
		if made {
			if e = syscall.Fchown(fd, uid, gid); e == nil {
				e = syscall.Fchmod(fd, 0700)
			}
			if e != nil {
				syscall.Close(fd)
				return -1, e
			}
		}
		var st syscall.Stat_t
		if e = syscall.Fstat(fd, &st); e != nil {
			syscall.Close(fd)
			return -1, e
		}
		owned := int(st.Uid) == uid && st.Mode&0002 == 0 && (st.Mode&0020 == 0 || int(st.Gid) == gid)
		trustedAncestor := i < len(components)-1 && int(st.Uid) == rootOwner && st.Mode&0022 == 0
		if !owned && !trustedAncestor {
			syscall.Close(fd)
			return -1, fmt.Errorf("storage must use trusted ancestors and dedicated onepane-owned directories; existing ownership is preserved")
		}
	}
	return fd, nil
}

func trustedDirectory(root int, relative string, owner int, create bool) (int, error) {
	fd, err := dup(root)
	if err != nil {
		return -1, err
	}
	for _, component := range parts(relative) {
		if component == "" || component == "." || component == ".." {
			syscall.Close(fd)
			return -1, fmt.Errorf("invalid trusted directory component")
		}
		next, e := syscall.Openat(fd, component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if e == syscall.ENOENT && create {
			if e = syscall.Mkdirat(fd, component, 0755); e != nil && e != syscall.EEXIST {
				syscall.Close(fd)
				return -1, e
			}
			next, e = syscall.Openat(fd, component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		}
		syscall.Close(fd)
		if e != nil {
			return -1, e
		}
		fd = next
		var st syscall.Stat_t
		if e = syscall.Fstat(fd, &st); e != nil || int(st.Uid) != owner || st.Mode&0022 != 0 {
			syscall.Close(fd)
			return -1, fmt.Errorf("configuration ancestors must be trusted, private and unlinked")
		}
	}
	return fd, nil
}

func trustedConfig(root int, relative string, owner int) ([]byte, os.FileInfo, error) {
	parent, err := trustedDirectory(root, path.Dir(relative), owner, false)
	if err != nil {
		return nil, nil, err
	}
	defer syscall.Close(parent)
	fd, err := syscall.Openat(parent, path.Base(relative), syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), relative)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || int(st.Uid) != owner || st.Nlink != 1 || info.Mode().Perm()&0022 != 0 {
		return nil, nil, fmt.Errorf("configuration must be a root-owned private regular file with one link")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil || len(raw) > maxConfigBytes {
		return nil, nil, fmt.Errorf("configuration exceeds read bounds")
	}
	return raw, info, nil
}

func writeManagedDropIn(dir int, name string, raw []byte, owner int) error {
	existing, err := syscall.Openat(dir, name, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err == nil {
		f := os.NewFile(uintptr(existing), name)
		info, e := f.Stat()
		var old []byte
		if e == nil {
			old, e = io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
		}
		f.Close()
		if e != nil {
			return e
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || int(st.Uid) != owner || st.Nlink != 1 || info.Mode().Perm()&0022 != 0 || !bytes.HasPrefix(old, []byte(storageMarker)) {
			return fmt.Errorf("existing storage drop-in is not an owned OnePane-managed file; preserve and reconcile it manually")
		}
	} else if err != syscall.ENOENT {
		return err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".onepane-storage-" + hex.EncodeToString(nonce[:]) + ".tmp"
	fd, err := syscall.Openat(dir, temporary, syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer syscall.Unlinkat(dir, temporary)
	f := os.NewFile(uintptr(fd), temporary)
	if _, err = f.Write(raw); err == nil {
		err = f.Chmod(0644)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = syscall.Renameat(dir, temporary, dir, name); err != nil {
		return err
	}
	return syscall.Fsync(dir)
}

func ConfigureSystemdStorage(configPath string) (StoragePlan, error) {
	if os.Geteuid() != 0 {
		return StoragePlan{}, fmt.Errorf("systemd storage configuration requires explicit root execution")
	}
	uid, gid, home, err := serviceIdentity()
	if err != nil {
		return StoragePlan{}, err
	}
	if !strings.HasPrefix(configPath, "/") || path.Clean(configPath) != configPath {
		return StoragePlan{}, fmt.Errorf("configuration requires its exact absolute path")
	}
	root, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return StoragePlan{}, err
	}
	defer syscall.Close(root)
	raw, original, err := trustedConfig(root, strings.TrimPrefix(configPath, "/"), 0)
	if err != nil {
		return StoragePlan{}, err
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		return StoragePlan{}, err
	}
	p, err := PlanStorage(cfg, home)
	if err != nil {
		return p, err
	}
	sum := sha256.Sum256(raw)
	p.ConfigSHA256 = hex.EncodeToString(sum[:])
	type heldDirectory struct {
		path string
		fd   int
		stat syscall.Stat_t
	}
	var held []heldDirectory
	defer func() {
		for _, h := range held {
			syscall.Close(h.fd)
		}
	}()
	for _, selected := range []string{p.Home, p.Data, p.Models, p.Projects} {
		fd, e := walkStorage(root, strings.TrimPrefix(selected, "/"), uid, gid, 0, true)
		if e != nil {
			return StoragePlan{}, e
		}
		var st syscall.Stat_t
		if e = syscall.Fstat(fd, &st); e != nil {
			syscall.Close(fd)
			return StoragePlan{}, e
		}
		held = append(held, heldDirectory{selected, fd, st})
	}
	for _, h := range held {
		fd, e := walkStorage(root, strings.TrimPrefix(h.path, "/"), uid, gid, 0, false)
		if e != nil {
			return StoragePlan{}, e
		}
		var now syscall.Stat_t
		e = syscall.Fstat(fd, &now)
		syscall.Close(fd)
		if e != nil || now.Dev != h.stat.Dev || now.Ino != h.stat.Ino {
			return StoragePlan{}, fmt.Errorf("storage path changed during configuration")
		}
	}
	containers, err := walkStorage(root, strings.TrimPrefix(path.Join(p.Home, ".config", "containers"), "/"), uid, gid, 0, true)
	if err != nil {
		return StoragePlan{}, err
	}
	err = ensureContainersConfig(containers, uid, gid)
	syscall.Close(containers)
	if err != nil {
		return StoragePlan{}, err
	}
	current, info, err := trustedConfig(root, strings.TrimPrefix(configPath, "/"), 0)
	if err != nil || !os.SameFile(original, info) || !bytes.Equal(raw, current) {
		return StoragePlan{}, fmt.Errorf("configuration changed while storage was prepared")
	}
	dir, err := trustedDirectory(root, dropInDirectory, 0, true)
	if err != nil {
		return StoragePlan{}, err
	}
	defer syscall.Close(dir)
	if err = writeManagedDropIn(dir, dropInName, []byte(p.DropIn()), 0); err != nil {
		return StoragePlan{}, err
	}
	return p, nil
}
