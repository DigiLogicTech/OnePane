package nodefederation

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
 "errors"
 "io"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
 "runtime"
 "strings"
 "unicode"
	"time"
)

type Identity struct {
	Certificate tls.Certificate
	CertPEM     []byte
	Fingerprint string
}

func EnsureIdentity(dataDir, nodeID string) (Identity, error) {
	if len(nodeID)<1||len(nodeID)>128||strings.TrimSpace(nodeID)!=nodeID||
  strings.IndexFunc(nodeID,unicode.IsControl)>=0{
  return Identity{},fmt.Errorf("invalid Node identity id")
 }
	dir := filepath.Join(dataDir, "keys")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Identity{}, err
	}
	certPath := filepath.Join(dir, "federation.crt")
	keyPath := filepath.Join(dir, "federation.key")
	// Reject linked or broadly accessible identity directories. A link
	// could redirect key creation or loading outside the intended Node.
	dirInfo, err := os.Lstat(dir)
	if err != nil || !dirInfo.IsDir() || (runtime.GOOS != "windows" && dirInfo.Mode().Perm()&0o077 != 0) {
		return Identity{}, fmt.Errorf("insecure Node identity directory: %v", err)
	}
	certInfo, certErr := os.Lstat(certPath)
	keyInfo, keyErr := os.Lstat(keyPath)
	if certErr != nil && !errors.Is(certErr, os.ErrNotExist) { return Identity{}, certErr }
	if keyErr != nil && !errors.Is(keyErr, os.ErrNotExist) { return Identity{}, keyErr }
	certExists, keyExists := certErr == nil, keyErr == nil
	if certExists != keyExists {
		return Identity{}, fmt.Errorf("incomplete Node identity; refusing to replace an existing credential")
	}
	if certExists && (!certInfo.Mode().IsRegular() || !keyInfo.Mode().IsRegular() ||
		(runtime.GOOS != "windows" && keyInfo.Mode().Perm()&0o077 != 0)) {
		return Identity{}, fmt.Errorf("unsafe Node certificate or private key file")
	}
	if !certExists {
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
		// Create exclusively and durably: never truncate a key that another
		// process (or a raced symlink) installed after the initial checks.
		// A crash leaves a partial pair that the next startup refuses.
		if err := writeNewIdentityFile(keyPath, keyPEM, 0o600); err != nil {
			return Identity{}, err
		}
		if err := writeNewIdentityFile(certPath, certPEM, 0o644); err != nil {
			return Identity{}, err
		}
	}
	certPEM, err := readIdentityFile(certPath, false)
 if err != nil { return Identity{}, err }
 keyPEM, err := readIdentityFile(keyPath, true)
 if err != nil { return Identity{}, err }
 // Parse the exact bytes already checked and read from stable open files.
 // Loading by pathname a second time permits path/symlink replacement
 // between an Lstat permission check and the credential read.
 cert, err := tls.X509KeyPair(certPEM, keyPEM)
 if err != nil { return Identity{}, err }
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

func writeNewIdentityFile(path string, payload []byte, mode os.FileMode) error {
 f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,mode)
 if err!=nil{return err}
 if _,err=f.Write(payload);err!=nil{_ =f.Close();return err}
 if err=f.Sync();err!=nil{_ =f.Close();return err}
 return f.Close()
}


// readIdentityFile checks that the inode read is the same regular file that
// passed lstat. Bound credential reads to avoid a hostile/accidental large
// file exhausting a small host. A symlink swap before Open changes inode;
// a swap after Open cannot change the already-open file descriptor.
func readIdentityFile(path string, private bool) ([]byte, error) {
 const maxIdentityFileBytes int64 = 16 * 1024
 before, err := os.Lstat(path)
 if err != nil { return nil, err }
 if !before.Mode().IsRegular() || before.Size()<1 || before.Size()>maxIdentityFileBytes {
  return nil, fmt.Errorf("Node credential must be an ordinary bounded file")
 }
 if private && runtime.GOOS != "windows" && before.Mode().Perm()&0o077 != 0 {
  return nil, fmt.Errorf("Node private key must be owner-only")
 }
 f, err := os.Open(path)
 if err != nil { return nil, err }
 defer f.Close()
 after, err := f.Stat()
 if err != nil { return nil, err }
 if !after.Mode().IsRegular() || !os.SameFile(before, after) ||
  after.Size()<1 || after.Size()>maxIdentityFileBytes {
  return nil, fmt.Errorf("Node credential changed before read")
 }
 if private && runtime.GOOS != "windows" && after.Mode().Perm()&0o077 != 0 {
  return nil, fmt.Errorf("Node private key became accessible to other users")
 }
 contents, err := io.ReadAll(io.LimitReader(f, maxIdentityFileBytes+1))
 if err != nil { return nil, err }
 if len(contents)==0 || int64(len(contents))>maxIdentityFileBytes {
  return nil, fmt.Errorf("Node credential exceeds safe file bounds")
 }
 return contents, nil
}
