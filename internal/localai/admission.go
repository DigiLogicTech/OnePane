package localai

import (
	"errors"
	"fmt"
	"time"
)

// lowMemoryHostBytes is the RAM size of the supported 16 GiB laptop class.
// Hosts at or below this keep a larger operating reserve so admission cannot
// push the machine into swap.
const lowMemoryHostBytes int64 = 16 << 30

// CapacityShortage is a durable local wait. It carries the measured bytes
// so a task can show why it is paused. It is not a cloud route.
type CapacityShortage struct {
	Resource string
	Required int64
	LiveFree int64
	Reserve  int64
	Resident int64
	Usable   int64
	Waitable bool
}

func (e *CapacityShortage) Error() string {
	if e == nil {
		return "waiting for local model capacity"
	}
	resource := e.Resource
	if resource == "" {
		resource = "memory"
	}
	return fmt.Sprintf("waiting for local %s: need %d bytes, live free %d bytes, reserve %d bytes, other resident %d bytes, usable %d bytes; wait for a model to unload or choose a smaller model",
		resource, e.Required, e.LiveFree, e.Reserve, e.Resident, e.Usable)
}

// LiveResources is a point-in-time view of host RAM and accelerator memory.
// Tests supply a simulated 16 GiB / 6 GiB machine through ResourceProbe.
type LiveResources struct {
	Memory Memory
	GPUs   []GPU
}

// hostRAMReserve is the memory OnePane leaves unused.
// Machines with 16 GiB or less keep max(1 GiB, 15%) capped at 2 GiB.
// Larger hosts keep the historical min(10%, 1 GiB) ceiling.
func hostRAMReserve(total int64) int64 {
	if total <= 0 {
		return 0
	}
	const gib = int64(1) << 30
	if total <= lowMemoryHostBytes {
		reserve := total * 15 / 100
		if total >= 4*gib && reserve < gib {
			reserve = gib
		}
		if reserve > 2*gib {
			reserve = 2 * gib
		}
		if reserve >= total {
			reserve = total / 5
			if reserve < 1 {
				reserve = 1
			}
		}
		return reserve
	}
	reserve := total / 10
	if reserve > gib {
		reserve = gib
	}
	if reserve < 1 {
		reserve = 1
	}
	return reserve
}

// RAMAdmission is the decision for one incoming model's host-RAM claim.
// KeptResident is memory that will not be unloaded (busy, pinned, or
// unresolved). Reclaimable is idle unpinned memory that can be freed now.
type RAMAdmission struct {
	Required, Total, LiveFree, Reserve int64
	KeptResident, Reclaimable, Usable  int64
	Fits                               bool
}

func evaluateRAMAdmission(required, total, liveFree, keptResident, reclaimable int64) RAMAdmission {
	out := RAMAdmission{
		Required: required, Total: total, LiveFree: liveFree,
		Reserve: hostRAMReserve(total), KeptResident: keptResident, Reclaimable: reclaimable,
	}
	if required <= 0 || total <= 0 || liveFree < 0 || keptResident < 0 || reclaimable < 0 {
		return out
	}
	if !cpuModelFitsConcurrentRAM(required, total, keptResident) {
		out.Usable = 0
		return out
	}
	projected := liveFree
	if reclaimable > 0 {
		sum, ok := addManagedMemoryBytes(liveFree, reclaimable)
		if !ok {
			return out
		}
		projected = sum
	}
	usable := projected - out.Reserve
	if usable < 0 {
		usable = 0
	}
	out.Usable = usable
	out.Fits = required <= usable
	return out
}

func (d RAMAdmission) shortage() *CapacityShortage {
	return &CapacityShortage{
		Resource: "ram",
		Required: d.Required,
		LiveFree: d.LiveFree,
		Reserve:  d.Reserve,
		Resident: d.KeptResident,
		Usable:   d.Usable,
		Waitable: true,
	}
}

// projectedLiveFree combines an OS reading taken after unload with the
// estimate of memory just released. If the OS has already reported the
// increase, that reading wins. If it has not moved yet, the estimate is
// added once so a swap is not rejected for a stale sample.
func projectedLiveFree(before, after, reclaimed int64) int64 {
	if after < 0 {
		after = 0
	}
	if before < 0 {
		before = 0
	}
	if after > before {
		return after
	}
	if reclaimed <= 0 {
		return after
	}
	sum, ok := addManagedMemoryBytes(before, reclaimed)
	if !ok || sum < after {
		return after
	}
	return sum
}

