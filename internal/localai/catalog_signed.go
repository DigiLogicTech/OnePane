package localai

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

const LocalAICatalogSchemaVersion = 1

type RuntimeCatalogEntry struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Backend       string `json:"backend,omitempty"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	SourceURL     string `json:"source_url"`
	SHA256        string `json:"sha256"`
	ArchiveFormat string `json:"archive_format"`
	ExecutableRel string `json:"executable_rel"`
}

type ModelCatalogEntry struct {
	ModelRef     string `json:"model_ref"`
	Quantization string `json:"quantization"`
	RuntimeName  string `json:"runtime_name"`
	SourceRef    string `json:"source_ref"`
	SourceURL    string `json:"source_url"`
	SHA256       string `json:"sha256"`
	Filename     string `json:"filename"`
	SizeBytes    int64  `json:"size_bytes"`
}

type ArtifactCatalog struct {
	SchemaVersion  int                   `json:"schema_version"`
	CatalogVersion string                `json:"catalog_version"`
	GeneratedAt    int64                 `json:"generated_at"`
	ExpiresAt      int64                 `json:"expires_at"`
	ModelSpecs     []ModelSpec           `json:"model_specs,omitempty"`
	Runtimes       []RuntimeCatalogEntry `json:"runtimes"`
	Models         []ModelCatalogEntry   `json:"models"`
}

type SignedCatalogEnvelope struct {
	KeyID        string `json:"key_id"`
	PayloadB64   string `json:"payload_b64"`
	SignatureB64 string `json:"signature_b64"`
}

type CatalogRecord struct {
	ID             string
	CatalogVersion string
	KeyID          string
	SourceURL      *string
	PayloadJSON    json.RawMessage
	PayloadSHA256  string
	SignatureB64   string
	GeneratedAt    int64
	ExpiresAt      int64
	Status         string
	ImportedAt     int64
}

type CatalogTrustStore struct {
	Keys map[string]ed25519.PublicKey
}

func NewCatalogTrustStore() *CatalogTrustStore {
	return &CatalogTrustStore{Keys: map[string]ed25519.PublicKey{}}
}

func (t *CatalogTrustStore) Add(keyID string, key ed25519.PublicKey) error {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" || len(key) != ed25519.PublicKeySize {
		return errors.New("invalid catalog trust root")
	}
	if t.Keys == nil {
		t.Keys = map[string]ed25519.PublicKey{}
	}
	cpy := append(ed25519.PublicKey(nil), key...)
	t.Keys[keyID] = cpy
	return nil
}

func validateSHA256(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

func (c ArtifactCatalog) Validate(now int64) error {
	if c.SchemaVersion != LocalAICatalogSchemaVersion || strings.TrimSpace(c.CatalogVersion) == "" {
		return errors.New("unsupported or missing local AI catalog version")
	}
	if c.GeneratedAt <= 0 || c.ExpiresAt <= c.GeneratedAt || (now > 0 && c.ExpiresAt <= now) {
		return errors.New("local AI catalog is expired or has invalid timestamps")
	}
	seenSpec := map[string]struct{}{}
	for _, spec := range c.ModelSpecs {
		if strings.TrimSpace(spec.ModelRef) == "" || strings.TrimSpace(spec.DisplayName) == "" || strings.TrimSpace(spec.Runtime) == "" || spec.ParamsB <= 0 || spec.ContextLength <= 0 || len(spec.UseCases) == 0 || len(spec.Quantizations) == 0 {
			return fmt.Errorf("invalid model specification %q", spec.ModelRef)
		}
		for _, use := range spec.UseCases {
			if !validateUseCase(use) {
				return fmt.Errorf("invalid use case in model specification %q", spec.ModelRef)
			}
		}
		key := strings.ToLower(spec.ModelRef)
		if _, ok := seenSpec[key]; ok {
			return fmt.Errorf("duplicate model specification %q", spec.ModelRef)
		}
		seenSpec[key] = struct{}{}
	}
	seenRuntime := map[string]struct{}{}
	for _, r := range c.Runtimes {
		key := strings.ToLower(strings.Join([]string{r.Name, r.Version, r.OS, r.Architecture, r.Backend}, "|"))
		if strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Version) == "" || strings.TrimSpace(r.OS) == "" || strings.TrimSpace(r.Architecture) == "" || !strings.HasPrefix(strings.ToLower(r.SourceURL), "https://") || !validateSHA256(r.SHA256) || strings.TrimSpace(r.ExecutableRel) == "" {
			return fmt.Errorf("invalid runtime catalog entry %q", r.Name)
		}
		switch r.ArchiveFormat {
		case "tar.gz", "zip", "binary":
		default:
			return fmt.Errorf("unsupported runtime archive format %q", r.ArchiveFormat)
		}
		switch strings.ToLower(strings.TrimSpace(r.Backend)) {
		case "", "cpu", "cuda", "rocm", "hip", "vulkan", "sycl", "metal", "opencl", "musa", "ascend", "cann":
		default:
			return fmt.Errorf("unsupported runtime backend %q", r.Backend)
		}
		if _, ok := seenRuntime[key]; ok {
			return fmt.Errorf("duplicate runtime catalog entry %q", key)
		}
		seenRuntime[key] = struct{}{}
	}
	seenModel := map[string]struct{}{}
	for _, m := range c.Models {
		key := strings.ToLower(strings.Join([]string{m.ModelRef, m.Quantization, m.RuntimeName}, "|"))
		if strings.TrimSpace(m.ModelRef) == "" || strings.TrimSpace(m.Quantization) == "" || strings.TrimSpace(m.RuntimeName) == "" || strings.TrimSpace(m.SourceRef) == "" || !strings.HasPrefix(strings.ToLower(m.SourceURL), "https://") || !validateSHA256(m.SHA256) || strings.TrimSpace(m.Filename) == "" || m.SizeBytes <= 0 {
			return fmt.Errorf("invalid model catalog entry %q", m.ModelRef)
		}
		if _, ok := seenModel[key]; ok {
			return fmt.Errorf("duplicate model catalog entry %q", key)
		}
		seenModel[key] = struct{}{}
	}
	return nil
}

func VerifySignedCatalog(raw []byte, trust *CatalogTrustStore, now int64) (ArtifactCatalog, []byte, SignedCatalogEnvelope, error) {
	var env SignedCatalogEnvelope
	var catalog ArtifactCatalog
	if trust == nil {
		return catalog, nil, env, errors.New("catalog trust store unavailable")
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return catalog, nil, env, fmt.Errorf("decode signed catalog: %w", err)
	}
	pub, ok := trust.Keys[strings.TrimSpace(env.KeyID)]
	if !ok || len(pub) != ed25519.PublicKeySize {
		return catalog, nil, env, fmt.Errorf("untrusted catalog signing key %q", env.KeyID)
	}
	payload, err := base64.StdEncoding.DecodeString(env.PayloadB64)
	if err != nil || len(payload) == 0 {
		return catalog, nil, env, errors.New("invalid catalog payload encoding")
	}
	sig, err := base64.StdEncoding.DecodeString(env.SignatureB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return catalog, nil, env, errors.New("invalid catalog signature encoding")
	}
	if !ed25519.Verify(pub, payload, sig) {
		return catalog, nil, env, errors.New("catalog signature verification failed")
	}
	if err := json.Unmarshal(payload, &catalog); err != nil {
		return catalog, nil, env, fmt.Errorf("decode catalog payload: %w", err)
	}
	if err := catalog.Validate(now); err != nil {
		return catalog, nil, env, err
	}
	return catalog, payload, env, nil
}

type CatalogService struct {
	db     *sql.DB
	tx     storage.Transactor
	events event.Store
	ids    id.Generator
	clock  interface{ UnixMilli() int64 }
	trust  *CatalogTrustStore
}

func NewCatalogService(db *sql.DB, tx storage.Transactor, clk interface{ UnixMilli() int64 }, trust *CatalogTrustStore) *CatalogService {
	return &CatalogService{db: db, tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk, trust: trust}
}

func (s *CatalogService) ImportSigned(ctx context.Context, raw []byte, sourceURL *string, actor *string) (CatalogRecord, error) {
	var out CatalogRecord
	if s == nil || s.db == nil || s.tx == nil || s.clock == nil {
		return out, errors.New("catalog service unavailable")
	}
	now := s.clock.UnixMilli()
	catalog, payload, env, err := VerifySignedCatalog(raw, s.trust, now)
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(payload)
	idv, err := s.ids.New("laicat")
	if err != nil {
		return out, err
	}
	out = CatalogRecord{ID: idv, CatalogVersion: catalog.CatalogVersion, KeyID: env.KeyID, SourceURL: sourceURL, PayloadJSON: append(json.RawMessage(nil), payload...), PayloadSHA256: hex.EncodeToString(sum[:]), SignatureB64: env.SignatureB64, GeneratedAt: catalog.GeneratedAt, ExpiresAt: catalog.ExpiresAt, Status: "active", ImportedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var activeID, activeVersion, activeSHA string
		var activeGeneratedAt int64
		activeErr := tx.QueryRowContext(ctx, `SELECT id,catalog_version,payload_sha256,generated_at FROM local_ai_catalogs WHERE status='active'`).Scan(&activeID, &activeVersion, &activeSHA, &activeGeneratedAt)
		switch {
		case activeErr == nil && strings.EqualFold(activeSHA, out.PayloadSHA256):
			// Periodic refreshes of the exact same signed catalog are idempotent.
			out.ID = activeID
			out.CatalogVersion = activeVersion
			out.GeneratedAt = activeGeneratedAt
			return nil
		case activeErr == nil && out.GeneratedAt <= activeGeneratedAt:
			return fmt.Errorf("catalog rollback/replay rejected: incoming generated_at=%d active generated_at=%d", out.GeneratedAt, activeGeneratedAt)
		case activeErr != nil && !errors.Is(activeErr, sql.ErrNoRows):
			return activeErr
		}

		if _, err := tx.ExecContext(ctx, `UPDATE local_ai_catalogs SET status='superseded' WHERE status='active'`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO local_ai_catalogs(id,catalog_version,key_id,source_url,payload_json,payload_sha256,signature_b64,generated_at,expires_at,status,imported_at) VALUES(?,?,?,?,?,?,?,?,?,'active',?)`, out.ID, out.CatalogVersion, out.KeyID, out.SourceURL, string(out.PayloadJSON), out.PayloadSHA256, out.SignatureB64, out.GeneratedAt, out.ExpiresAt, out.ImportedAt); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		p, _ := json.Marshal(map[string]any{"catalog_id": out.ID, "catalog_version": out.CatalogVersion, "key_id": out.KeyID, "payload_sha256": out.PayloadSHA256, "expires_at": out.ExpiresAt})
		return s.events.Append(ctx, tx, event.Event{ID: eid, Type: "local_ai.catalog_activated", AggregateType: "local_ai_catalog", AggregateID: out.ID, ActorPrincipalID: actor, Payload: p, OccurredAt: now})
	})
	return out, err
}

