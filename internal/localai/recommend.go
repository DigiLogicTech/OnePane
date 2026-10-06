package localai

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type quantInfo struct {
	bytesPerParam float64
	quality       int
}

var quants = map[string]quantInfo{
	"Q8_0": {1.0, 100}, "Q6_K": {0.78, 96}, "Q5_K_M": {0.68, 93}, "Q4_K_M": {0.58, 88}, "Q3_K_M": {0.48, 80}, "Q2_K": {0.40, 70},
}

func supportsUseCase(m ModelSpec, u UseCase) bool {
	for _, x := range m.UseCases {
		if x == u {
			return true
		}
	}
	return false
}
func contextOverhead(paramsB float64, ctx int64) int64 {
	base := int64(paramsB * 120 * 1024 * 1024)
	kv := ctx * int64(math.Max(128, paramsB*96)) * 2
	return base + kv
}
func weightBytes(paramsB float64, q string) int64 {
	qi := quants[q]
	return int64(paramsB * 1e9 * qi.bytesPerParam * 1.08)
}
func storageNeed(weights int64) (int64, int64) { return weights, weights }
func classifyFit(required, available int64) FitLevel {
	if available <= 0 {
		return FitTooTight
	}
	u := float64(required) / float64(available)
	switch {
	case u <= .60:
		return FitPerfect
	case u <= .85:
		return FitGood
	case u <= .98:
		return FitMarginal
	default:
		return FitTooTight
	}
}
func fitRank(f FitLevel) int {
	switch f {
	case FitPerfect:
		return 3
	case FitGood:
		return 2
	case FitMarginal:
		return 1
	}
	return 0
}
func minFitRank(f FitLevel) int {
	if f == "" {
		return fitRank(FitGood)
	}
	return fitRank(f)
}

var backendPriority = []string{"cuda", "rocm", "hip", "metal", "sycl", "vulkan", "opencl", "musa", "ascend", "cann"}

func gpuBackends(g GPU) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range append(append([]string{}, g.Backends...), g.Backend) {
		b = strings.ToLower(strings.TrimSpace(b))
		if b != "" && !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	return out
}
func preferredBackend(backends []string) string {
	for _, p := range backendPriority {
		for _, b := range backends {
			if b == p {
				return p
			}
		}
	}
	if len(backends) > 0 {
		return backends[0]
	}
	return ""
}
func deviceID(g GPU) string {
	if strings.TrimSpace(g.DeviceID) != "" {
		return g.DeviceID
	}
	return fmt.Sprintf("%s:%d", strings.ToLower(g.Vendor), g.DeviceIndex)
}
func runtimeDeviceName(backend string, index int) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "cuda":
		return fmt.Sprintf("CUDA%d", index)
	case "rocm", "hip":
		return fmt.Sprintf("ROCm%d", index)
	case "vulkan":
		return fmt.Sprintf("Vulkan%d", index)
	case "sycl":
		return fmt.Sprintf("SYCL%d", index)
	case "metal":
		return fmt.Sprintf("Metal%d", index)
	case "opencl":
		return fmt.Sprintf("OpenCL%d", index)
	case "musa":
		return fmt.Sprintf("MUSA%d", index)
	case "ascend", "cann":
		return fmt.Sprintf("CANN%d", index)
	default:
		return ""
	}
}
func accelDevice(g GPU, backend string, alloc int64) PlacementDevice {
	share := 0.0
	if alloc > 0 && g.VRAMBytes > 0 {
		share = float64(alloc) / float64(g.VRAMBytes)
	}
	return PlacementDevice{Kind: "accelerator", Vendor: g.Vendor, Name: g.Name, DeviceID: deviceID(g), DeviceIndex: g.DeviceIndex, Backend: backend, RuntimeDevice: runtimeDeviceName(backend, g.DeviceIndex), CapacityBytes: g.VRAMBytes, AllocatedBytes: alloc, Share: share}
}
func allocateAcross(gpus []GPU, backend string, required int64) []PlacementDevice {
	remaining := required
	out := make([]PlacementDevice, 0, len(gpus))
	total := int64(0)
	for _, g := range gpus {
		total += g.VRAMBytes
	}
	for i, g := range gpus {
		a := int64(0)
		if i == len(gpus)-1 {
			a = remaining
		} else if total > 0 {
			// Do not multiply byte counts in int64: large VRAM/model sizes can
			// overflow before division. The ratio is only used to choose a
			// placement split, so float64 precision is more than sufficient.
			a = int64(float64(required) * (float64(g.VRAMBytes) / float64(total)))
			if a > remaining {
				a = remaining
			}
		}
		if a > g.VRAMBytes {
			a = g.VRAMBytes
		}
		remaining -= a
		out = append(out, accelDevice(g, backend, a))
	}
	return out
}
func groupByBackend(p HardwareProfile) map[string][]GPU {
	groups := map[string][]GPU{}
	for _, g := range p.GPUs {
		if g.VRAMBytes <= 0 {
			continue
		}
		for _, b := range gpuBackends(g) {
			groups[b] = append(groups[b], g)
		}
	}
	return groups
}
func chooseGroup(groups map[string][]GPU) (string, []GPU) {
	for _, b := range backendPriority {
		if xs := groups[b]; len(xs) > 0 {
			return b, xs
		}
	}
	for b, xs := range groups {
		return b, xs
	}
	return "", nil
}
func totalCapacity(gs []GPU) int64 {
	var n int64
	for _, g := range gs {
		n += g.VRAMBytes
	}
	return n
}

