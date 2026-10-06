package localai

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func signedCatalogFixture(t *testing.T, now int64) ([]byte, *CatalogTrustStore) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cat := ArtifactCatalog{
		SchemaVersion:  LocalAICatalogSchemaVersion,
		CatalogVersion: "test-1",
		GeneratedAt:    now - 1000,
		ExpiresAt:      now + 3600000,
		ModelSpecs:     []ModelSpec{{ModelRef: "example/model", DisplayName: "Example", Provider: "Example", ParamsB: 1, ContextLength: 8192, UseCases: []UseCase{UseGeneral}, QualityScore: 50, SourceRef: "hf://example/model", Runtime: "llamacpp", Quantizations: []string{"Q4_K_M"}}},
		Runtimes:       []RuntimeCatalogEntry{{Name: "llamacpp", Version: "1", OS: "linux", Architecture: "amd64", SourceURL: "https://example.invalid/llama.tar.gz", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ArchiveFormat: "tar.gz", ExecutableRel: "llama-server"}},
		Models:         []ModelCatalogEntry{{ModelRef: "example/model", Quantization: "Q4_K_M", RuntimeName: "llamacpp", SourceRef: "hf://example/model", SourceURL: "https://example.invalid/model.gguf", SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Filename: "model-Q4_K_M.gguf", SizeBytes: 1234}},
	}
	payload, err := json.Marshal(cat)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, payload)
	env, err := json.Marshal(SignedCatalogEnvelope{KeyID: "test", PayloadB64: base64.StdEncoding.EncodeToString(payload), SignatureB64: base64.StdEncoding.EncodeToString(sig)})
	if err != nil {
		t.Fatal(err)
	}
	trust := NewCatalogTrustStore()
	if err := trust.Add("test", pub); err != nil {
		t.Fatal(err)
	}
	return env, trust
}

func TestVerifySignedCatalog(t *testing.T) {
	now := int64(2_000_000)
	raw, trust := signedCatalogFixture(t, now)
	cat, payload, env, err := VerifySignedCatalog(raw, trust, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if cat.CatalogVersion != "test-1" || len(cat.ModelSpecs) != 1 || len(payload) == 0 || env.KeyID != "test" {
		t.Fatalf("unexpected catalog: %#v", cat)
	}
}

func TestVerifySignedCatalogRejectsTamper(t *testing.T) {
	now := int64(2_000_000)
	raw, trust := signedCatalogFixture(t, now)
	var env SignedCatalogEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	payload, err := base64.StdEncoding.DecodeString(env.PayloadB64)
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)/2] ^= 1
	env.PayloadB64 = base64.StdEncoding.EncodeToString(payload)
	tampered, _ := json.Marshal(env)
	if _, _, _, err := VerifySignedCatalog(tampered, trust, now); err == nil {
		t.Fatal("tampered catalog unexpectedly verified")
	}
}

func TestCatalogRejectsExpired(t *testing.T) {
	now := int64(2_000_000)
	raw, trust := signedCatalogFixture(t, now)
	if _, _, _, err := VerifySignedCatalog(raw, trust, now+4_000_000); err == nil {
		t.Fatal("expired catalog unexpectedly verified")
	}
}
