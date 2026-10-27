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
	for _, name := range []string{"preinst", "postinst"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "packaging", "debian", name))
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
			t.Fatal("preinst/postinst recovery validation drift")
		}
		common = part
	}
	return common
}

func TestPackageRecoveryProtectedSnapshotAndPointers(t *testing.T) {
	common := packageRecoveryCommon(t)
	if os.Geteuid() != 0 {
		if os.Getenv("ONEPANE_REQUIRE_ROOT_RECOVERY_TESTS") == "1" {
			t.Fatal("mandatory root fixture tests require root")
		}
		t.Skip("privileged disposable-filesystem source tests run in the explicit hosted Linux root step")
	}
	// Every write is inside this new Go-managed temporary directory. Only the
	// marked helper definitions are executed: no maintainer script main, account,
	// service, installed binary, application data or model is touched.
	root := t.TempDir()
	if err := os.Chmod(filepath.Dir(root), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
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
