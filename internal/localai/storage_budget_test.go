package localai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallableModelSpecificationsUseVerifiedQuantizationsOnly(t *testing.T) {
	cat := ArtifactCatalog{
		ModelSpecs: []ModelSpec{{
			ModelRef: "example/model", Runtime: "llamacpp",
			Quantizations: []string{"Q8_0", "Q6_K", "Q4_K_M"},
		}},
		Models: []ModelCatalogEntry{{
			ModelRef: "example/model", RuntimeName: "llamacpp", Quantization: "Q4_K_M", SizeBytes: 1234,
		}},
	}
	got := installableModelSpecifications(cat)
	if len(got) != 1 || len(got[0].Quantizations) != 1 || got[0].Quantizations[0] != "Q4_K_M" {
		t.Fatalf("installable specs=%+v", got)
	}
}

func TestFilterHardwareForCatalogDropsUnsupportedGPUBackends(t *testing.T) {
	p := HardwareProfile{
		OSName: "linux", Architecture: "amd64",
		GPUs: []GPU{{Vendor: "AMD", Name: "GPU", VRAMBytes: 8 << 30, Backend: "rocm", Backends: []string{"rocm", "vulkan"}}},
	}
	cat := ArtifactCatalog{Runtimes: []RuntimeCatalogEntry{{
		Name: "llamacpp", OS: "linux", Architecture: "amd64", Backend: "vulkan",
	}}}
	got := filterHardwareForCatalog(p, cat)
	if len(got.GPUs) != 1 || len(got.GPUs[0].Backends) != 1 || got.GPUs[0].Backends[0] != "vulkan" || got.GPUs[0].Backend != "vulkan" {
		t.Fatalf("filtered GPU=%+v", got.GPUs)
	}
}

func TestRuntimeStorageBudgetIncludesDependencies(t *testing.T) {
	entry := RuntimeCatalogEntry{
		SizeBytes: 100,
		Dependencies: []RuntimeDependencyCatalogEntry{{SizeBytes: 25}, {SizeBytes: 75}},
	}
	if got := runtimeDownloadBudget(entry); got != 200 {
		t.Fatalf("download budget=%d", got)
	}
	if got := runtimeInstallReserve(200); got != 256<<20 {
		t.Fatalf("minimum install reserve=%d", got)
	}
	const large = int64(300 << 20)
	if got := runtimeInstallReserve(large); got != 4*large {
		t.Fatalf("scaled install reserve=%d", got)
	}
}

func TestRemainingArtifactBytesUsesPartialProgress(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "model.gguf")
	if err := os.WriteFile(dest+".partial", make([]byte, 400), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := remainingArtifactBytes(dest, 1000); got != 600 {
		t.Fatalf("remaining=%d", got)
	}
	if err := os.WriteFile(dest, make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := remainingArtifactBytes(dest, 1000); got != 0 {
		t.Fatalf("completed remaining=%d", got)
	}
}
