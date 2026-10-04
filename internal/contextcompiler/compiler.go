package contextcompiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
)

type Section struct {
	ID            string
	Kind          string
	Trust         string
	Authoritative bool
	Required      bool
	Historical    bool
	Priority      int
	Content       json.RawMessage
}

type ManifestEntry struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	SizeBytes int    `json:"size_bytes"`
	Included  bool   `json:"included"`
	Reason    string `json:"reason,omitempty"`
}

type Manifest struct {
	BudgetBytes int             `json:"budget_bytes"`
	UsedBytes   int             `json:"used_bytes"`
	ContextHash string          `json:"context_hash"`
	Entries     []ManifestEntry `json:"entries"`
}

type Result struct {
	Sections     []agentprotocol.ContextSection
	Manifest     Manifest
	ManifestJSON json.RawMessage
}

var (
	ErrInvalidInput     = errors.New("invalid context compiler input")
	ErrRequiredOverflow = errors.New("required authoritative context exceeds budget")
)

func Compile(maxBytes int, sections []Section) (Result, error) {
	if maxBytes <= 0 {
		return Result{}, fmt.Errorf("%w: positive byte budget required", ErrInvalidInput)
	}
	items := append([]Section(nil), sections...)
	seen := map[string]bool{}
	for _, s := range items {
		if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Kind) == "" || !agentprotocol.ValidTrustLabel(s.Trust) || len(s.Content) == 0 || !json.Valid(s.Content) || seen[s.ID] {
			return Result{}, fmt.Errorf("%w: invalid/duplicate section", ErrInvalidInput)
		}
		seen[s.ID] = true
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Required != items[j].Required {
			return items[i].Required
		}
		if items[i].Historical != items[j].Historical {
			return !items[i].Historical
		}
		if items[i].Authoritative != items[j].Authoritative {
			return items[i].Authoritative
		}
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		return items[i].ID < items[j].ID
	})
	out := Result{Manifest: Manifest{BudgetBytes: maxBytes}}
	for _, s := range items {
		section := agentprotocol.ContextSection{ID: s.ID, Kind: s.Kind, Trust: s.Trust, Authoritative: s.Authoritative, Content: append(json.RawMessage(nil), s.Content...)}
		encoded, _ := json.Marshal(section)
		size := len(encoded)
		entry := ManifestEntry{ID: s.ID, Kind: s.Kind, SizeBytes: size}
		if out.Manifest.UsedBytes+size > maxBytes {
			if s.Required {
				return Result{}, fmt.Errorf("%w: section %s", ErrRequiredOverflow, s.ID)
			}
			entry.Reason = "budget"
			out.Manifest.Entries = append(out.Manifest.Entries, entry)
			continue
		}
		entry.Included = true
		out.Manifest.UsedBytes += size
		out.Manifest.Entries = append(out.Manifest.Entries, entry)
		out.Sections = append(out.Sections, section)
	}
	canonical, _ := json.Marshal(out.Sections)
	sum := sha256.Sum256(canonical)
	out.Manifest.ContextHash = "sha256:" + hex.EncodeToString(sum[:])
	manifest, _ := json.Marshal(out.Manifest)
	out.ManifestJSON = manifest
	return out, nil
}