func bestPlacement(p HardwareProfile, required int64, pref PlacementMode) (PlacementPlan, RunMode, int64, FitLevel) {
	groups := groupByBackend(p)
	// Prefer a single device whenever it actually fits unless the operator asked
	// for a specific sharding mode.
	if pref == "" || pref == PlacementSingleDevice {
		var candidates []struct {
			g GPU
			b string
		}
		for _, g := range p.GPUs {
			if g.VRAMBytes < required {
				continue
			}
			bs := gpuBackends(g)
			if b := preferredBackend(bs); b != "" {
				candidates = append(candidates, struct {
					g GPU
					b string
				}{g, b})
			}
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].g.VRAMBytes < candidates[j].g.VRAMBytes })
		if len(candidates) > 0 {
			c := candidates[0]
			fit := classifyFit(required, c.g.VRAMBytes)
			return PlacementPlan{Mode: PlacementSingleDevice, Backend: c.b, Devices: []PlacementDevice{accelDevice(c.g, c.b, required)}}, RunGPU, c.g.VRAMBytes, fit
		}
		if pref == PlacementSingleDevice {
			return PlacementPlan{}, RunGPU, 0, FitTooTight
		}
	}
	// Same-backend multi-device plans only. Never pool heterogeneous accelerators.
	multiModes := []PlacementMode{PlacementLayerSharded}
	if pref == PlacementLayerSharded || pref == PlacementRowSharded || pref == PlacementTensorSharded {
		multiModes = []PlacementMode{pref}
	}
	for _, mode := range multiModes {
		for _, b := range backendPriority {
			gs := groups[b]
			if len(gs) < 2 {
				continue
			}
			cap := totalCapacity(gs)
			if required > cap {
				continue
			}
			plan := PlacementPlan{Mode: mode, Backend: b, Devices: allocateAcross(gs, b, required)}
			if mode == PlacementTensorSharded {
				plan.Experimental = true
				plan.Notes = append(plan.Notes, "tensor sharding is runtime/backend dependent and requires empirical qualification")
			}
			return plan, RunGPU, cap, classifyFit(required, cap)
		}
	}
	if pref == PlacementLayerSharded || pref == PlacementRowSharded || pref == PlacementTensorSharded {
		return PlacementPlan{}, RunGPU, 0, FitTooTight
	}
	// CPU + one compatible accelerator backend. This keeps hardware classes explicit.
	if pref == "" || pref == PlacementCPUOffload {
		b, gs := chooseGroup(groups)
		cap := totalCapacity(gs)
		ram := p.Memory.AvailableBytes
		if cap > 0 && required <= cap+ram {
			gpuAlloc := required
			if gpuAlloc > cap {
				gpuAlloc = cap
			}
			devs := allocateAcross(gs, b, gpuAlloc)
			cpu := required - gpuAlloc
			devs = append(devs, PlacementDevice{Kind: "cpu", Name: p.CPU.Name, Backend: "cpu", CapacityBytes: ram, AllocatedBytes: cpu})
			fit := classifyFit(required, cap+ram)
			if fitRank(fit) > fitRank(FitGood) {
				fit = FitGood
			}
			return PlacementPlan{Mode: PlacementCPUOffload, Backend: b, Devices: devs}, RunCPUGPU, cap + ram, fit
		}
		if pref == PlacementCPUOffload {
			return PlacementPlan{}, RunCPUGPU, 0, FitTooTight
		}
	}
	ram := p.Memory.AvailableBytes
	fit := classifyFit(required, ram)
	if fitRank(fit) > fitRank(FitGood) {
		fit = FitGood
	}
	if fit == FitTooTight {
		return PlacementPlan{}, RunCPU, ram, fit
	}
	return PlacementPlan{Mode: PlacementCPUOnly, Backend: "cpu", Devices: []PlacementDevice{{Kind: "cpu", Name: p.CPU.Name, Backend: "cpu", CapacityBytes: ram, AllocatedBytes: required}}}, RunCPU, ram, fit
}

