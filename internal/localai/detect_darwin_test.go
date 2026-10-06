package localai

import (
	"context"
	"fmt"
	"testing"
)

type darwinFakeRunner struct {
	outputs map[string]string
}

func (r darwinFakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name
	for _, arg := range args {
		key += " " + arg
	}
	if v, ok := r.outputs[key]; ok {
		return []byte(v), nil
	}
	return nil, fmt.Errorf("missing fake output for %s", key)
}

func TestParseDisplayMemoryDarwin(t *testing.T) {
	if got := parseDisplayMemoryDarwin("16 GB"); got != 16*1024*1024*1024 {
		t.Fatalf("16 GB = %d", got)
	}
	if got := parseDisplayMemoryDarwin("512 MB"); got != 512*1024*1024 {
		t.Fatalf("512 MB = %d", got)
	}
}

func TestAvailableMemoryDarwin(t *testing.T) {
	r := darwinFakeRunner{outputs: map[string]string{
		"vm_stat": "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free: 10.\nPages active: 100.\nPages inactive: 20.\nPages speculative: 5.\n",
	}}
	want := int64(35 * 16384)
	if got := availableMemoryDarwin(context.Background(), r); got != want {
		t.Fatalf("available memory = %d, want %d", got, want)
	}
}
