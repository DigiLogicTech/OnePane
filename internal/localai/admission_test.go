package localai

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHostRAMReserveKeepsHeadroomOn16GiB(t *testing.T) {
	gib := int64(1) << 30
	if got := hostRAMReserve(16 * gib); got != 2*gib {
		t.Fatalf("16 GiB reserve = %d, want 2 GiB", got)
	}
	if got := hostRAMReserve(8 * gib); got != (8*gib*15)/100 {
		t.Fatalf("8 GiB reserve = %d", got)
	}
	if got := hostRAMReserve(32 * gib); got != gib {
		t.Fatalf("32 GiB reserve = %d, want 1 GiB", got)
	}
	if got := hostRAMReserve(512 << 20); got <= 0 || got >= 512<<20 {
		t.Fatalf("small-host reserve %d left no usable RAM", got)
	}
}

func TestLiveFreeRAMRefusesModelThatFitsOnlyAgainstTotal(t *testing.T) {
	gib := int64(1) << 30
	// 16 GiB machine, 1.3 GiB free. The old total-minus-1GiB check would
	// admit an 8 GiB model. Live free must not.
	need := 8 * gib
	total := 16 * gib
	live := (13 * gib) / 10
	if !cpuModelFitsConcurrentRAM(need, total, 0) {
		t.Fatal("fixture no longer represents the total-RAM over-admit")
	}
	decision := evaluateRAMAdmission(need, total, live, 0, 0)
	if decision.Fits {
		t.Fatalf("admitted %d into live free %d usable %d", need, live, decision.Usable)
	}
	err := decision.shortage()
	text := err.Error()
	for _, part := range []string{
		"waiting for local ram",
		"need " + strconv.FormatInt(need, 10) + " bytes",
		"live free " + strconv.FormatInt(live, 10) + " bytes",
		"reserve " + strconv.FormatInt(decision.Reserve, 10) + " bytes",
	} {
		if !strings.Contains(text, part) {
			t.Fatalf("shortage %q missing %q", text, part)
		}
	}
	if strings.Contains(strings.ToLower(text), "cloud") {
		t.Fatalf("shortage offered a cloud route: %s", text)
	}
	// Same host with 12 GiB actually free can admit the model and still
	// keep the 2 GiB reserve.
	open := evaluateRAMAdmission(need, total, 12*gib, 0, 0)
	if !open.Fits || open.Usable != 10*gib {
		t.Fatalf("idle 16 GiB host rejected a model that fits: %+v", open)
	}
}

func TestPartialOffloadCountsOnlyHostRAM(t *testing.T) {
	gib := int64(1) << 30
	plan := PlacementPlan{Mode: PlacementCPUOffload, Devices: []PlacementDevice{
		{Kind: "accelerator", DeviceID: "gpu0", AllocatedBytes: 4 * gib},
		{Kind: "cpu", AllocatedBytes: 6 * gib},
	}}
	if got := placementHostRAM(PlacementCPUOffload, 10*gib, plan); got != 6*gib {
		t.Fatalf("partial offload RAM = %d, want 6 GiB", got)
	}
	if got := placementHostRAM(PlacementCPUOnly, 8*gib, PlacementPlan{Mode: PlacementCPUOnly}); got != 8*gib {
		t.Fatalf("cpu-only RAM = %d", got)
	}
	// 6 GiB of host RAM does not fit in 1.3 GiB free on a 16 GiB machine,
	// even when the GPU slice would fit in a 6 GiB card.
	decision := evaluateRAMAdmission(6*gib, 16*gib, (13*gib)/10, 0, 0)
	if decision.Fits {
		t.Fatal("partial offload RAM was ignored")
	}
}

func TestModelLoadTimeoutScalesWithSize(t *testing.T) {
	if got := modelLoadTimeout(0); got != 120*time.Second {
		t.Fatalf("empty model timeout %s", got)
	}
	small := modelLoadTimeout(1)
	if small <= 120*time.Second {
		t.Fatalf("tiny model kept the fixed timeout %s", small)
	}
	six := modelLoadTimeout(6 << 30)
	if six <= 120*time.Second || six >= 20*time.Minute {
		t.Fatalf("6 GiB load timeout %s", six)
	}
	if huge := modelLoadTimeout(80 << 30); huge != 20*time.Minute {
		t.Fatalf("huge model timeout %s", huge)
	}
}

func TestPlacementChangeRefusesInFlightStatuses(t *testing.T) {
	for _, status := range []string{string(RuntimeBusy), string(RuntimeDraining), string(RuntimeStarting)} {
		if !placementChangeInFlight(status, 0) {
			t.Fatalf("status %s was treated as idle", status)
		}
	}
	if !placementChangeInFlight(string(RuntimeHealthy), 1) {
		t.Fatal("healthy model with a live lease was treated as idle")
	}
	if placementChangeInFlight(string(RuntimeHealthy), 0) || placementChangeInFlight(string(RuntimeStopped), 0) {
		t.Fatal("idle model was treated as in flight")
	}
}