func (s *CatalogService) loadRecord(ctx context.Context, idv *string) (CatalogRecord, ArtifactCatalog, error) {
	var rec CatalogRecord
	var cat ArtifactCatalog
	var source sql.NullString
	var payload string
	query := `SELECT id,catalog_version,key_id,source_url,payload_json,payload_sha256,signature_b64,generated_at,expires_at,status,imported_at FROM local_ai_catalogs WHERE status='active'`
	args := []any{}
	if idv != nil {
		query = `SELECT id,catalog_version,key_id,source_url,payload_json,payload_sha256,signature_b64,generated_at,expires_at,status,imported_at FROM local_ai_catalogs WHERE id=?`
		args = append(args, *idv)
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&rec.ID, &rec.CatalogVersion, &rec.KeyID, &source, &payload, &rec.PayloadSHA256, &rec.SignatureB64, &rec.GeneratedAt, &rec.ExpiresAt, &rec.Status, &rec.ImportedAt)
	if err != nil {
		return rec, cat, err
	}
	if source.Valid {
		rec.SourceURL = &source.String
	}
	rec.PayloadJSON = json.RawMessage(payload)
	if err := json.Unmarshal([]byte(payload), &cat); err != nil {
		return rec, cat, errors.New("catalog payload is corrupt")
	}
	if err := cat.Validate(s.clock.UnixMilli()); err != nil {
		return rec, cat, err
	}
	sum := sha256.Sum256([]byte(payload))
	if !strings.EqualFold(rec.PayloadSHA256, hex.EncodeToString(sum[:])) {
		return rec, cat, errors.New("catalog payload integrity mismatch")
	}
	if rec.KeyID == BundledCatalogKeyID {
		verified, err := verifyBundledCatalogPayload(payload, s.clock.UnixMilli())
		if err != nil { return rec, cat, err }
		return rec, verified, nil
	}
	pub, ok := s.trust.Keys[rec.KeyID]
	if !ok || len(pub) != ed25519.PublicKeySize {
		return rec, cat, errors.New("catalog signing key is no longer trusted")
	}
	sig, err := base64.StdEncoding.DecodeString(rec.SignatureB64)
	if err != nil || !ed25519.Verify(pub, []byte(payload), sig) {
		return rec, cat, errors.New("catalog signature re-verification failed")
	}
	return rec, cat, nil
}

