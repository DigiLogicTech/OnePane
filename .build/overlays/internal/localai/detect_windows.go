package localai

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Detector struct{ now func() time.Time }

func NewDetector() *Detector { return &Detector{now: time.Now} }

func (d *Detector) Detect(ctx context.Context, nodeID, dataDir string) (HardwareProfile, error) {
	p := HardwareProfile{NodeID: strings.TrimSpace(nodeID), OSName: "windows", Architecture: runtime.GOARCH, DetectedAt: d.now().UnixMilli()}
	if p.NodeID == "" {
		return p, fmt.Errorf("node id required")
	}
	osInfo := windowsWMICKV(ctx, "os", "get", "Caption,Version,TotalVisibleMemorySize,FreePhysicalMemory", "/value")
	p.OSVersion = strings.TrimSpace(osInfo["Caption"] + " " + osInfo["Version"])
	cpu := windowsWMICKV(ctx, "cpu", "get", "Name", "/value")
	p.CPU = CPU{Name: strings.TrimSpace(cpu["Name"]), LogicalCores: runtime.NumCPU(), Architecture: runtime.GOARCH, Backends: []string{"cpu", "cpu/" + runtime.GOARCH}}
	if p.CPU.Name == "" {
		p.CPU.Name = strings.TrimSpace(os.Getenv("PROCESSOR_IDENTIFIER"))
	}
	if p.CPU.Name == "" {
		p.CPU.Name = "Windows CPU"
	}
	totalKB, _ := strconv.ParseInt(strings.TrimSpace(osInfo["TotalVisibleMemorySize"]), 10, 64)
	freeKB, _ := strconv.ParseInt(strings.TrimSpace(osInfo["FreePhysicalMemory"]), 10, 64)
	p.Memory = Memory{TotalBytes: totalKB * 1024, AvailableBytes: freeKB * 1024}
	if p.Memory.TotalBytes <= 0 || p.Memory.AvailableBytes <= 0 {
		return p, fmt.Errorf("unable to determine system RAM")
	}
	p.GPUs = d.detectWindowsGPUs(ctx)
	p.RuntimeProbes = d.detectWindowsRuntimes(ctx)
	if strings.TrimSpace(dataDir) == "" {
		dataDir = filepath.Join(os.Getenv("ProgramData"), "OnePane")
	}
	p.Storage = detectWindowsStorage(ctx, dataDir)
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

func windowsWMICKV(ctx context.Context, args ...string) map[string]string {
	out := map[string]string{}
	b, err := exec.CommandContext(ctx, "wmic.exe", args...).Output()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func detectWindowsStorage(ctx context.Context, path string) Storage {
	vol := filepath.VolumeName(filepath.Clean(path))
	if vol == "" {
		vol = "C:"
	}
	q := fmt.Sprintf("DeviceID='%s'", strings.ReplaceAll(vol, "'", ""))
	x := windowsWMICKV(ctx, "logicaldisk", "where", q, "get", "Size,FreeSpace", "/value")
	total, _ := strconv.ParseInt(x["Size"], 10, 64)
	free, _ := strconv.ParseInt(x["FreeSpace"], 10, 64)
	return Storage{Path: path, CapacityBytes: total, AvailableBytes: free}
}
