package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/config"
)

func TestOpenLeavesPartialIdentityUntouched(t *testing.T) {
	root := t.TempDir()
	identity := filepath.Join(root, "keys", "node.identity")
	if err := os.MkdirAll(filepath.Dir(identity), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identity, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Storage.DataDir = root
	_, err := Open(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "will not regenerate") {
		t.Fatalf("Open error = %v", err)
	}
	info, statErr := os.Stat(identity)
	if statErr != nil || info.Size() != 0 {
		t.Fatalf("identity changed: %v size=%d", statErr, info.Size())
	}
	if _, statErr = os.Stat(filepath.Join(root, "state", "harness.db")); !os.IsNotExist(statErr) {
		t.Fatalf("partial root created a database: %v", statErr)
	}
}
