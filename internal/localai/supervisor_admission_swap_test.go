package localai

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"

	_ "modernc.org/sqlite"
)

type idleProcessBackend struct{}

func (idleProcessBackend) Start(context.Context, LaunchSpec) (int, error) {
	return 0, errors.New("admission test does not launch a runtime")
}
func (idleProcessBackend) Signal(int, os.Signal) error { return nil }
func (idleProcessBackend) Alive(int) bool              { return false }

func admissionDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/admit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`CREATE TABLE local_hardware_profiles(node_id TEXT, accelerators_json TEXT, memory_json TEXT, detected_at INTEGER)`,
		`CREATE TABLE local_model_install_plans(id TEXT PRIMARY KEY, memory_required_bytes INTEGER, plan_json TEXT)`,
		`CREATE TABLE managed_local_models(deployment_id TEXT, plan_id TEXT)`,
		`CREATE TABLE model_deployments(id TEXT PRIMARY KEY, runtime_config_json TEXT, runtime_name TEXT, residency_state TEXT, updated_at INTEGER, revision INTEGER)`,
		`CREATE TABLE local_runtime_instances(
			id TEXT PRIMARY KEY, node_id TEXT, deployment_id TEXT, runtime_id TEXT, managed_model_id TEXT,
			bind_address TEXT, port INTEGER, pid INTEGER, status TEXT, process_fingerprint TEXT,
			launch_json TEXT, health_json TEXT, started_at INTEGER, last_seen_at INTEGER, stopped_at INTEGER,
			failure_reason TEXT, revision INTEGER, created_at INTEGER, updated_at INTEGER)`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	return db
}

func insertPlan(t *testing.T, db *sql.DB, id string, bytes int64, plan string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO local_model_install_plans VALUES(?,?,?)`, id, bytes, plan); err != nil {
		t.Fatal(err)
	}
}

func insertInstance(t *testing.T, db *sql.DB, deployment, plan, status, config string, pinned bool, pid int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO managed_local_models VALUES(?,?)`, deployment, plan); err != nil {
		t.Fatal(err)
	}
	raw := config
	if pinned {
		raw = `{"colibri_pinned":true}`
	}
	if raw == "" {
		raw = `{}`
	}
	if _, err := db.Exec(`INSERT INTO model_deployments VALUES(?,?,?,?,?,?)`, deployment, raw, "llama", "resident", 10, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO local_runtime_instances VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"inst-"+deployment, "node", deployment, "rt", "mm", "127.0.0.1", 2000+len(deployment), pid, status, "fp",
		`{}`, `{}`, 10, 10, nil, nil, 1, 10, 10); err != nil {
		t.Fatal(err)
	}
}

func simulatedMachine(totalRAM, freeRAM, totalVRAM, freeVRAM int64) func(context.Context) (LiveResources, error) {
	return func(context.Context) (LiveResources, error) {
		return LiveResources{
			Memory: Memory{TotalBytes: totalRAM, AvailableBytes: freeRAM},
			GPUs: []GPU{{
				Vendor: "nvidia", Name: "GTX 1060", DeviceID: "gpu0", Backend: "cuda",
				VRAMBytes: totalVRAM, FreeVRAMBytes: freeVRAM, DeviceIndex: 0,
			}},
		}, nil
	}
}

func newAdmissionSupervisor(t *testing.T, db *sql.DB, probe func(context.Context) (LiveResources, error)) *RuntimeSupervisor {
	t.Helper()
	s := NewRuntimeSupervisor(db, nil, clock.Real{}, t.TempDir(), idleProcessBackend{})
	s.resourceProbe = probe
	return s
}

func TestSixteenGigabyteHostRefusesModelThatIgnoresLiveFreeRAM(t *testing.T) {
	gib := int64(1) << 30
	db := admissionDB(t)
	insertPlan(t, db, "big", 8*gib, `{"placement":{"mode":"cpu_only"}}`)
	if _, err := db.Exec(`INSERT INTO managed_local_models VALUES('incoming','big')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO local_hardware_profiles VALUES('node','[]',?,1)`,
		`{"total_bytes":17179869184,"available_bytes":17179869184}`); err != nil {
		t.Fatal(err)
	}
	s := newAdmissionSupervisor(t, db, simulatedMachine(16*gib, (13*gib)/10, 0, 0))
	err := s.ensureCapacity(context.Background(), "incoming", "node")
	var shortage *CapacityShortage
	if !errors.As(err, &shortage) || shortage.Resource != "ram" || shortage.Required != 8*gib {
		t.Fatalf("live free RAM was not enforced: %v", err)
	}
	if shortage.LiveFree != (13*gib)/10 || shortage.Reserve != 2*gib {
		t.Fatalf("shortage did not report the live numbers: %+v", shortage)
	}
}

