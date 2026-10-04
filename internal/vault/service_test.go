package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureMasterKeyStableAndPrivate(t *testing.T) {
	d := t.TempDir()
	a, err := EnsureMasterKey(d)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EnsureMasterKey(d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) || len(a) != 32 {
		t.Fatal("master key not stable")
	}
	st, err := os.Stat(filepath.Join(d, "vault", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0077 != 0 {
		t.Fatalf("unsafe key mode %o", st.Mode().Perm())
	}
}
func TestEnvelopeRoundTrip(t *testing.T) {
	k := bytes.Repeat([]byte{7}, 32)
	n, c, err := seal(k, []byte("secret"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := open(k, n, c, []byte("aad"))
	if err != nil || string(p) != "secret" {
		t.Fatal("roundtrip failed")
	}
	if _, err := open(k, n, c, []byte("wrong")); err == nil {
		t.Fatal("AAD mismatch should fail")
	}
}
