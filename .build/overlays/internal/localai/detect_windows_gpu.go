package localai

import (
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

func (d *Detector) detectWindowsGPUs(ctx context.Context) []GPU {
	var out []GPU
	vulkan := windowsCommandWorks(ctx, "vulkaninfo.exe", "--summary")
	sycl := windowsCommandWorks(ctx, "sycl-ls.exe")
	opencl := windowsCommandWorks(ctx, "clinfo.exe", "-l")
	if b, err := exec.CommandContext(ctx, "nvidia-smi.exe", "--query-gpu=index,uuid,name,memory.total,memory.free,driver_version", "--format=csv,noheader,nounits").Output(); err == nil {
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
			out = append(out, GPU{Vendor: "nvidia", DeviceID: strings.TrimSpace(p[1]), Name: strings.TrimSpace(p[2]), VRAMBytes: windowsMiB(p[3]), FreeVRAMBytes: windowsMiB(p[4]), DriverVersion: strings.TrimSpace(p[5]), Backend: "cuda", Backends: backs, DeviceIndex: idx})
		}
	}
	b, err := exec.CommandContext(ctx, "wmic.exe", "path", "win32_VideoController", "get", "Name,AdapterRAM,DriverVersion,PNPDeviceID", "/format:csv").Output()
	if err == nil {
		for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(strings.ToLower(line), "node,") {
				continue
			}
			cols := strings.Split(line, ",")
			if len(cols) < 5 {
				continue
			}
			name := strings.TrimSpace(cols[3])
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
			dup := false
			for _, g := range out {
				if (vendor == "nvidia" && g.Vendor == "nvidia") || strings.EqualFold(g.Name, name) {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			vram, _ := strconv.ParseInt(strings.TrimSpace(cols[1]), 10, 64)
			backs := []string{}
			preferred := ""
			if vendor == "amd" && windowsCommandWorks(ctx, "rocm-smi.exe", "--showproductname") {
				backs = append(backs, "rocm")
				preferred = "rocm"
			}
			if vendor == "intel" && sycl {
				backs = append(backs, "sycl")
				preferred = "sycl"
			}
			if vulkan {
				backs = windowsAppendUnique(backs, "vulkan")
				if preferred == "" {
					preferred = "vulkan"
				}
			}
			if opencl {
				backs = windowsAppendUnique(backs, "opencl")
				if preferred == "" {
					preferred = "opencl"
				}
			}
			out = append(out, GPU{Vendor: vendor, Name: name, VRAMBytes: vram, DriverVersion: strings.TrimSpace(cols[2]), DeviceID: strings.TrimSpace(cols[4]), Backend: preferred, Backends: backs, DeviceIndex: len(out)})
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

func (d *Detector) detectWindowsRuntimes(ctx context.Context) []RuntimeProbe {
	defs := []struct {
		name, bin string
		args      []string
	}{{"llama.cpp", "llama-server.exe", []string{"--version"}}, {"ollama", "ollama.exe", []string{"--version"}}, {"vllm", "vllm.exe", []string{"--version"}}, {"podman", "podman.exe", []string{"--version"}}, {"docker", "docker.exe", []string{"--version"}}, {"python", "python.exe", []string{"--version"}}}
	out := make([]RuntimeProbe, 0, len(defs))
	for _, x := range defs {
		rp := RuntimeProbe{Name: x.name}
		if path, err := exec.LookPath(x.bin); err == nil {
			rp.Installed = true
			rp.Executable = path
			if b, e := exec.CommandContext(ctx, x.bin, x.args...).Output(); e == nil {
				rp.Version = strings.TrimSpace(string(b))
			}
		}
		out = append(out, rp)
	}
	return out
}
func windowsCommandWorks(ctx context.Context, name string, args ...string) bool {
	_, err := exec.CommandContext(ctx, name, args...).Output()
	return err == nil
}
func windowsMiB(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v * 1024 * 1024
}
func windowsAppendUnique(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}