func TestCPUOnlySwapUnloadsIdleModelAndKeepsBusyAndPinned(t *testing.T) {
	gib := int64(1) << 30
	db := admissionDB(t)
	plan := `{"placement":{"mode":"cpu_only","devices":[{"kind":"cpu","allocated_bytes":8589934592}]}}`
	insertPlan(t, db, "cpu", 8*gib, plan)
	insertInstance(t, db, "idle", "cpu", "healthy", `{}`, false, 50)
	insertInstance(t, db, "busy", "cpu", "busy", `{}`, false, 51)
	insertInstance(t, db, "pinned", "cpu", "healthy", ``, true, 52)
	if _, err := db.Exec(`INSERT INTO managed_local_models VALUES('incoming','cpu')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO local_hardware_profiles VALUES('node','[]','{"total_bytes":17179869184,"available_bytes":2147483648}',1)`); err != nil {
		t.Fatal(err)
	}
	// 2 GiB free cannot hold another 8 GiB model until the idle one is unloaded.
	// The busy and pinned models stay. Their 16 GiB of estimates still do not
	// fit with the incoming model, so this fixture uses only the idle model
	// as reclaimable and the others are asserted separately below.
	s := newAdmissionSupervisor(t, db, simulatedMachine(16*gib, 2*gib, 0, 0))
	// Busy plus pinned keep 16 GiB, so the incoming model cannot fit even
	// after the idle model leaves. Nothing may be stopped.
	err := s.ensureCapacity(context.Background(), "incoming", "node")
	var shortage *CapacityShortage
	if !errors.As(err, &shortage) {
		t.Fatalf("busy and pinned RAM was treated as free: %v", err)
	}
	for _, id := range []string{"idle", "busy", "pinned"} {
		var status string
		if err = db.QueryRow(`SELECT status FROM local_runtime_instances WHERE deployment_id=?`, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		want := "healthy"
		if id == "busy" {
			want = "busy"
		}
		if status != want {
			t.Fatalf("%s status became %s", id, status)
		}
	}

	if _, err = db.Exec(`DELETE FROM local_runtime_instances WHERE deployment_id IN ('busy','pinned')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DELETE FROM managed_local_models WHERE deployment_id IN ('busy','pinned')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DELETE FROM model_deployments WHERE id IN ('busy','pinned')`); err != nil {
		t.Fatal(err)
	}
	if err = s.ensureCapacity(context.Background(), "incoming", "node"); err != nil {
		t.Fatalf("idle CPU model was not swapped out: %v", err)
	}
	var status string
	if err = db.QueryRow(`SELECT status FROM local_runtime_instances WHERE deployment_id='idle'`).Scan(&status); err != nil || status != "stopped" {
		t.Fatalf("idle model status %q: %v", status, err)
	}
}

func TestPartialOffloadSwapOnSixGigabyteGPU(t *testing.T) {
	gib := int64(1) << 30
	db := admissionDB(t)
	residentPlan := `{"placement":{"mode":"cpu_offload","devices":[{"kind":"accelerator","device_id":"gpu0","allocated_bytes":4294967296},{"kind":"cpu","allocated_bytes":8589934592}]}}`
	incomingPlan := `{"placement":{"mode":"cpu_offload","devices":[{"kind":"accelerator","device_id":"gpu0","allocated_bytes":4294967296},{"kind":"cpu","allocated_bytes":6442450944}]}}`
	insertPlan(t, db, "resident", 12*gib, residentPlan)
	insertPlan(t, db, "incoming-plan", 10*gib, incomingPlan)
	insertInstance(t, db, "idle-gpu", "resident", "healthy", `{}`, false, 70)
	if _, err := db.Exec(`INSERT INTO managed_local_models VALUES('incoming','incoming-plan')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO local_hardware_profiles VALUES('node',?, ?,1)`,
		`[{"vendor":"nvidia","name":"GTX 1060","device_id":"gpu0","backend":"cuda","vram_bytes":6442450944,"free_vram_bytes":1073741824,"device_index":0}]`,
		`{"total_bytes":17179869184,"available_bytes":3221225472}`); err != nil {
		t.Fatal(err)
	}
	// 1.3 GiB free RAM cannot hold the 6 GiB CPU slice, and the idle model
	// alone cannot make room while a second copy of its RAM is also kept.
	// First prove the RAM slice is required: no resident, still too little free RAM.
	if _, err := db.Exec(`UPDATE local_runtime_instances SET status='stopped' WHERE deployment_id='idle-gpu'`); err != nil {
		t.Fatal(err)
	}
	s := newAdmissionSupervisor(t, db, simulatedMachine(16*gib, (13*gib)/10, 6*gib, 5*gib))
	err := s.ensureCapacity(context.Background(), "incoming", "node")
	var shortage *CapacityShortage
	if !errors.As(err, &shortage) || shortage.Resource != "ram" || shortage.Required != 6*gib {
		t.Fatalf("partial offload RAM was not admitted against live free RAM: %v", err)
	}

	// Restore the idle hybrid model and give the host 3 GiB free. Unloading
	// that model releases 8 GiB RAM and 4 GiB VRAM, which is enough for the
	// incoming partial-offload model on a 6 GiB GPU.
	if _, err = db.Exec(`UPDATE local_runtime_instances SET status='healthy', pid=70, revision=1 WHERE deployment_id='idle-gpu'`); err != nil {
		t.Fatal(err)
	}
	s.resourceProbe = simulatedMachine(16*gib, 3*gib, 6*gib, 1*gib)
	if err = s.ensureCapacity(context.Background(), "incoming", "node"); err != nil {
		t.Fatalf("6 GiB GPU swap did not unload the idle partial-offload model: %v", err)
	}
	var status string
	if err = db.QueryRow(`SELECT status FROM local_runtime_instances WHERE deployment_id='idle-gpu'`).Scan(&status); err != nil || status != "stopped" {
		t.Fatalf("gpu idle model status %q: %v", status, err)
	}
}

func TestSixGigabyteCardRefusesModelLargerThanLiveFreeVRAM(t *testing.T) {
	gib := int64(1) << 30
	db := admissionDB(t)
	plan := `{"placement":{"mode":"single_device","backend":"cuda","devices":[{"kind":"accelerator","device_id":"gpu0","backend":"cuda","allocated_bytes":4294967296}]}}`
	insertPlan(t, db, "gpu", 4*gib, plan)
	if _, err := db.Exec(`INSERT INTO managed_local_models VALUES('incoming','gpu')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO local_hardware_profiles VALUES('node','[]','{"total_bytes":17179869184,"available_bytes":12884901888}',1)`); err != nil {
		t.Fatal(err)
	}
	s := newAdmissionSupervisor(t, db, simulatedMachine(16*gib, 12*gib, 6*gib, 1*gib))
	err := s.ensureCapacity(context.Background(), "incoming", "node")
	var shortage *CapacityShortage
	if !errors.As(err, &shortage) || shortage.Resource != "vram" {
		t.Fatalf("6 GiB card with 1 GiB free admitted a 4 GiB model: %v", err)
	}
	if shortage.Required != 4*gib {
		t.Fatalf("vram shortage numbers: %+v", shortage)
	}
}

func TestHybridResidentRAMIsCounted(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`CREATE TABLE local_runtime_instances(deployment_id TEXT, node_id TEXT, status TEXT, pid INTEGER)`,
		`CREATE TABLE managed_local_models(deployment_id TEXT, plan_id TEXT)`,
		`CREATE TABLE local_model_install_plans(id TEXT, memory_required_bytes INTEGER, plan_json TEXT)`,
		`INSERT INTO local_model_install_plans VALUES('hy',10737418240,'{"placement":{"mode":"cpu_offload","devices":[{"kind":"accelerator","allocated_bytes":6442450944},{"kind":"cpu","allocated_bytes":4294967296}]}}')`,
		`INSERT INTO managed_local_models VALUES('hybrid','hy')`,
		`INSERT INTO local_runtime_instances VALUES('hybrid','local','healthy',9)`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := &RuntimeSupervisor{db: db}
	used, err := s.estimatedResidentCPUMemory(context.Background(), "local", "incoming")
	if err != nil || used != 4<<30 {
		t.Fatalf("hybrid host RAM = %d err=%v", used, err)
	}
}

func TestCapacityWaitWakesWhenModelIsReleasedAndDoesNotNameCloud(t *testing.T) {
	wake := make(chan struct{})
	notes := 0
	calls := 0
	inst, err := waitForLocalRuntime(context.Background(), func(context.Context) (RuntimeInstance, error) {
		calls++
		if calls == 1 {
			return RuntimeInstance{}, &CapacityShortage{Resource: "ram", Required: 4, LiveFree: 1, Reserve: 2, Usable: 0, Waitable: true}
		}
		return RuntimeInstance{ID: "loaded", Status: RuntimeHealthy}, nil
	}, func() <-chan struct{} { return wake }, func(_ context.Context, err error) {
		notes++
		if !strings.Contains(err.Error(), "need 4 bytes") || strings.Contains(strings.ToLower(err.Error()), "cloud") {
			t.Errorf("wait note: %v", err)
		}
		close(wake)
	})
	if err != nil || inst.ID != "loaded" || notes != 1 || calls != 2 {
		t.Fatalf("wait did not resume on release: inst=%+v err=%v notes=%d calls=%d", inst, err, notes, calls)
	}
}

func TestCapacityWaitStaysLocalWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitForLocalRuntime(ctx, func(context.Context) (RuntimeInstance, error) {
		return RuntimeInstance{}, &CapacityShortage{Resource: "vram", Required: 9, LiveFree: 1, Usable: 0, Waitable: true}
	}, func() <-chan struct{} {
		ch := make(chan struct{})
		return ch
	}, nil)
	var shortage *CapacityShortage
	if !errors.As(err, &shortage) || shortage.Resource != "vram" {
		t.Fatalf("cancelled wait lost the local shortage: %v", err)
	}
}
