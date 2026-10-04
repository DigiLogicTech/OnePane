package localai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type CommandRunner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

type Detector struct {
	runner CommandRunner
	now    func() time.Time
}

func NewDetector() *Detector                          { return &Detector{runner: execRunner{}, now: time.Now} }
func NewDetectorWithRunner(r CommandRunner) *Detector { return &Detector{runner: r, now: time.Now} }

func (d *Detector) Detect(ctx context.Context, nodeID, dataDir string) (HardwareProfile, error) {
	if runtime.GOOS != "darwin" {
		return HardwareProfile{}, fmt.Errorf("native detector currently supports macOS; got %s", runtime.GOOS)
	}
	p := HardwareProfile{
		NodeID:       strings.TrimSpace(nodeID),
		OSName:       "darwin",
		Architecture: runtime.GOARCH,
		DetectedAt:   d.now().UnixMilli(),
	}
	if p.NodeID == "" {
		return p, fmt.Errorf("node id required")
	}
	if b, err := d.runner.Output(ctx, "sw_vers", "-productVersion"); err == nil {
		p.OSVersion = strings.TrimSpace(string(b))
	}
	if b, err := d.runner.Output(ctx, "uname", "-r"); err == nil {
		p.KernelVersion = strings.TrimSpace(string(b))
	}

	p.CPU = CPU{
		Name:         d.cpuName(ctx),
		LogicalCores: runtime.NumCPU(),
		Architecture: runtime.GOARCH,
		Features:     d.cpuFeatures(ctx),
		Backends:     []string{"cpu", "cpu/" + runtime.GOARCH},
	}
	p.Memory = d.memory(ctx)
	if p.Memory.TotalBytes <= 0 {
		return p, fmt.Errorf("unable to determine system RAM")
	}
	p.GPUs = d.detectGPUs(ctx, p.Memory)
	p.RuntimeProbes = d.detectRuntimes(ctx)
	if dataDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataDir = filepath.Join(home, "Library", "Application Support", "OnePane", "data")
		} else {
			dataDir = "/tmp/onepane"
		}
	}
	p.Storage = readStorageDarwin(dataDir)
	if p.Storage.AvailableBytes <= 0 {
		return p, fmt.Errorf("unable to determine storage capacity for %s", dataDir)
	}
	fp, err := p.ComputeFingerprint()
	if err != nil {
		return p, err
	}
	p.Fingerprint = fp
	return p, p.Validate()
}

func (d *Detector) cpuName(ctx context.Context) string {
	for _, key := range []string{"machdep.cpu.brand_string", "hw.model"} {
		if b, err := d.runner.Output(ctx, "sysctl", "-n", key); err == nil && strings.TrimSpace(string(b)) != "" {
			return strings.TrimSpace(string(b))
		}
	}
	if runtime.GOARCH == "arm64" {
		return "Apple Silicon"
	}
	return "unknown"
}

func (d *Detector) cpuFeatures(ctx context.Context) []string {
	features := map[string]struct{}{}
	if runtime.GOARCH == "amd64" {
		for _, key := range []string{"machdep.cpu.features", "machdep.cpu.leaf7_features", "machdep.cpu.extfeatures"} {
			if b, err := d.runner.Output(ctx, "sysctl", "-n", key); err == nil {
				for _, f := range strings.Fields(strings.ToLower(string(b))) {
					features[f] = struct{}{}
				}
			}
		}
	} else if b, err := d.runner.Output(ctx, "sysctl", "-a"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			lo := strings.ToLower(strings.TrimSpace(line))
			if !strings.HasPrefix(lo, "hw.optional.arm.feat_") || !(strings.HasSuffix(lo, ": 1") || strings.HasSuffix(lo, "= 1")) {
				continue
			}
			name := line
			if i := strings.IndexAny(name, ":="); i >= 0 {
				name = name[:i]
			}
			name = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "hw.optional.arm.feat_")
			if name != "" {
				features[name] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(features))
	for f := range features {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func (d *Detector) memory(ctx context.Context) Memory {
	var total int64
	if b, err := d.runner.Output(ctx, "sysctl", "-n", "hw.memsize"); err == nil {
		total, _ = strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	}
	available := availableMemoryDarwin(ctx, d.runner)
	if available <= 0 || available > total {
		available = total
	}
	return Memory{TotalBytes: total, AvailableBytes: available, Unified: runtime.GOARCH == "arm64"}
}

func availableMemoryDarwin(ctx context.Context, r CommandRunner) int64 {
	b, err := r.Output(ctx, "vm_stat")
	if err != nil {
		return 0
	}
	pageSize := int64(4096)
	var pages int64
	for _, line := range strings.Split(string(b), "\n") {
		lo := strings.ToLower(line)
		if strings.Contains(lo, "page size of") {
			if i := strings.Index(lo, "page size of"); i >= 0 {
				tail := lo[i+len("page size of"):]
				for _, f := range strings.Fields(tail) {
					f = strings.Trim(f, "() .bytes")
					if n, e := strconv.ParseInt(f, 10, 64); e == nil && n > 0 {
						pageSize = n
						break
					}
				}
			}
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		if key != "pages free" && key != "pages inactive" && key != "pages speculative" {
			continue
		}
		value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line[colon+1:]), "."))
		n, _ := strconv.ParseInt(value, 10, 64)
		if n > 0 {
			pages += n
		}
	}
	return pages * pageSize
}

