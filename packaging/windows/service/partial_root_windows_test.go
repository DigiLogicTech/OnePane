//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPartialInstallerRootIsReportedAndPreserved(t *testing.T) {
	root := t.TempDir()
	identity := filepath.Join(root, "data", "keys", "node.identity")
	if err := os.MkdirAll(filepath.Dir(identity), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identity, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := rejectPartialDataRoot(root)
	if err == nil || !strings.Contains(err.Error(), "will not regenerate") {
		t.Fatalf("partial root error = %v", err)
	}
	info, statErr := os.Stat(identity)
	if statErr != nil || info.Size() != 0 {
		t.Fatalf("empty identity changed: %v %+v", statErr, info)
	}
	if _, statErr = os.Stat(filepath.Join(root, "config.yaml")); !os.IsNotExist(statErr) {
		t.Fatal("partial root gained a config file")
	}
	if err := os.WriteFile(identity, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rejectPartialDataRoot(root); err != nil {
		t.Fatal(err)
	}
}
