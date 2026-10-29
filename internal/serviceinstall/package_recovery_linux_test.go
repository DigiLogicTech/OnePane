//go:build linux

package serviceinstall

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func packageRecoveryCommon(t *testing.T) string {
	t.Helper()
	var common string
	for _, name := range []string{"packaging/debian/preinst", "packaging/debian/postinst", "scripts/upgrade-ubuntu.sh"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		start, end := strings.Index(text, "# ONEPANE RECOVERY COMMON BEGIN"), strings.Index(text, "# ONEPANE RECOVERY COMMON END")
		if start < 0 || end < start {
			t.Fatal("missing reviewed recovery helpers")
		}
		part := text[start:end]
		if common != "" && common != part {
			t.Fatal("package/standalone recovery validation drift")
		}
		common = part
	}
	return common
}

func packageRecoveryFixtureRoot(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		if os.Getenv("ONEPANE_REQUIRE_ROOT_RECOVERY_TESTS") == "1" {
			t.Fatal("mandatory root fixture tests require root")
		}
		t.Skip("privileged disposable-filesystem source tests run in the explicit hosted Linux root step")
	}
	root := t.TempDir()
	if err := os.Chmod(filepath.Dir(root), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPackageRecoveryProtectedSnapshotAndPointers(t *testing.T) {
	common := packageRecoveryCommon(t)
	root := packageRecoveryFixtureRoot(t)
	// Every write is inside this new Go-managed temporary directory. Only the
	// marked helper definitions are executed: no maintainer script main, account,
	// service, installed binary, application data or model is touched.
	call := func(body string, args ...string) error {
		cmdArgs := append([]string{"-c", common + "\nRECOVERY_ROOT=$1\nshift\n" + body, "recovery-fixture", root}, args...)
		out, err := exec.Command("sh", cmdArgs...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, out)
		}
		return nil
	}
	newCandidate := func(name string) string {
		t.Helper()
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		body := []byte("synthetic recovery fixture; never executed\n")
		if err := os.WriteFile(filepath.Join(dir, "onepane"), body, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "binary.sha256"), []byte(fmt.Sprintf("%x\n", sha256.Sum256(body))), 0600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	good := newCandidate("snapshot.good")
	// Equivalent unprivileged filesystem access is genuine OS execution in
	// this disposable fixture, not an installed OnePane service assertion.
	for _, access := range []string{"-x", "-w"} {
		cmd := exec.Command("/usr/bin/test", access, filepath.Join(good, "onepane"))
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534, Groups: []uint32{}}}
		err := cmd.Run()
		if (err == nil) != (access == "-x") {
			t.Fatalf("unprivileged recovery access %s: %v", access, err)
		}
	}
	if err := call(`recovery_initialize_root && recovery_candidate_valid "$1" && recovery_publish_pointer "$RECOVERY_ROOT/pending" "$1"`, good); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "pending")); err != nil || string(raw) != good+"\n" {
		t.Fatal("protected pointer did not retain exact candidate")
	}
	for _, scenario := range []string{"wrong_hash", "writable_directory", "writable_binary", "private_directory", "private_binary", "wrong_owner", "binary_symlink", "directory_symlink", "binary_hardlink", "outside_root", "traversal"} {
		t.Run(scenario, func(t *testing.T) {
			candidate := newCandidate("snapshot." + scenario)
			file := filepath.Join(candidate, "onepane")
			var err error
			switch scenario {
			case "wrong_hash":
				err = os.WriteFile(file, []byte("changed"), 0755)
			case "writable_directory":
				err = os.Chmod(candidate, 0777)
			case "writable_binary":
				err = os.Chmod(file, 0777)
			case "private_directory":
				err = os.Chmod(candidate, 0700)
			case "private_binary":
				err = os.Chmod(file, 0700)
			case "wrong_owner":
				err = os.Chown(file, 65534, 65534)
			case "binary_symlink":
				if err = os.Remove(file); err == nil {
					err = os.Symlink(filepath.Join(good, "onepane"), file)
				}
			case "directory_symlink":
				link := filepath.Join(root, "snapshot.link")
				err = os.Symlink(candidate, link)
				candidate = link
			case "binary_hardlink":
				err = os.Link(file, filepath.Join(candidate, "alias"))
			case "outside_root":
				outside := t.TempDir()
				candidate = filepath.Join(outside, "snapshot.good")
			case "traversal":
				candidate = root + "/snapshot.good/../snapshot.good"
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = call(`recovery_candidate_valid "$1"`, candidate); err == nil {
				t.Fatal("unsafe recovery candidate accepted")
			}
			if err = call(`recovery_publish_pointer "$RECOVERY_ROOT/pending" "$1"`, candidate); err == nil {
				t.Fatal("unsafe candidate published")
			}
			if raw, e := os.ReadFile(filepath.Join(root, "pending")); e != nil || string(raw) != good+"\n" {
				t.Fatal("denied candidate replaced retained pointer")
			}
		})
	}
	outside := filepath.Join(root, "retained-customization")
	if err := os.WriteFile(outside, []byte("preserve\n"), 0600); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(root, "active")
	if err := os.Symlink(outside, active); err != nil {
		t.Fatal(err)
	}
	if err := call(`recovery_publish_pointer "$RECOVERY_ROOT/active" "$1"`, good); err == nil {
		t.Fatal("linked pointer replaced")
	}
	if raw, err := os.ReadFile(outside); err != nil || string(raw) != "preserve\n" {
		t.Fatal("linked pointer destination modified")
	}
}

