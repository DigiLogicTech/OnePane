package localai

import (
	"strings"
	"testing"
)

func TestLlamaPlacementArgs(t *testing.T) {
	tests := []struct {
		name string
		plan PlacementPlan
		want []string
	}{
		{"cpu", PlacementPlan{Mode: PlacementCPUOnly}, []string{"--n-gpu-layers", "0"}},
		{"single", PlacementPlan{Mode: PlacementSingleDevice, Devices: []PlacementDevice{{Kind: "accelerator", Backend: "cuda", DeviceIndex: 2}}}, []string{"--device", "CUDA2", "--split-mode", "none", "--main-gpu", "0", "--n-gpu-layers", "all"}},
		{"single-rocm", PlacementPlan{Mode: PlacementSingleDevice, Devices: []PlacementDevice{{Kind: "accelerator", Backend: "rocm", DeviceIndex: 1}}}, []string{"--device", "ROCm1", "--split-mode", "none"}},
		{"single-sycl", PlacementPlan{Mode: PlacementSingleDevice, Devices: []PlacementDevice{{Kind: "accelerator", Backend: "sycl", DeviceIndex: 0}}}, []string{"--device", "SYCL0", "--split-mode", "none"}},
		{"single-vulkan", PlacementPlan{Mode: PlacementSingleDevice, Devices: []PlacementDevice{{Kind: "accelerator", Backend: "vulkan", DeviceIndex: 0}}}, []string{"--device", "Vulkan0", "--split-mode", "none"}},
		{"single-cann", PlacementPlan{Mode: PlacementSingleDevice, Devices: []PlacementDevice{{Kind: "accelerator", Backend: "cann", DeviceIndex: 0}}}, []string{"--device", "CANN0", "--split-mode", "none"}},
		{"layer", PlacementPlan{Mode: PlacementLayerSharded, Devices: []PlacementDevice{{Kind: "accelerator", DeviceIndex: 0, Share: .5}, {Kind: "accelerator", DeviceIndex: 1, Share: .5}}}, []string{"--split-mode", "layer", "--tensor-split", "0.500000,0.500000"}},
		{"tensor", PlacementPlan{Mode: PlacementTensorSharded, Devices: []PlacementDevice{{Kind: "accelerator", DeviceIndex: 0, Share: .4}, {Kind: "accelerator", DeviceIndex: 1, Share: .6}}}, []string{"--split-mode", "tensor", "--tensor-split", "0.400000,0.600000"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(llamaPlacementArgs(tt.plan), " ")
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Fatalf("%q missing %q", got, w)
				}
			}
		})
	}
}
