package id

import (
	"strings"
	"testing"
)

func TestGeneratorProducesUUIDv7(t *testing.T) {
	got, err := (Generator{}).New("task")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "task_") {
		t.Fatalf("missing prefix: %s", got)
	}
	u := strings.TrimPrefix(got, "task_")
	if len(u) != 36 {
		t.Fatalf("uuid length=%d want 36", len(u))
	}
	if u[14] != '7' {
		t.Fatalf("uuid version nibble=%q want 7", u[14])
	}
	if !strings.ContainsRune("89ab", rune(u[19])) {
		t.Fatalf("invalid RFC variant nibble %q", u[19])
	}
}
