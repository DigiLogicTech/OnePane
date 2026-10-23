package node

import (
	"github.com/DigiLogicTech/OnePane/internal/privatekeyfile"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureIdentitySeedIsStable(t *testing.T) {
	dir := t.TempDir()
	got1, err := EnsureIdentitySeed(dir)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := EnsureIdentitySeed(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got1 != got2 {
		t.Fatalf("identity changed: %s != %s", got1, got2)
	}
	info, err := os.Stat(filepath.Join(dir, "keys", "node.identity"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("identity mode=%o want 600", info.Mode().Perm())
	}
	if err := privatekeyfile.Check(filepath.Join(dir, "keys", "node.identity")); err != nil {
		t.Fatalf("identity access control: %v", err)
	}
}
