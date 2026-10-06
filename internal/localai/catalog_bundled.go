package localai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

const BundledCatalogKeyID = "onepane-bundled-pinned-v1"

func BundledArtifactCatalog() ArtifactCatalog {
	return ArtifactCatalog{
		SchemaVersion:  LocalAICatalogSchemaVersion,
		CatalogVersion: "onepane-alpha3.2-runtime-deps-2026-10-06",
		GeneratedAt:    1791244800000,
		ExpiresAt:      2082758400000,
		ModelSpecs:     BuiltinCatalog(),
		Runtimes: []RuntimeCatalogEntry{
			{Name: "llamacpp", Version: "b11430", Backend: "cpu", OS: "windows", Architecture: "amd64", SourceURL: "https://github.com/ggml-org/llama.cpp/releases/download/b11430/llama-b11430-bin-win-cpu-x64.zip", SHA256: "b608455b0109793f774537d63d15d4cf2098ddbc5b200ebc9a648e1d85369666", ArchiveFormat: "zip", ExecutableRel: "llama-server.exe"},
			{Name: "llamacpp", Version: "b11430", Backend: "cuda", OS: "windows", Architecture: "amd64", SourceURL: "https://github.com/ggml-org/llama.cpp/releases/download/b11430/llama-b11430-bin-win-cuda-12.4-x64.zip", SHA256: "dac7ab2c98174c5b7a632bb4b0a57c30972b8beaf699133ca0a31b7eb86ac89f", ArchiveFormat: "zip", ExecutableRel: "llama-server.exe", Dependencies: []RuntimeDependencyCatalogEntry{{Name: "cuda-runtime-12.4", SourceURL: "https://github.com/ggml-org/llama.cpp/releases/download/b11430/cudart-llama-bin-win-cuda-12.4-x64.zip", SHA256: "8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6", ArchiveFormat: "zip"}}},
			{Name: "llamacpp", Version: "b11430", Backend: "vulkan", OS: "windows", Architecture: "amd64", SourceURL: "https://github.com/ggml-org/llama.cpp/releases/download/b11430/llama-b11430-bin-win-vulkan-x64.zip", SHA256: "fea0653c6eab7e3e1abfa4785d033db831818b483d6912e372d2c1a99babed0a", ArchiveFormat: "zip", ExecutableRel: "llama-server.exe"},
			{Name: "llamacpp", Version: "b11430", Backend: "cpu", OS: "linux", Architecture: "amd64", SourceURL: "https://github.com/ggml-org/llama.cpp/releases/download/b11430/llama-b11430-bin-ubuntu-x64.tar.gz", SHA256: "1b898a3b23df35cc6c3e93c3adba5c63ec31d48bee8e5f30639e9372836ec2f2", ArchiveFormat: "tar.gz", ExecutableRel: "llama-b11430/llama-server"},
			{Name: "llamacpp", Version: "b11430", Backend: "cuda", OS: "linux", Architecture: "amd64", SourceURL: "https://github.com/ggml-org/llama.cpp/releases/download/b11430/llama-b11430-bin-ubuntu-cuda-12.8-x64.tar.gz", SHA256: "8f79f75093e8fb6d9c65e59167511e9547776794191073e488ce81efd5ade723", ArchiveFormat: "tar.gz", ExecutableRel: "llama-b11430/llama-server", Dependencies: []RuntimeDependencyCatalogEntry{{Name: "cuda-runtime-12.8", SourceURL: "https://github.com/ggml-org/llama.cpp/releases/download/b11430/cudart-llama-b11430-bin-ubuntu-cuda-12.8-x64.tar.gz", SHA256: "0db934433d96342cf30b2c85b0673d89537b1d0d029a3bf28ccb4db68622e8ca", ArchiveFormat: "tar.gz"}}},
			{Name: "llamacpp", Version: "b11430", Backend: "vulkan", OS: "linux", Architecture: "amd64", SourceURL: "https://github.com/ggml-org/llama.cpp/releases/download/b11430/llama-b11430-bin-ubuntu-vulkan-x64.tar.gz", SHA256: "99652a5a753b4045a885c5c3f5565c05a59d0391ac2cb207a327c7d0f72917f2", ArchiveFormat: "tar.gz", ExecutableRel: "llama-b11430/llama-server"},
		},
		Models: []ModelCatalogEntry{
			{ModelRef: "google/gemma-3-1b-it", Quantization: "Q4_K_M", RuntimeName: "llamacpp", SourceRef: "hf://google/gemma-3-1b-it", SourceURL: "https://huggingface.co/bartowski/google_gemma-3-1b-it-GGUF/resolve/main/google_gemma-3-1b-it-Q4_K_M.gguf", SHA256: "12bf0fff8815d5f73a3c9b586bd8fee8e7b248c935de70dec367679873d0f29d", Filename: "google_gemma-3-1b-it-Q4_K_M.gguf", SizeBytes: 845000000},
			{ModelRef: "microsoft/Phi-4-mini-instruct", Quantization: "Q4_K_M", RuntimeName: "llamacpp", SourceRef: "hf://microsoft/Phi-4-mini-instruct", SourceURL: "https://huggingface.co/bartowski/microsoft_Phi-4-mini-instruct-GGUF/resolve/main/microsoft_Phi-4-mini-instruct-Q4_K_M.gguf", SHA256: "01999f17c39cc3074afae5e9c539bc82d45f2dd7faa3917c66cbef76fce8c0c2", Filename: "microsoft_Phi-4-mini-instruct-Q4_K_M.gguf", SizeBytes: 2490000000},
			{ModelRef: "Qwen/Qwen2.5-Coder-7B-Instruct", Quantization: "Q4_K_M", RuntimeName: "llamacpp", SourceRef: "hf://Qwen/Qwen2.5-Coder-7B-Instruct", SourceURL: "https://huggingface.co/bartowski/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/Qwen2.5-Coder-7B-Instruct-Q4_K_M.gguf", SHA256: "0d10372614925f17c1fd0b9f987d9c542a6f9b1548f83621b70c3ce8e9d3c1d8", Filename: "Qwen2.5-Coder-7B-Instruct-Q4_K_M.gguf", SizeBytes: 4680000000},
		},
	}
}

