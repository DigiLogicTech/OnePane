package privatekeyfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapKeyStableAndInvalidContentNeverRotated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "identity")
	one, err := Ensure(path)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Ensure(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 32 || !bytes.Equal(one, two) {
		t.Fatal("bootstrap key changed")
	}
	if err = Check(path); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("invalid original"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Ensure(path); err == nil {
		t.Fatal("invalid existing key accepted")
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != "invalid original" {
		t.Fatal("invalid existing key was overwritten")
	}
}

func TestBootstrapKeyRejectsHardLinkedKeyWithoutChangingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "original")
	payload := bytes.Repeat([]byte{42}, 32)
	if err := os.WriteFile(target, payload, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Link(target, alias); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if _, err := Ensure(alias); err == nil {
		t.Fatal("hard-linked bootstrap key accepted")
	}
	saved, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(saved, payload) {
		t.Fatal("hard-link target changed")
	}
}
