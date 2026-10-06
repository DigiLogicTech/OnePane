package localai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type GPU struct {
	Vendor        string   `json:"vendor"`
	Name          string   `json:"name"`
	VRAMBytes     int64    `json:"vram_bytes"`
	FreeVRAMBytes int64    `json:"free_vram_bytes,omitempty"`
	DriverVersion string   `json:"driver_version,omitempty"`
	Backend       string   `json:"backend,omitempty"` // legacy preferred backend
	Backends      []string `json:"backends,omitempty"`
	DeviceID      string   `json:"device_id,omitempty"`
	DeviceIndex   int      `json:"device_index"`
}

type CPU struct {
	Name         string   `json:"name"`
	LogicalCores int      `json:"logical_cores"`
	Architecture string   `json:"architecture"`
	Features     []string `json:"features,omitempty"`
	Backends     []string `json:"backends,omitempty"`
}

type Memory struct {
	TotalBytes     int64 `json:"total_bytes"`
	AvailableBytes int64 `json:"available_bytes"`
	Unified        bool  `json:"unified"`
}

type Storage struct {
	Path           string `json:"path"`
	CapacityBytes  int64  `json:"capacity_bytes"`
	AvailableBytes int64  `json:"available_bytes"`
}

type RuntimeProbe struct {
	Name       string `json:"name"`
	Installed  bool   `json:"installed"`
	Version    string `json:"version,omitempty"`
	Executable string `json:"executable,omitempty"`
}

type HardwareProfile struct {
	ID            string         `json:"id,omitempty"`
	NodeID        string         `json:"node_id"`
	Fingerprint   string         `json:"fingerprint"`
	OSName        string         `json:"os_name"`
	OSVersion     string         `json:"os_version,omitempty"`
	Architecture  string         `json:"architecture"`
	KernelVersion string         `json:"kernel_version,omitempty"`
	CPU           CPU            `json:"cpu"`
	Memory        Memory         `json:"memory"`
	GPUs          []GPU          `json:"gpus"`
	RuntimeProbes []RuntimeProbe `json:"runtime_probes"`
	Storage       Storage        `json:"storage"`
	DetectedAt    int64          `json:"detected_at"`
}

func (p HardwareProfile) Validate() error {
	if strings.TrimSpace(p.NodeID) == "" || strings.TrimSpace(p.OSName) == "" || strings.TrimSpace(p.Architecture) == "" {
		return errors.New("node, OS and architecture are required")
	}
	if p.CPU.LogicalCores < 1 || p.Memory.TotalBytes <= 0 || p.Memory.AvailableBytes <= 0 || p.Storage.AvailableBytes <= 0 {
		return errors.New("invalid hardware capacity")
	}
	for _, g := range p.GPUs {
		if strings.TrimSpace(g.Vendor) == "" || strings.TrimSpace(g.Name) == "" || g.VRAMBytes < 0 {
			return errors.New("invalid GPU record")
		}
	}
	return nil
}

