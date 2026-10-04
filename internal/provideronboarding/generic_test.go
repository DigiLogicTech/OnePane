package provideronboarding

import (
	"context"
	"testing"
)

func TestProviderCatalogCoversMajorCloudSurfaces(t *testing.T) {
	ps := Builtins()
	if len(ps) < 26 {
		t.Fatalf("expected broad provider catalog, got %d", len(ps))
	}
	for _, id := range []PresetID{PresetOpenAI, PresetAnthropic, PresetGemini, PresetVertexAI, PresetAzureOpenAI, PresetBedrock, PresetXAI, PresetMistral, PresetGroq, PresetDeepSeek, PresetOpenRouter, PresetTogether, PresetFireworks, PresetCohere, PresetCloudflare, PresetNVIDIA, PresetHuggingFace, PresetAlibaba, PresetKimi, PresetMiniMax, PresetCustomOpenAI} {
		if _, ok := PresetByID(id); !ok {
			t.Fatalf("missing provider preset %s", id)
		}
	}
}

func TestFixedProviderRejectsCredentialRedirectHost(t *testing.T) {
	p, _ := PresetByID(PresetOpenAI)
	if _, err := normalizeProviderURL(p, "https://evil.example/v1"); err == nil {
		t.Fatal("expected fixed provider host pinning")
	}
}

func TestAzureAllowsResourceSubdomain(t *testing.T) {
	p, _ := PresetByID(PresetAzureOpenAI)
	got, err := normalizeProviderURL(p, "https://example-resource.openai.azure.com/openai/v1")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("missing normalized endpoint")
	}
}

func TestProviderCredentialRequiresSecretResolver(t *testing.T) {
	svc := New(nil, nil)
	p, _ := PresetByID(PresetOpenAI)
	ref := "vault:provider/openai/api-key"
	if err := svc.validateDirectCredential(context.Background(), p, &ref); err == nil {
		t.Fatal("expected missing secret resolver to fail closed")
	}
}
