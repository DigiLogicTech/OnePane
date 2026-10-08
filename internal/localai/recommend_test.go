package localai

import "testing"

func testProfile() HardwareProfile {
	p := HardwareProfile{NodeID: "node1", OSName: "linux", Architecture: "amd64", CPU: CPU{Name: "x", LogicalCores: 8, Architecture: "amd64"}, Memory: Memory{TotalBytes: 64 << 30, AvailableBytes: 55 << 30}, GPUs: []GPU{{Vendor: "nvidia", Name: "gpu", VRAMBytes: 24 << 30, Backend: "cuda"}}, Storage: Storage{Path: "/var/lib/onepane", CapacityBytes: 1000 << 30, AvailableBytes: 500 << 30}}
	p.Fingerprint, _ = p.ComputeFingerprint()
	return p
}
func TestRecommendFitsAndRespectsStorage(t *testing.T) {
	out, err := Recommend(testProfile(), BuiltinCatalog(), RecommendRequest{UseCase: UseCoding, ContextTokens: 8192, Limit: 3, MinimumFit: FitGood, StorageHeadroomPct: 15, PreferGPU: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("expected recommendation")
	}
	if out[0].FitLevel == FitTooTight {
		t.Fatal("too-tight model returned")
	}
	if out[0].DiskRequired+out[0].DownloadScratch > int64(float64(500<<30)*.85) {
		t.Fatal("storage headroom ignored")
	}
}
func TestRecommendRejectsDiskPressure(t *testing.T) {
	p := testProfile()
	p.Storage.AvailableBytes = 1 << 30
	out, err := Recommend(p, BuiltinCatalog(), RecommendRequest{UseCase: UseCoding, ContextTokens: 8192, Limit: 3, MinimumFit: FitMarginal, StorageHeadroomPct: 15})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("expected no fits under disk pressure, got %d", len(out))
	}
}
func TestRoutingCanRecommendSmallModel(t *testing.T) {
	out, err := Recommend(testProfile(), BuiltinCatalog(), RecommendRequest{UseCase: UseRouting, ContextTokens: 4096, Limit: 2, MinimumFit: FitGood, StorageHeadroomPct: 15, PreferGPU: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("no routing model")
	}
	if out[0].Model.ParamsB > 4 {
		t.Fatalf("expected small routing model, got %.1fB", out[0].Model.ParamsB)
	}
}

func TestPlacementDoesNotPoolHeterogeneousBackends(t *testing.T) {
	p := HardwareProfile{
		CPU:    CPU{Name: "cpu", LogicalCores: 16, Architecture: "amd64"},
		Memory: Memory{AvailableBytes: 16 << 30},
		GPUs: []GPU{
			{Vendor: "nvidia", Name: "nvidia", DeviceID: "GPU-a", DeviceIndex: 0, VRAMBytes: 24 << 30, Backends: []string{"cuda"}},
			{Vendor: "amd", Name: "amd", DeviceID: "card1", DeviceIndex: 1, VRAMBytes: 24 << 30, Backends: []string{"rocm"}},
		},
	}
	plan, _, _, fit := bestPlacement(p, 40<<30, PlacementLayerSharded)
	if fit != FitTooTight || len(plan.Devices) != 0 {
		t.Fatalf("heterogeneous GPUs must not be pooled for sharding: fit=%s plan=%+v", fit, plan)
	}
}

func TestPlacementShardsCompatibleGPUs(t *testing.T) {
	p := HardwareProfile{
		CPU:    CPU{Name: "cpu", LogicalCores: 16, Architecture: "amd64"},
		Memory: Memory{AvailableBytes: 16 << 30},
		GPUs: []GPU{
			{Vendor: "nvidia", Name: "gpu0", DeviceID: "GPU-a", DeviceIndex: 0, VRAMBytes: 32 << 30, Backends: []string{"cuda", "vulkan"}},
			{Vendor: "nvidia", Name: "gpu1", DeviceID: "GPU-b", DeviceIndex: 1, VRAMBytes: 32 << 30, Backends: []string{"cuda", "vulkan"}},
		},
	}
	plan, mode, _, fit := bestPlacement(p, 48<<30, PlacementLayerSharded)
	if fit == FitTooTight || mode != RunGPU || plan.Mode != PlacementLayerSharded || plan.Backend != "cuda" || len(plan.Devices) != 2 {
		t.Fatalf("expected explicit CUDA layer-shard plan: fit=%s mode=%s plan=%+v", fit, mode, plan)
	}
	for _, d := range plan.Devices {
		if d.AllocatedBytes <= 0 || d.AllocatedBytes > d.CapacityBytes {
			t.Fatalf("invalid per-device allocation: %+v", d)
		}
	}
}

func TestTensorPlacementIsExplicitlyExperimental(t *testing.T) {
	p := HardwareProfile{
		CPU:    CPU{Name: "cpu", LogicalCores: 16, Architecture: "amd64"},
		Memory: Memory{AvailableBytes: 16 << 30},
		GPUs: []GPU{
			{Vendor: "amd", Name: "gpu0", DeviceID: "card0", DeviceIndex: 0, VRAMBytes: 32 << 30, Backends: []string{"rocm"}},
			{Vendor: "amd", Name: "gpu1", DeviceID: "card1", DeviceIndex: 1, VRAMBytes: 32 << 30, Backends: []string{"rocm"}},
		},
	}
	plan, _, _, fit := bestPlacement(p, 48<<30, PlacementTensorSharded)
	if fit == FitTooTight || plan.Mode != PlacementTensorSharded || !plan.Experimental {
		t.Fatalf("expected experimental tensor placement, got fit=%s plan=%+v", fit, plan)
	}
}

func TestCPUOnlyPlacementIsFirstClass(t *testing.T) {
	p := HardwareProfile{CPU: CPU{Name: "epyc", LogicalCores: 64, Architecture: "amd64", Backends: []string{"cpu", "cpu/amd64"}}, Memory: Memory{AvailableBytes: 96 << 30}}
	plan, mode, _, fit := bestPlacement(p, 24<<30, PlacementCPUOnly)
	if fit == FitTooTight || mode != RunCPU || plan.Mode != PlacementCPUOnly || len(plan.Devices) != 1 || plan.Devices[0].Kind != "cpu" {
		t.Fatalf("expected CPU-only placement: fit=%s mode=%s plan=%+v", fit, mode, plan)
	}
}

func TestQ40SizingRequiresExactVerifiedArtifact(t *testing.T){
 // Q4_0 is a supported llama.cpp tensor quantization. Its memory estimate
 // is allowed, but the separate artifact resolver still requires a
 // signed/adopted digest-pinned exact-quantization download.
 v,ok:=quants["Q4_0"]
 if !ok||v.bytesPerParam<=0||v.quality<=0{t.Fatalf("Q4_0 was not registered as a supported estimator: %+v",v)}
 if got:=weightBytes(9,"Q4_0");got<=0{t.Fatalf("Q4_0 estimate invalid: %d",got)}
 if _,ok:=quants["Q0_UNKNOWN"];ok{t.Fatal("unknown quantizations cannot be accepted")}
}