func (p HardwareProfile) ComputeFingerprint() (string, error) {
	stable := struct {
		OSName, OSVersion, Architecture, KernelVersion string
		CPU                                            CPU
		MemoryTotal                                    int64
		GPUs                                           []GPU
	}{p.OSName, p.OSVersion, p.Architecture, p.KernelVersion, p.CPU, p.Memory.TotalBytes, append([]GPU(nil), p.GPUs...)}
	sort.Slice(stable.GPUs, func(i, j int) bool {
		if stable.GPUs[i].Vendor == stable.GPUs[j].Vendor {
			return stable.GPUs[i].DeviceIndex < stable.GPUs[j].DeviceIndex
		}
		return stable.GPUs[i].Vendor < stable.GPUs[j].Vendor
	})
	raw, err := json.Marshal(stable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type UseCase string

const (
	UseGeneral   UseCase = "general"
	UseCoding    UseCase = "coding"
	UseReasoning UseCase = "reasoning"
	UseChat      UseCase = "chat"
	UseEmbedding UseCase = "embedding"
	UseRouting   UseCase = "routing"
)

type ModelSpec struct {
	ModelRef      string    `json:"model_ref"`
	DisplayName   string    `json:"display_name"`
	Provider      string    `json:"provider"`
	Architecture  string    `json:"architecture,omitempty"`
	ParamsB       float64   `json:"params_b"`
	ActiveParamsB float64   `json:"active_params_b,omitempty"`
	IsMoE         bool      `json:"is_moe"`
	ContextLength int64     `json:"context_length"`
	UseCases      []UseCase `json:"use_cases"`
	QualityScore  int       `json:"quality_score"`
	SourceRef     string    `json:"source_ref"`
	Runtime       string    `json:"runtime"`
	Quantizations []string  `json:"quantizations"`
}

type FitLevel string

const (
	FitPerfect  FitLevel = "perfect"
	FitGood     FitLevel = "good"
	FitMarginal FitLevel = "marginal"
	FitTooTight FitLevel = "too_tight"
)

type RunMode string

const (
	RunGPU    RunMode = "gpu"
	RunMoE    RunMode = "moe"
	RunCPUGPU RunMode = "cpu_gpu"
	RunCPU    RunMode = "cpu"
)

type PlacementMode string

const (
	PlacementSingleDevice  PlacementMode = "single_device"
	PlacementLayerSharded  PlacementMode = "layer_sharded"
	PlacementRowSharded    PlacementMode = "row_sharded"
	PlacementTensorSharded PlacementMode = "tensor_sharded"
	PlacementCPUOffload    PlacementMode = "cpu_offload"
	PlacementCPUOnly       PlacementMode = "cpu_only"
)

type PlacementDevice struct {
	Kind           string  `json:"kind"` // accelerator | cpu
	Vendor         string  `json:"vendor,omitempty"`
	Name           string  `json:"name"`
	DeviceID       string  `json:"device_id,omitempty"`
	DeviceIndex    int     `json:"device_index,omitempty"`
	Backend        string  `json:"backend"`
	RuntimeDevice  string  `json:"runtime_device,omitempty"`
	CapacityBytes  int64   `json:"capacity_bytes"`
	AllocatedBytes int64   `json:"allocated_bytes"`
	Share          float64 `json:"share,omitempty"`
}

type PlacementPlan struct {
	Mode         PlacementMode     `json:"mode"`
	Backend      string            `json:"backend"`
	Devices      []PlacementDevice `json:"devices"`
	Experimental bool              `json:"experimental,omitempty"`
	Notes        []string          `json:"notes,omitempty"`
}

func (p PlacementPlan) TotalCapacity() int64 {
	var n int64
	for _, d := range p.Devices {
		n += d.CapacityBytes
	}
	return n
}
func (p PlacementPlan) TotalAllocated() int64 {
	var n int64
	for _, d := range p.Devices {
		n += d.AllocatedBytes
	}
	return n
}

type Recommendation struct {
	Model              ModelSpec       `json:"model"`
	Quantization       string          `json:"quantization"`
	ContextTokens      int64           `json:"context_tokens"`
	FitLevel           FitLevel        `json:"fit_level"`
	RunMode            RunMode         `json:"run_mode"`
	EstimatedTPS       *float64        `json:"estimated_tps,omitempty"`
	PrefillTPS         *float64        `json:"prefill_tps,omitempty"`
	TTFTMS             *float64        `json:"ttft_ms,omitempty"`
	EstimateSource     string          `json:"estimate_source,omitempty"`
	EstimateConfidence string          `json:"estimate_confidence,omitempty"`
	MemoryRequired     int64           `json:"memory_required_bytes"`
	MemoryAvailable    int64           `json:"memory_available_bytes"`
	UtilizationPct     float64         `json:"utilization_pct"`
	DiskRequired         int64           `json:"disk_required_bytes"`
	DownloadScratch      int64           `json:"download_scratch_bytes"`
	ModelArtifactBytes   int64           `json:"model_artifact_bytes,omitempty"`
	RuntimeDownloadBytes int64           `json:"runtime_download_bytes,omitempty"`
	RuntimeInstallReserve int64          `json:"runtime_install_reserve_bytes,omitempty"`
	Placement            PlacementPlan   `json:"placement"`
	LLMFit             *LLMFitAdvisory `json:"llmfit,omitempty"`
	Score              int             `json:"score"`
	Notes              []string        `json:"notes"`
}

type RecommendRequest struct {
	UseCase             UseCase
	ContextTokens       int64
	Limit               int
	MinimumFit          FitLevel
	StorageHeadroomPct  int
	PreferGPU           bool
	PlacementPreference PlacementMode
}

func validateUseCase(u UseCase) bool {
	switch u {
	case UseGeneral, UseCoding, UseReasoning, UseChat, UseEmbedding, UseRouting:
		return true
	}
	return false
}
func (r RecommendRequest) Validate() error {
	if !validateUseCase(r.UseCase) {
		return fmt.Errorf("invalid use case %q", r.UseCase)
	}
	if r.ContextTokens <= 0 || r.Limit < 1 || r.Limit > 50 || r.StorageHeadroomPct < 0 || r.StorageHeadroomPct > 90 {
		return errors.New("invalid recommendation bounds")
	}
	switch r.PlacementPreference {
	case "", PlacementSingleDevice, PlacementLayerSharded, PlacementRowSharded, PlacementTensorSharded, PlacementCPUOffload, PlacementCPUOnly:
	default:
		return errors.New("invalid placement preference")
	}
	return nil
}
