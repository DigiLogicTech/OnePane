package nodepower

import (
	"bytes"
	"testing"
)

func TestMagicPacket(t *testing.T) {
	got, err := MagicPacket("01:23:45:67:89:ab")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 102 {
		t.Fatalf("length=%d want 102", len(got))
	}
	if !bytes.Equal(got[:6], bytes.Repeat([]byte{0xff}, 6)) {
		t.Fatal("missing WOL sync stream")
	}
	wantMAC := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab}
	for i := 0; i < 16; i++ {
		start := 6 + i*6
		if !bytes.Equal(got[start:start+6], wantMAC) {
			t.Fatalf("copy %d mismatch", i)
		}
	}
}
