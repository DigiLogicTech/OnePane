package localai

import (
	"context"
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
	"unsafe"
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

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var (
	kernel32Windows          = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatusEx = kernel32Windows.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeSpaceExW  = kernel32Windows.NewProc("GetDiskFreeSpaceExW")
)

func (d *Detector) Detect(ctx context.Context, nodeID, dataDir string) (HardwareProfile, error) {
	p := HardwareProfile{NodeID: strings.TrimSpace(nodeID), OSName: "windows", Architecture: runtime.GOARCH, DetectedAt: d.now().UnixMilli()}
	if p.NodeID == "" {
		return p, fmt.Errorf("node id required")
	}
	p.OSVersion = d.detectOSVersion(ctx)
	p.CPU = CPU{Name: d.detectCPUName(ctx), LogicalCores: runtime.NumCPU(), Architecture: runtime.GOARCH, Backends: []string{"cpu", "cpu/" + runtime.GOARCH}}
	p.Memory = readWindowsMemory()
	if p.Memory.TotalBytes <= 0 || p.Memory.AvailableBytes <= 0 {
		return p, fmt.Errorf("unable to determine system RAM")
	}
	p.GPUs = d.detectGPUs(ctx)
	p.RuntimeProbes = d.detectRuntimes(ctx)
	if strings.TrimSpace(dataDir) == "" {
		dataDir = filepath.Join(os.Getenv("ProgramData"), "OnePane")
		if strings.TrimSpace(os.Getenv("ProgramData")) == "" {
			dataDir = `C:\ProgramData\OnePane`
		}
	}
	p.Storage = readWindowsStorage(dataDir)
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

func (d *Detector) detectOSVersion(ctx context.Context) string {
	if b, err := d.runner.Output(ctx, "cmd.exe", "/d", "/c", "ver"); err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b))
	}
	return "Windows"
}

func (d *Detector) detectCPUName(ctx context.Context) string {
	if b, err := d.runner.Output(ctx, "wmic.exe", "cpu", "get", "Name", "/value"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "Name=") {
				if v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Name=")); v != "" {
					return v
				}
			}
		}
	}
	if v := strings.TrimSpace(os.Getenv("PROCESSOR_IDENTIFIER")); v != "" {
		return v
	}
	return "Windows CPU"
}

func readWindowsMemory() Memory {
	st := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&st)))
	if r == 0 {
		return Memory{}
	}
	return Memory{TotalBytes: int64(st.TotalPhys), AvailableBytes: int64(st.AvailPhys)}
}

func readWindowsStorage(path string) Storage {
	probe := filepath.Clean(path)
	for {
		if _, err := os.Stat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe || parent == "." {
			if vol := filepath.VolumeName(path); vol != "" {
				probe = vol + `\`
			}
			break
		}
		probe = parent
	}
	ptr, err := syscall.UTF16PtrFromString(probe)
	if err != nil {
		return Storage{Path: path}
	}
	var avail, total, free uint64
	r, _, _ := procGetDiskFreeSpaceExW.Call(uintptr(unsafe.Pointer(ptr)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if r == 0 {
		return Storage{Path: path}
	}
	return Storage{Path: path, CapacityBytes: int64(total), AvailableBytes: int64(avail)}
}

func (d *Detector) detectGPUs(ctx context.Context) []GPU {
	var out []GPU
	vulkan := commandWorksWindows(ctx, d.runner, "vulkaninfo.exe", "--summary")
	sycl := commandWorksWindows(ctx, d.runner, "sycl-ls.exe")
	opencl := commandWorksWindows(ctx, d.runner, "clinfo.exe", "-l")

	if b, err := d.runner.Output(ctx, "nvidia-smi.exe", "--query-gpu=index,uuid,name,memory.total,memory.free,driver_version", "--format=csv,noheader,nounits"); err == nil {
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
			out = append(out, GPU{Vendor: "nvidia", DeviceID: strings.TrimSpace(p[1]), Name: strings.TrimSpace(p[2]), VRAMBytes: parseMiBWindows(p[3]), FreeVRAMBytes: parseMiBWindows(p[4]), DriverVersion: strings.TrimSpace(p[5]), Backend: "cuda", Backends: backs, DeviceIndex: idx})
		}
	}

	if b, err := d.runner.Output(ctx, "wmic.exe", "path", "win32_VideoController", "get", "Name,AdapterRAM,DriverVersion,PNPDeviceID", "/format:csv"); err == nil {
		for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(strings.ToLower(line), "node,") {
				continue
			}
			cols := strings.Split(line, ",")
			if len(cols) < 5 {
				continue
			}
			adapterRAM, _ := strconv.ParseInt(strings.TrimSpace(cols[1]), 10, 64)
			driver := strings.TrimSpace(cols[2])
			name := strings.TrimSpace(cols[3])
			pnp := strings.TrimSpace(cols[4])
			if name == "" {
				continue
			}
			lo := strings.ToLower(name)
			vendor := "other"
			switch {
			case strings.Contains(lo, "nvidia"):
				vendor = "nvidia"
			case strings.Contains(lo, "amd"), strings.Contains(lo, "radeon"):
				vendor = "amd"
			case strings.Contains(lo, "intel"):
				vendor = "intel"
			}
			duplicate := false
			for _, g := range out {
				if vendor == "nvidia" && g.Vendor == "nvidia" || strings.EqualFold(g.Name, name) {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			backs := []string{}
			preferred := ""
			if vendor == "amd" && commandWorksWindows(ctx, d.runner, "rocm-smi.exe", "--showproductname") {
				backs = append(backs, "rocm")
				preferred = "rocm"
			}
			if vendor == "intel" && sycl {
				backs = append(backs, "sycl")
				preferred = "sycl"
			}
			if vulkan {
				backs = appendUniqueWindows(backs, "vulkan")
				if preferred == "" {
					preferred = "vulkan"
				}
			}
			if opencl {
				backs = appendUniqueWindows(backs, "opencl")
				if preferred == "" {
					preferred = "opencl"
				}
			}
			out = append(out, GPU{Vendor: vendor, Name: name, VRAMBytes: adapterRAM, DriverVersion: driver, DeviceID: pnp, Backend: preferred, Backends: backs, DeviceIndex: len(out)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Vendor == out[j].Vendor {
			return out[i].DeviceIndex < out[j].DeviceIndex
		}
		return out[i].Vendor < out[j].Vendor
	})
	return out
}

func (d *Detector) detectRuntimes(ctx context.Context) []RuntimeProbe {
	defs := []struct {
		name, bin string
		args      []string
	}{
		{"llama.cpp", "llama-server.exe", []string{"--version"}},
		{"ollama", "ollama.exe", []string{"--version"}},
		{"vllm", "vllm.exe", []string{"--version"}},
		{"podman", "podman.exe", []string{"--version"}},
		{"docker", "docker.exe", []string{"--version"}},
		{"python", "python.exe", []string{"--version"}},
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

func commandWorksWindows(ctx context.Context, r CommandRunner, name string, args ...string) bool {
	_, err := r.Output(ctx, name, args...)
	return err == nil
}

func parseMiBWindows(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v * 1024 * 1024
}

func appendUniqueWindows(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}
