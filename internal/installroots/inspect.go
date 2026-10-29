package installroots

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const identityBytes int64 = 32

// InspectIdentityDir looks at dataDir/keys/node.identity and
// dataDir/vault/master.key. It stats only. It does not open, rewrite,
// truncate, or delete either file.
func InspectIdentityDir(dataDir string) PartialFinding {
	dataDir = filepath.Clean(strings.TrimSpace(dataDir))
	checks := []string{
		filepath.Join(dataDir, "keys", "node.identity"),
		filepath.Join(dataDir, "vault", "master.key"),
	}
	var unread PartialFinding
	for _, path := range checks {
		finding, ok := inspectFile(dataDir, path)
		if !ok {
			continue
		}
		if finding.Kind == PartialUnreadable && !unread.Partial {
			unread = finding
			continue
		}
		if finding.Partial && finding.Kind != PartialUnreadable {
			return finding
		}
	}
	return unread
}

// InspectInstallerDataRoot inspects <root>/data, the Windows service layout.
func InspectInstallerDataRoot(root string) PartialFinding {
	finding := InspectIdentityDir(filepath.Join(root, "data"))
	if finding.Partial {
		finding.Root = root
	}
	return finding
}

func inspectFile(root, path string) (PartialFinding, bool) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return PartialFinding{}, false
	}
	rel := path
	if r, err := filepath.Rel(root, path); err == nil {
		rel = r
	}
	finding := PartialFinding{Root: root, Artifact: rel, Partial: true}
	if err != nil {
		finding.Kind = PartialUnreadable
		return finding, true
	}
	if info.Mode()&os.ModeSymlink != 0 {
		finding.Kind = PartialReparse
		return finding, true
	}
	if info.IsDir() {
		finding.Kind = PartialDirectory
		return finding, true
	}
	if !info.Mode().IsRegular() {
		finding.Kind = PartialReparse
		return finding, true
	}
	finding.Size = info.Size()
	if finding.Size == identityBytes {
		return PartialFinding{}, false
	}
	if finding.Size == 0 {
		finding.Kind = PartialEmpty
		return finding, true
	}
	finding.Kind = PartialTruncated
	return finding, true
}
