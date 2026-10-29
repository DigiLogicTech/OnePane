package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopOriginSurvivesRelaunchAndRejectsCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "port")
	first, err := listenDesktopOrigin(path)
	if err != nil {
		t.Fatal(err)
	}
	addr := first.Addr().String()
	if other, err := listenDesktopOrigin(path); err == nil {
		other.Close()
		t.Fatal("busy saved origin silently changed")
	}
	first.Close()
	second, err := listenDesktopOrigin(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.Addr().String() != addr {
		t.Fatalf("origin changed: %s -> %s", addr, second.Addr())
	}
}

func TestDesktopOriginRejectsCorruptSavedPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "port")
	for _, bad := range []string{"0", "80", "65536", "not-a-port", "18183\n123"} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if ln, err := listenDesktopOrigin(path); err == nil {
			ln.Close()
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestDesktopOriginAdoptsLastLegacyPort(t *testing.T) {
	dir := t.TempDir()
	legacy, err := listenDesktopOrigin(filepath.Join(dir, "initial"))
	if err != nil {
		t.Fatal(err)
	}
	address := legacy.Addr().String()
	legacy.Close()
	log := "desktop UI gateway listening at http://127.0.0.1:12345\n" + "desktop UI gateway listening at http://" + address + "\n"
	if err := os.WriteFile(filepath.Join(dir, "desktop.log"), []byte(log), 0600); err != nil {
		t.Fatal(err)
	}
	adopted, err := listenDesktopOrigin(filepath.Join(dir, "port"))
	if err != nil {
		t.Fatal(err)
	}
	defer adopted.Close()
	if adopted.Addr().String() != address {
		t.Fatalf("did not adopt latest legacy origin: %s", adopted.Addr())
	}
}
