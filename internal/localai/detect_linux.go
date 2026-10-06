package localai

import (
	"bufio"
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
	if runtime.GOOS != "linux" {
		return HardwareProfile{}, fmt.Errorf("native detector currently supports linux; got %s", runtime.GOOS)
	}
	p := HardwareProfile{NodeID: strings.TrimSpace(nodeID), OSName: "linux", Architecture: runtime.GOARCH, DetectedAt: d.now().UnixMilli()}
	if p.NodeID == "" {
		return p, fmt.Errorf("node id required")
	}
	p.OSVersion = readOSRelease()
	if b, err := d.runner.Output(ctx, "uname", "-r"); err == nil {
		p.KernelVersion = strings.TrimSpace(string(b))
	}
	p.CPU = CPU{Name: readCPUName(), LogicalCores: runtime.NumCPU(), Architecture: runtime.GOARCH, Features: readCPUFeatures(), Backends: []string{"cpu", "cpu/" + runtime.GOARCH}}
	p.Memory = readMemory()
	if p.Memory.TotalBytes <= 0 {
		return p, fmt.Errorf("unable to determine system RAM")
	}
	p.GPUs = d.detectGPUs(ctx)
	p.RuntimeProbes = d.detectRuntimes(ctx)
	if dataDir == "" {
		dataDir = "/var/lib/onepane"
	}
	p.Storage = readStorage(dataDir)
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

func readOSRelease() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	s := bufio.NewScanner(strings.NewReader(string(b)))
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		}
	}
	return ""
}
func readCPUName() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "model name") {
			if _, v, ok := strings.Cut(line, ":"); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return "unknown"
}

func readCPUFeatures() []string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return nil
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "flags") || strings.HasPrefix(line, "Features") {
			if _, v, ok := strings.Cut(line, ":"); ok {
				features := strings.Fields(strings.TrimSpace(v))
				sort.Strings(features)
				return features
			}
		}
	}
	return nil
}

func readMemory() Memory {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Memory{}
	}
	vals := map[string]int64{}
	s := bufio.NewScanner(strings.NewReader(string(b)))
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) >= 2 {
			n, _ := strconv.ParseInt(f[1], 10, 64)
			vals[strings.TrimSuffix(f[0], ":")] = n * 1024
		}
	}
	return Memory{TotalBytes: vals["MemTotal"], AvailableBytes: vals["MemAvailable"]}
}
func readStorage(path string) Storage {
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
	return Storage{Path: path, CapacityBytes: int64(st.Blocks) * int64(st.Bsize), AvailableBytes: int64(st.Bavail) * int64(st.Bsize)}
}
func parseMiB(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v * 1024 * 1024
}
func (d *Detector) detectGPUs(ctx context.Context) []GPU {
	var out []GPU
	vulkan := commandWorks(ctx, d.runner, "vulkaninfo", "--summary")
	syCL := commandWorks(ctx, d.runner, "sycl-ls")
	opencl := commandWorks(ctx, d.runner, "clinfo", "-l")
	if b, err := d.runner.Output(ctx, "nvidia-smi", "--query-gpu=index,uuid,name,memory.total,memory.free,driver_version", "--format=csv,noheader,nounits"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			p := strings.Split(line, ",")
			if len(p) < 6 {
				continue
			}
			idx, _ := strconv.Atoi(strings.TrimSpace(p[0]))
			backs := []string{"cuda"}
			if vulkan {
				backs = append(backs, "vulkan")
			}
			out = append(out, GPU{Vendor: "nvidia", DeviceID: strings.TrimSpace(p[1]), Name: strings.TrimSpace(p[2]), VRAMBytes: parseMiB(p[3]), FreeVRAMBytes: parseMiB(p[4]), DriverVersion: strings.TrimSpace(p[5]), Backend: "cuda", Backends: backs, DeviceIndex: idx})
		}
	}
	// AMD is discovered independently of NVIDIA so mixed-vendor hosts remain visible.
	if b, err := d.runner.Output(ctx, "rocm-smi", "--showproductname", "--showmeminfo", "vram", "--showdriverversion", "--json"); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		var doc map[string]map[string]any
		if json.Unmarshal(b, &doc) == nil {
			idx := 0
			for card, fields := range doc {
				name := firstStringAny(fields, "Card series", "Card model", "Card SKU")
				if name == "" {
					name = "AMD GPU"
				}
				total := firstIntAny(fields, "VRAM Total Memory (B)", "VRAM Total Used Memory (B)")
				backs := []string{"rocm"}
				if vulkan {
					backs = append(backs, "vulkan")
				}
				out = append(out, GPU{Vendor: "amd", DeviceID: card, Name: name, VRAMBytes: total, Backend: "rocm", Backends: backs, DeviceIndex: idx})
				idx++
			}
		}
	}
	// lspci is the broad fallback and also finds Intel/other Vulkan/OpenCL devices.
	if b, err := d.runner.Output(ctx, "lspci"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			lo := strings.ToLower(line)
			if !(strings.Contains(lo, "vga") || strings.Contains(lo, "3d controller")) {
				continue
			}
			vendor := "other"
			if strings.Contains(lo, "intel") {
				vendor = "intel"
			} else if strings.Contains(lo, "amd") || strings.Contains(lo, "advanced micro devices") {
				vendor = "amd"
			} else if strings.Contains(lo, "nvidia") {
				vendor = "nvidia"
			}
			already := false
			for _, g := range out {
				if strings.Contains(strings.ToLower(g.Name), strings.ToLower(strings.TrimSpace(line))) || (g.Vendor == vendor && vendor != "intel" && vendor != "other") {
					already = true
					break
				}
			}
			if already {
				continue
			}
			backs := []string{}
			preferred := ""
			if vendor == "intel" && syCL {
				backs = append(backs, "sycl")
				preferred = "sycl"
			}
			if vulkan {
				backs = append(backs, "vulkan")
				if preferred == "" {
					preferred = "vulkan"
				}
			}
			if opencl {
				backs = append(backs, "opencl")
				if preferred == "" {
					preferred = "opencl"
				}
			}
			out = append(out, GPU{Vendor: vendor, Name: strings.TrimSpace(line), Backend: preferred, Backends: backs, DeviceIndex: len(out)})
		}
	}
	return out
}

func commandWorks(ctx context.Context, r CommandRunner, name string, args ...string) bool {
	_, err := r.Output(ctx, name, args...)
	return err == nil
}
func firstStringAny(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func firstIntAny(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return int64(v)
		case string:
			n, _ := strconv.ParseInt(strings.Fields(v)[0], 10, 64)
			if n > 0 {
				return n
			}
		}
	}
	return 0
}

func (d *Detector) detectRuntimes(ctx context.Context) []RuntimeProbe {
	defs := []struct {
		name, bin string
		args      []string
	}{{"llama.cpp", "llama-server", []string{"--version"}}, {"ollama", "ollama", []string{"--version"}}, {"vllm", "vllm", []string{"--version"}}, {"podman", "podman", []string{"--version"}}, {"docker", "docker", []string{"--version"}}}
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

func currentStorage(path string) Storage { return readStorage(path) }