// placementHostRAM is the host-RAM claim of a placement. CPU-only uses the
// full model estimate. Partial and hybrid offload count only the CPU
// devices, which is the part that still occupies system RAM.
func placementHostRAM(mode PlacementMode, memoryRequired int64, plan PlacementPlan) int64 {
	if mode == "" {
		mode = plan.Mode
	}
	if mode == PlacementCPUOnly {
		if memoryRequired > 0 {
			return memoryRequired
		}
		return placementCPUAllocated(plan)
	}
	return placementCPUAllocated(plan)
}

func placementCPUAllocated(plan PlacementPlan) int64 {
	var n int64
	for _, d := range plan.Devices {
		if d.Kind != "cpu" || d.AllocatedBytes <= 0 {
			continue
		}
		next, ok := addManagedMemoryBytes(n, d.AllocatedBytes)
		if !ok {
			return 0
		}
		n = next
	}
	return n
}

// modelLoadTimeout grows with the bytes that must be read into RAM.
// The baseline stays 120s. Added time assumes a slow 40 MiB/s drive so a
// multi-gigabyte model is not failed while it is still loading. The wait
// is capped so a corrupt size cannot block startup forever.
func modelLoadTimeout(sizeBytes int64) time.Duration {
	const base = 120 * time.Second
	const cap = 20 * time.Minute
	const bytesPerSecond = 40 << 20
	if sizeBytes <= 0 {
		return base
	}
	seconds := sizeBytes / bytesPerSecond
	if sizeBytes%bytesPerSecond != 0 {
		seconds++
	}
	d := base + time.Duration(seconds)*time.Second
	if d < base || d > cap {
		return cap
	}
	return d
}

func vramShortage(required, liveFree, resident, usable int64) *CapacityShortage {
	return &CapacityShortage{
		Resource: "vram",
		Required: required,
		LiveFree: liveFree,
		Reserve:  0,
		Resident: resident,
		Usable:   usable,
		Waitable: true,
	}
}

// liveAcceleratorFits reports whether each requested accelerator allocation
// fits in measured free VRAM plus memory that idle models can release.
// Devices without a free-VRAM sample are left to the total-capacity check.
func liveAcceleratorFits(target, live, alreadyFreed map[string]int64, idle []residentRuntime) bool {
	extra := map[string]int64{}
	for k, n := range alreadyFreed {
		extra[k] = n
	}
	for _, r := range idle {
		for _, d := range r.Placement.Devices {
			if d.Kind != "accelerator" || d.AllocatedBytes <= 0 {
				continue
			}
			k := placementKey(d)
			if _, known := live[k]; !known {
				continue
			}
			next, ok := addManagedMemoryBytes(extra[k], d.AllocatedBytes)
			if !ok {
				return false
			}
			extra[k] = next
		}
	}
	for k, n := range target {
		free, known := live[k]
		if !known {
			continue
		}
		have := free
		if extra[k] > 0 {
			sum, ok := addManagedMemoryBytes(free, extra[k])
			if !ok {
				return false
			}
			have = sum
		}
		if n > have {
			return false
		}
	}
	return true
}

func constrainedVRAMShortage(target, live, used map[string]int64) *CapacityShortage {
	var worst *CapacityShortage
	for k, n := range target {
		free, known := live[k]
		if !known {
			free = 0
		}
		resident := used[k]
		usable := free
		if resident < 0 {
			resident = 0
		}
		short := &CapacityShortage{Resource: "vram", Required: n, LiveFree: free, Resident: resident, Usable: usable, Waitable: true}
		if worst == nil || n-usable > worst.Required-worst.Usable {
			worst = short
		}
	}
	if worst == nil {
		return &CapacityShortage{Resource: "vram", Waitable: true}
	}
	return worst
}

// placementChangeInFlight is true when a CPU/GPU move would interrupt work
// that has already been admitted. Draining is included: it is an in-flight stop.
func placementChangeInFlight(status string, activeLeases int) bool {
	if activeLeases > 0 {
		return true
	}
	switch status {
	case string(RuntimeStarting), string(RuntimeBusy), string(RuntimeDraining):
		return true
	default:
		return false
	}
}

var errPlacementInFlight = errors.New("compute placement change refused while a request is in flight; wait for it to finish")
