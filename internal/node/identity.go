package node

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// EnsureIdentitySeed creates a stable local node seed. It is not the eventual
// federation TLS private key; it only gives the pre-vault bootstrap a stable
// node identity. Federation keys will be managed by the Secret Broker.
func EnsureIdentitySeed(dataDir string) (string, error) {
	keyDir := filepath.Join(dataDir, "keys")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		return "", fmt.Errorf("create key directory: %w", err)
	}
	path := filepath.Join(keyDir, "node.identity")

	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read node identity: %w", err)
	}
	if os.IsNotExist(err) {
		b = make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("generate node identity: %w", err)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			// Another process may have won the bootstrap race.
			if os.IsExist(err) {
				b, err = os.ReadFile(path)
				if err != nil {
					return "", fmt.Errorf("read concurrently-created node identity: %w", err)
				}
			} else {
				return "", fmt.Errorf("create node identity: %w", err)
			}
		} else {
			if _, err := f.Write(b); err != nil {
				_ = f.Close()
				return "", fmt.Errorf("write node identity: %w", err)
			}
			if err := f.Sync(); err != nil {
				_ = f.Close()
				return "", fmt.Errorf("sync node identity: %w", err)
			}
			if err := f.Close(); err != nil {
				return "", fmt.Errorf("close node identity: %w", err)
			}
		}
	}
	if len(b) != 32 {
		return "", fmt.Errorf("invalid node identity length: got %d bytes", len(b))
	}

	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