func bundledCatalogPayload() ([]byte, string, error) {
	cat := BundledArtifactCatalog()
	raw, err := json.Marshal(cat)
	if err != nil { return nil, "", err }
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

func verifyBundledCatalogPayload(payload string, now int64) (ArtifactCatalog, error) {
	var cat ArtifactCatalog
	raw, want, err := bundledCatalogPayload()
	if err != nil { return cat, err }
	sum := sha256.Sum256([]byte(payload))
	if hex.EncodeToString(sum[:]) != want || string(raw) != payload {
		return cat, errors.New("bundled catalog payload differs from build-pinned catalog")
	}
	if err := json.Unmarshal([]byte(payload), &cat); err != nil { return cat, err }
	if err := cat.Validate(now); err != nil { return cat, err }
	return cat, nil
}

func (s *CatalogService) EnsureBundled(ctx context.Context) (CatalogRecord, error) {
	if rec, _, err := s.Active(ctx); err == nil { return rec, nil }
	raw, sha, err := bundledCatalogPayload()
	if err != nil { return CatalogRecord{}, err }
	var cat ArtifactCatalog
	if err := json.Unmarshal(raw, &cat); err != nil { return CatalogRecord{}, err }
	if err := cat.Validate(s.clock.UnixMilli()); err != nil { return CatalogRecord{}, err }

	now := s.clock.UnixMilli()
	idv, err := s.ids.New("laicat")
	if err != nil { return CatalogRecord{}, err }
	rec := CatalogRecord{
		ID: idv, CatalogVersion: cat.CatalogVersion, KeyID: BundledCatalogKeyID,
		PayloadJSON: json.RawMessage(raw), PayloadSHA256: sha, SignatureB64: "build-pinned",
		GeneratedAt: cat.GeneratedAt, ExpiresAt: cat.ExpiresAt, Status: "active", ImportedAt: now,
	}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE local_ai_catalogs SET status='superseded' WHERE status='active'`); err != nil { return err }
		_, err := tx.ExecContext(ctx, `INSERT INTO local_ai_catalogs(id,catalog_version,key_id,source_url,payload_json,payload_sha256,signature_b64,generated_at,expires_at,status,imported_at)
			VALUES(?,?,?,NULL,?,?,?,?,?,'active',?)`,
			rec.ID, rec.CatalogVersion, rec.KeyID, string(rec.PayloadJSON), rec.PayloadSHA256, rec.SignatureB64, rec.GeneratedAt, rec.ExpiresAt, rec.ImportedAt)
		return err
	})
	return rec, err
}

func (s *CatalogService) Installability(ctx context.Context) (map[string]map[string]bool, error) {
	_, cat, err := s.Active(ctx)
	if err != nil { return nil, err }
	out := map[string]map[string]bool{}
	for _, m := range cat.Models {
		q := out[m.ModelRef]
		if q == nil { q = map[string]bool{}; out[m.ModelRef] = q }
		q[m.Quantization] = true
	}
	return out, nil
}
