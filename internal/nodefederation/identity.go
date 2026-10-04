package nodefederation

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

type Identity struct {
	Certificate tls.Certificate
	CertPEM     []byte
	Fingerprint string
}

func EnsureIdentity(dataDir, nodeID string) (Identity, error) {
	if nodeID == "" {
		return Identity{}, fmt.Errorf("node id required")
	}
	dir := filepath.Join(dataDir, "keys")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Identity{}, err
	}
	certPath := filepath.Join(dir, "federation.crt")
	keyPath := filepath.Join(dir, "federation.key")
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return Identity{}, err
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
		if err != nil {
			return Identity{}, err
		}
		now := time.Now().UTC()
		tpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: nodeID, Organization: []string{"OnePane"}}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true}
		der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
		if err != nil {
			return Identity{}, err
		}
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return Identity{}, err
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
		if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
			return Identity{}, err
		}
		if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
			return Identity{}, err
		}
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return Identity{}, err
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return Identity{}, err
	}
	if len(cert.Certificate) == 0 {
		return Identity{}, fmt.Errorf("federation certificate empty")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return Identity{}, err
	}
	if leaf.Subject.CommonName != nodeID {
		return Identity{}, fmt.Errorf("federation certificate node mismatch: %s", leaf.Subject.CommonName)
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return Identity{Certificate: cert, CertPEM: certPEM, Fingerprint: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

func CertificateFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(sum[:])
}