func (s *CatalogService) Active(ctx context.Context) (CatalogRecord, ArtifactCatalog, error) {
	return s.loadRecord(ctx, nil)
}

func (s *CatalogService) ByID(ctx context.Context, catalogID string) (CatalogRecord, ArtifactCatalog, error) {
	if strings.TrimSpace(catalogID) == "" {
		return CatalogRecord{}, ArtifactCatalog{}, errors.New("catalog id required")
	}
	return s.loadRecord(ctx, &catalogID)
}

func (s *CatalogService) ModelSpecifications(ctx context.Context) ([]ModelSpec, error) {
	_, cat, err := s.Active(ctx)
	if err != nil {
		return nil, err
	}
	if len(cat.ModelSpecs) == 0 {
		return nil, errors.New("active catalog does not contain model specifications")
	}
	out := make([]ModelSpec, len(cat.ModelSpecs))
	copy(out, cat.ModelSpecs)
	return out, nil
}

func (s *CatalogService) ResolvePlan(ctx context.Context, plan InstallPlan) (CatalogRecord, RuntimeManifest, ModelArtifact, error) {
	rec, _, err := s.Active(ctx)
	if err != nil {
		return CatalogRecord{}, RuntimeManifest{}, ModelArtifact{}, err
	}
	return s.ResolvePlanWithCatalog(ctx, rec.ID, plan)
}

