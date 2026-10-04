package nodefederation

import (
	"crypto/x509"
	"testing"
)

func TestEnsureIdentityStableAndBoundToNode(t *testing.T) {
	dir := t.TempDir()
	a, err := EnsureIdentity(dir, "node-test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := EnsureIdentity(dir, "node-test")
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint != b.Fingerprint {
		t.Fatalf("fingerprint changed: %s != %s", a.Fingerprint, b.Fingerprint)
	}
	leaf, err := x509.ParseCertificate(a.Certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "node-test" {
		t.Fatalf("CN=%q", leaf.Subject.CommonName)
	}
}

func TestEnsureIdentityRejectsNodeIdentityChange(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureIdentity(dir, "node-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureIdentity(dir, "node-b"); err == nil {
		t.Fatal("certificate silently rebound to another node")
	}
}