func TestPackageRecoveryPrivateSnapshotAndAtomicReplacement(t *testing.T) {
	common := packageRecoveryCommon(t)
	root := packageRecoveryFixtureRoot(t)
	call := func(body string, args ...string) (string, error) {
		argv := append([]string{"-c", common + "\nRECOVERY_ROOT=$1\nshift\n" + body, "snapshot-fixture", root}, args...)
		out, err := exec.Command("sh", argv...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%w: %s", err, out)
		}
		return strings.TrimSpace(string(out)), nil
	}
	bin := filepath.Join(root, "bin with spaces % quoted")
	if err := os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(bin, "onepane")
	oldBody, newBody := []byte("retained original executable fixture\n"), []byte("selected next executable fixture\n")
	if err := os.WriteFile(target, oldBody, 0755); err != nil {
		t.Fatal(err)
	}
	snapshot, err := call(`recovery_snapshot_binary "$1"`, target)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(snapshot) != root || !strings.HasPrefix(filepath.Base(snapshot), "snapshot.") {
		t.Fatal("snapshot escaped protected root")
	}
	if raw, e := os.ReadFile(filepath.Join(snapshot, "onepane")); e != nil || string(raw) != string(oldBody) {
		t.Fatal("snapshot changed prior executable bytes")
	}
	if _, err = call(`recovery_candidate_valid "$1"`, snapshot); err != nil {
		t.Fatal(err)
	}
	ingress := filepath.Join(root, ".incoming.fixture")
	if err = os.Mkdir(ingress, 0700); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(ingress, "onepane")
	if err = os.WriteFile(next, newBody, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = call(`recovery_replace_binary "$1" "$2"`, next, target); err != nil {
		t.Fatal(err)
	}
	if raw, e := os.ReadFile(target); e != nil || string(raw) != string(newBody) {
		t.Fatal("selected executable was not atomically promoted")
	}
	if raw, e := os.ReadFile(filepath.Join(snapshot, "onepane")); e != nil || string(raw) != string(oldBody) {
		t.Fatal("promotion modified retained recovery")
	}
	if _, err = call(`recovery_candidate_valid "$1" && recovery_replace_binary "$1/onepane" "$2"`, snapshot, target); err != nil {
		t.Fatal(err)
	}
	if raw, e := os.ReadFile(target); e != nil || string(raw) != string(oldBody) {
		t.Fatal("protected rollback did not restore exact prior bytes")
	}
	outside := filepath.Join(root, "retained-file")
	if err = os.WriteFile(outside, oldBody, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if _, err = call(`recovery_replace_binary "$1" "$2"`, next, target); err == nil {
		t.Fatal("linked destination accepted")
	}
	if raw, e := os.ReadFile(outside); e != nil || string(raw) != string(oldBody) {
		t.Fatal("linked destination contents modified")
	}
	if err = os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, oldBody, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(bin, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if _, err = call(`recovery_snapshot_binary "$1"`, target); err == nil {
		t.Fatal("service-writable prefix admitted as recovery source")
	}
	if _, err = call(`recovery_replace_binary "$1" "$2"`, next, target); err == nil {
		t.Fatal("service-writable prefix admitted for privileged promotion")
	}
}

func TestPackageRecoveryDataCopyRunsWithoutRootAuthority(t *testing.T) {
	_ = packageRecoveryCommon(t)
	root := packageRecoveryFixtureRoot(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", "upgrade-ubuntu.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	start, end := strings.Index(text, "# ONEPANE DATA BACKUP BEGIN"), strings.Index(text, "# ONEPANE DATA BACKUP END")
	if start < 0 || end < start {
		t.Fatal("missing source backup boundary")
	}
	body := text[start:end]
	call := func(data string) (string, error) {
		cmd := exec.Command("sh", "-eu", "-c", body, "unprivileged-data-backup", data)
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534, Groups: []uint32{}}}
		out, e := cmd.CombinedOutput()
		if e != nil {
			return "", fmt.Errorf("%w: %s", e, out)
		}
		return strings.TrimSpace(string(out)), nil
	}
	newData := func(name string) string {
		t.Helper()
		data := filepath.Join(root, name)
		for _, dir := range []string{data, filepath.Join(data, "state")} {
			if e := os.Mkdir(dir, 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.Chown(dir, 65534, 65534); e != nil {
				t.Fatal(e)
			}
		}
		return data
	}
	writeUserFile := func(file string, bytes []byte) {
		t.Helper()
		if e := os.WriteFile(file, bytes, 0600); e != nil {
			t.Fatal(e)
		}
		if e := os.Chown(file, 65534, 65534); e != nil {
			t.Fatal(e)
		}
	}
	data := newData("selected data root")
	for _, name := range []string{"harness.db", "harness.db-wal", "harness.db-shm"} {
		writeUserFile(filepath.Join(data, "state", name), []byte("synthetic retained bytes: "+name))
	}
	writeUserFile(filepath.Join(data, "model-sentinel"), []byte("preserve model storage"))
	selected, err := call(data)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(selected) != filepath.Join(data, "backups") || !strings.HasPrefix(filepath.Base(selected), "pre-upgrade.") {
		t.Fatal("backup escaped selected data root")
	}
	info, err := os.Stat(selected)
	if err != nil || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != 65534 {
		t.Fatal("data backup used root or exposed private bytes")
	}
	for _, name := range []string{"harness.db", "harness.db-wal", "harness.db-shm"} {
		original, e := os.ReadFile(filepath.Join(data, "state", name))
		if e != nil {
			t.Fatal(e)
		}
		copy, e := os.ReadFile(filepath.Join(selected, name))
		if e != nil || string(copy) != string(original) {
			t.Fatal("data copy changed source bytes")
		}
	}
	if got, e := os.ReadFile(filepath.Join(data, "model-sentinel")); e != nil || string(got) != "preserve model storage" {
		t.Fatal("backup modified unrelated model storage")
	}
	for _, scenario := range []string{"missing_database", "linked_database", "linked_state", "linked_backups"} {
		t.Run(scenario, func(t *testing.T) {
			other := newData(scenario)
			outside := filepath.Join(data, "model-sentinel")
			switch scenario {
			case "linked_database":
				if e := os.Symlink(outside, filepath.Join(other, "state/harness.db")); e != nil {
					t.Fatal(e)
				}
			case "linked_state":
				if e := os.Remove(filepath.Join(other, "state")); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(filepath.Join(data, "state"), filepath.Join(other, "state")); e != nil {
					t.Fatal(e)
				}
			case "linked_backups":
				writeUserFile(filepath.Join(other, "state/harness.db"), []byte("retained"))
				if e := os.Symlink(filepath.Join(data, "backups"), filepath.Join(other, "backups")); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := call(other); e == nil {
				t.Fatal("unsafe/absent database source reported successful backup")
			}
		})
	}
}