func (s *CatalogService) ResolvePlanWithCatalog(ctx context.Context, catalogID string, plan InstallPlan) (CatalogRecord, RuntimeManifest, ModelArtifact, error) {
	var runtime RuntimeManifest
	var model ModelArtifact
	catalogRec, cat, err := s.ByID(ctx, catalogID)
	if err != nil {
		return catalogRec, runtime, model, err
	}
	var osName, arch string
	if err := s.db.QueryRowContext(ctx, `SELECT os_name,architecture FROM local_hardware_profiles WHERE id=? AND node_id=?`, plan.HardwareProfileID, plan.NodeID).Scan(&osName, &arch); err != nil {
		return catalogRec, runtime, model, err
	}
	var planRec Recommendation
	_ = json.Unmarshal(plan.PlanJSON, &planRec)
	wantedBackend := strings.ToLower(strings.TrimSpace(planRec.Placement.Backend))
	var fallback *RuntimeCatalogEntry
	for i := range cat.Runtimes {
		r := cat.Runtimes[i]
		if !strings.EqualFold(r.Name, plan.RuntimeName) || !strings.EqualFold(r.OS, osName) || !strings.EqualFold(r.Architecture, arch) {
			continue
		}
		backend := strings.ToLower(strings.TrimSpace(r.Backend))
		if backend == wantedBackend && wantedBackend != "" {
			runtime = RuntimeManifest{Name: r.Name, Version: r.Version, Backend: r.Backend, OS: r.OS, Architecture: r.Architecture, SourceURL: r.SourceURL, SHA256: r.SHA256, ArchiveFormat: r.ArchiveFormat, ExecutableRel: r.ExecutableRel}
			break
		}
		if backend == "" && fallback == nil {
			cp := r
			fallback = &cp
		}
	}
	if runtime.Name == "" && fallback != nil {
		r := *fallback
		runtime = RuntimeManifest{Name: r.Name, Version: r.Version, Backend: r.Backend, OS: r.OS, Architecture: r.Architecture, SourceURL: r.SourceURL, SHA256: r.SHA256, ArchiveFormat: r.ArchiveFormat, ExecutableRel: r.ExecutableRel}
	}
	if runtime.Name == "" {
		return catalogRec, runtime, model, fmt.Errorf("active catalog has no %s runtime for %s/%s", plan.RuntimeName, osName, arch)
	}
	for _, m := range cat.Models {
		if strings.EqualFold(m.ModelRef, plan.ModelRef) && strings.EqualFold(m.Quantization, plan.Quantization) && strings.EqualFold(m.RuntimeName, plan.RuntimeName) {
			if plan.SourceRef != "" && !strings.EqualFold(m.SourceRef, plan.SourceRef) {
				continue
			}
			model = ModelArtifact{ModelRef: m.ModelRef, SourceURL: m.SourceURL, ExpectedSHA256: m.SHA256, Filename: m.Filename, SizeBytes: m.SizeBytes}
			break
		}
	}
	if model.ModelRef == "" {
		return catalogRec, runtime, model, fmt.Errorf("active catalog has no verified artifact for %s %s", plan.ModelRef, plan.Quantization)
	}
	return catalogRec, runtime, model, nil
}

func (s *Service) RefreshCatalog(ctx context.Context, sourceURL string, actor *string) (CatalogRecord, error) {
	if s == nil || s.catalog == nil {
		return CatalogRecord{}, errors.New("local AI catalog unavailable")
	}
	sourceURL = strings.TrimSpace(sourceURL)
	if !strings.HasPrefix(strings.ToLower(sourceURL), "https://") {
		return CatalogRecord{}, errors.New("catalog refresh requires HTTPS")
	}
	dest := filepath.Join(s.dataDir, "downloads", "local-ai-catalog.json")
	fetcher := NewHTTPFetcher(8 << 20)
	if _, err := fetcher.Fetch(ctx, sourceURL, dest, ""); err != nil {
		return CatalogRecord{}, err
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		return CatalogRecord{}, err
	}
	return s.catalog.ImportSigned(ctx, raw, &sourceURL, actor)
}