func estimateTPS(m ModelSpec, mode RunMode, fit FitLevel) *float64 {
	active := m.ParamsB
	if m.IsMoE && m.ActiveParamsB > 0 {
		active = m.ActiveParamsB
	}
	if active <= 0 {
		return nil
	}
	base := 35.0 / active
	switch mode {
	case RunGPU:
		base *= 4
	case RunMoE:
		base *= 3.2
	case RunCPUGPU:
		base *= 1.6
	case RunCPU:
		base *= .65
	}
	if fit == FitMarginal {
		base *= .75
	}
	if base < .2 {
		base = .2
	}
	return &base
}

func Recommend(p HardwareProfile, catalog []ModelSpec, req RecommendRequest) ([]Recommendation, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var out []Recommendation
	usableDisk := int64(float64(p.Storage.AvailableBytes) * float64(100-req.StorageHeadroomPct) / 100)
	for _, m := range catalog {
		if !supportsUseCase(m, req.UseCase) {
			continue
		}
		ctx := req.ContextTokens
		if ctx > m.ContextLength {
			ctx = m.ContextLength
		}
		var best *Recommendation
		for _, q := range m.Quantizations {
			q = strings.ToUpper(q)
			qi, ok := quants[q]
			if !ok {
				continue
			}
			weights := weightBytes(m.ParamsB, q)
			required := weights + contextOverhead(m.ParamsB, ctx)
			placement, mode, avail, fit := bestPlacement(p, required, req.PlacementPreference)
			if fit == FitTooTight || fitRank(fit) < minFitRank(req.MinimumFit) {
				continue
			}
			disk, scratch := storageNeed(weights)
			if disk+scratch > usableDisk {
				continue
			}
			util := 100 * float64(required) / float64(avail)
			score := m.QualityScore + qi.quality/5 + fitRank(fit)*12
			if mode == RunGPU || mode == RunMoE {
				score += 15
			}
			if req.PreferGPU && (mode != RunGPU && mode != RunMoE) {
				score -= 20
			}
			rec := Recommendation{Model: m, Quantization: q, ContextTokens: ctx, FitLevel: fit, RunMode: mode, EstimatedTPS: estimateTPS(m, mode, fit), EstimateSource: "onepane_native", EstimateConfidence: "estimated", MemoryRequired: required, MemoryAvailable: avail, UtilizationPct: util, DiskRequired: disk, DownloadScratch: scratch, Placement: placement, Score: score}
			if ctx < req.ContextTokens {
				rec.Notes = append(rec.Notes, "requested context capped to model maximum")
			}
			if mode == RunCPUGPU {
				rec.Notes = append(rec.Notes, "requires CPU/accelerator offload")
			}
			if mode == RunCPU {
				rec.Notes = append(rec.Notes, "CPU-only inference")
			}
			if placement.Mode == PlacementLayerSharded {
				rec.Notes = append(rec.Notes, "model will be layer-sharded across compatible accelerators")
			}
			if best == nil || rec.Score > best.Score {
				c := rec
				best = &c
			}
		}
		if best != nil {
			out = append(out, *best)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].MemoryRequired < out[j].MemoryRequired
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}