func readStorageDarwin(path string) Storage {
	probe := path
	for {
		if _, err := os.Stat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			probe = "/"
			break
		}
		probe = parent
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(probe, &st); err != nil {
		return Storage{Path: path}
	}
	return Storage{
		Path:           path,
		CapacityBytes:  int64(st.Blocks) * int64(st.Bsize),
		AvailableBytes: int64(st.Bavail) * int64(st.Bsize),
	}
}

func (d *Detector) detectGPUs(ctx context.Context, mem Memory) []GPU {
	b, err := d.runner.Output(ctx, "system_profiler", "SPDisplaysDataType", "-json")
	if err != nil {
		if runtime.GOARCH == "arm64" {
			return []GPU{{Vendor: "apple", Name: "Apple Silicon GPU", VRAMBytes: mem.TotalBytes, FreeVRAMBytes: mem.AvailableBytes, Backend: "metal", Backends: []string{"metal"}, DeviceIndex: 0}}
		}
		return nil
	}
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	items, _ := doc["SPDisplaysDataType"].([]any)
	out := make([]GPU, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := firstStringDarwin(item, "sppci_model", "spdisplays_chipset_model", "_name")
		if name == "" {
			name = "macOS GPU"
		}
		vendorText := strings.ToLower(firstStringDarwin(item, "spdisplays_vendor", "spdisplays_vendor-id"))
		vendor := "other"
		switch {
		case strings.Contains(vendorText, "apple") || strings.Contains(strings.ToLower(name), "apple"):
			vendor = "apple"
		case strings.Contains(vendorText, "amd") || strings.Contains(vendorText, "ati") || strings.Contains(strings.ToLower(name), "amd") || strings.Contains(strings.ToLower(name), "radeon"):
			vendor = "amd"
		case strings.Contains(vendorText, "intel") || strings.Contains(strings.ToLower(name), "intel"):
			vendor = "intel"
		case strings.Contains(vendorText, "nvidia") || strings.Contains(strings.ToLower(name), "nvidia"):
			vendor = "nvidia"
		}
		vram := parseDisplayMemoryDarwin(firstStringDarwin(item, "spdisplays_vram", "spdisplays_vram_shared"))
		free := int64(0)
		if runtime.GOARCH == "arm64" && vendor == "apple" {
			vram = mem.TotalBytes
			free = mem.AvailableBytes
		}
		backs := []string{}
		preferred := ""
		if metal := strings.ToLower(firstStringDarwin(item, "spdisplays_metal")); strings.Contains(metal, "supported") || vendor == "apple" {
			backs = append(backs, "metal")
			preferred = "metal"
		}
		out = append(out, GPU{Vendor: vendor, Name: name, VRAMBytes: vram, FreeVRAMBytes: free, Backend: preferred, Backends: backs, DeviceIndex: len(out)})
	}
	if len(out) == 0 && runtime.GOARCH == "arm64" {
		out = append(out, GPU{Vendor: "apple", Name: "Apple Silicon GPU", VRAMBytes: mem.TotalBytes, FreeVRAMBytes: mem.AvailableBytes, Backend: "metal", Backends: []string{"metal"}, DeviceIndex: 0})
	}
	return out
}

func firstStringDarwin(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch x := v.(type) {
			case string:
				if strings.TrimSpace(x) != "" {
					return strings.TrimSpace(x)
				}
			case fmt.Stringer:
				if strings.TrimSpace(x.String()) != "" {
					return strings.TrimSpace(x.String())
				}
			}
		}
	}
	return ""
}

func parseDisplayMemoryDarwin(v string) int64 {
	fields := strings.Fields(strings.TrimSpace(v))
	if len(fields) < 2 {
		return 0
	}
	n, err := strconv.ParseFloat(strings.ReplaceAll(fields[0], ",", ""), 64)
	if err != nil || n <= 0 {
		return 0
	}
	switch strings.ToLower(fields[1]) {
	case "kb":
		return int64(n * 1024)
	case "mb":
		return int64(n * 1024 * 1024)
	case "gb":
		return int64(n * 1024 * 1024 * 1024)
	case "tb":
		return int64(n * 1024 * 1024 * 1024 * 1024)
	default:
		return 0
	}
}

func (d *Detector) detectRuntimes(ctx context.Context) []RuntimeProbe {
	defs := []struct {
		name, bin string
		args      []string
	}{
		{"llama.cpp", "llama-server", []string{"--version"}},
		{"ollama", "ollama", []string{"--version"}},
		{"podman", "podman", []string{"--version"}},
		{"docker", "docker", []string{"--version"}},
		{"python", "python3", []string{"--version"}},
	}
	out := make([]RuntimeProbe, 0, len(defs))
	for _, x := range defs {
		rp := RuntimeProbe{Name: x.name}
		if path, err := exec.LookPath(x.bin); err == nil {
			rp.Installed = true
			rp.Executable = path
			if b, e := d.runner.Output(ctx, x.bin, x.args...); e == nil {
				rp.Version = strings.TrimSpace(string(b))
			}
		}
		out = append(out, rp)
	}
	return out
}
