package node

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/DigiLogicTech/OnePane/internal/privatekeyfile"
)

// EnsureIdentitySeed creates a stable local node seed. It is not the eventual
// federation TLS private key; it only gives the pre-vault bootstrap a stable
// node identity. Federation keys will be managed by the Secret Broker.
func EnsureIdentitySeed(dataDir string) (string, error) {
	b, err := privatekeyfile.Ensure(filepath.Join(dataDir, "keys", "node.identity"))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
