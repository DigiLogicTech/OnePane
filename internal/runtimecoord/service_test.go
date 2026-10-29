package runtimecoord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type moveClock struct{ at int64 }

func (c *moveClock) Now() time.Time   { return time.UnixMilli(c.at).UTC() }
func (c *moveClock) UnixMilli() int64 { return c.at }

func coordinatorFixture(t *testing.T) *Service {
	return coordinatorAt(t, clock.Real{})
}

func coordinatorAt(t *testing.T, clk clock.Clock) *Service {
	t.Helper()
	db, err := sqlitestore.Open(t.TempDir() + "/queue.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node','Local',1,'fp','local','LOCAL_TRUSTED','{}','{}',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	infer := inference.NewService(db.SQL(), db, clock.Real{})
	model, err := infer.RegisterModel(context.Background(), inference.RegisterModelCommand{ModelRef: "queue-fixture", TrustState: inference.ModelUserTrusted})
	if err != nil {
		t.Fatal(err)
	}
	node := "node"
	dep, err := infer.RegisterDeployment(context.Background(), inference.RegisterDeploymentCommand{ModelID: model.ID, NodeID: &node})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`UPDATE model_deployments SET id='model' WHERE id=?`, dep.ID); err != nil {
		t.Fatal(err)
	}
	return NewService(db.SQL(), clk)
}

func localRequest(id, node string) inference.RuntimeScheduleRequest {
	return inference.RuntimeScheduleRequest{WorkspaceID: "tenant", InferenceRequestID: id,
		Deployment: inference.ModelDeployment{ID: "model", NodeID: &node}}
}

func waitQueued(t *testing.T, s *Service, id string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_execution_queue WHERE id=? AND state='queued'`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("request never entered the durable queue")
}

func TestSharedComputeQueuesTeamBehindOtherWorkAndReleasesOnCancellation(t *testing.T) {
	s := coordinatorFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	other, err := s.Acquire(ctx, localRequest("assistant", "node"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Release(context.Background(), nil)
	teamCtx, cancelTeam := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		lease, err := s.Acquire(teamCtx, localRequest("team", "node"))
		if lease != nil {
			lease.Release(context.Background(), nil)
		}
		result <- err
	}()
	waitQueued(t, s, "team")
	select {
	case err := <-result:
		t.Fatalf("Team bypassed occupied CPU: %v", err)
	default:
	}
	// A different Node's CPU remains usable while this Node is occupied.
	independent, err := s.Acquire(ctx, localRequest("other-node", "node-two"))
	if err != nil {
		t.Fatal(err)
	}
	independent.Release(context.Background(), nil)
	cancelTeam()
	if err = <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait: %v", err)
	}
	other.Release(context.Background(), nil)
	next, err := s.Acquire(ctx, localRequest("team-next", "node"))
	if err != nil {
		t.Fatal(err)
	}
	next.Release(context.Background(), nil)
	var state string
	if err = s.db.QueryRow(`SELECT state FROM runtime_execution_queue WHERE id='team'`).Scan(&state); err != nil || state != "cancelled" {
		t.Fatalf("queue state %q: %v", state, err)
	}
}

func TestAdmissionCancellationRaceDoesNotStrandComputeLease(t *testing.T) {
	s := coordinatorFixture(t)
	for i := 0; i < 40; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		id := fmt.Sprintf("race-%d", i)
		result := make(chan error, 1)
		// Stop scheduling after persistQueued, cancel, then permit scheduling.
		// Both ready branches can then win the original Acquire select.
		s.mu.Lock()
		go func() {
			lease, err := s.Acquire(ctx, localRequest(id, "node"))
			if lease != nil {
				lease.Release(context.Background(), nil)
			}
			result <- err
		}()
		waitQueued(t, s, id)
		cancel()
		s.mu.Unlock()
		select {
		case <-result:
		case <-time.After(3 * time.Second):
			t.Fatal("cancellation deadlock")
		}
		s.mu.Lock()
		count := len(s.active)
		s.mu.Unlock()
		if count != 0 {
			t.Fatalf("cancelled admission stranded %d active leases", count)
		}
	}
}

func TestHybridComputeClaimsAreIndependentOfPlacementDeviceOrder(t *testing.T) {
	s := coordinatorFixture(t)
	for _, devices := range []string{
		`[{"kind":"cpu","allocated_bytes":100},{"kind":"accelerator","device_id":"gpu-0","allocated_bytes":200},{"kind":"accelerator","device_id":"gpu-1","allocated_bytes":300}]`,
		`[{"kind":"accelerator","device_id":"gpu-0","allocated_bytes":200},{"kind":"cpu","allocated_bytes":100},{"kind":"accelerator","device_id":"gpu-1","allocated_bytes":300}]`,
		`[{"kind":"accelerator","device_id":"gpu-0","allocated_bytes":200},{"kind":"accelerator","device_id":"gpu-1","allocated_bytes":300},{"kind":"cpu","allocated_bytes":100}]`,
	} {
		req := localRequest("placement", "node")
		req.Deployment.RuntimeConfigJSON = json.RawMessage(`{"runtime_backend":"colibri","placement":{"devices":` + devices + `}}`)
		claim, err := s.claimForDeployment(context.Background(), req.Deployment)
		want := []string{"node:node:cpu", "node:node:gpu:gpu-0", "node:node:gpu:gpu-1"}
		if err != nil || claim.ComputeMode != "hybrid" || claim.RAMBytes != 100 || claim.VRAMBytes != 500 || !reflect.DeepEqual(claim.ResourceIDs, want) {
			t.Fatalf("placement order lost shared resources: %+v %v", claim, err)
		}
	}
}

func seedDeployment(t *testing.T, s *Service, id, config string) {
	t.Helper()
	var modelID string
	if err := s.db.QueryRow(`SELECT model_id FROM model_deployments WHERE id='model'`).Scan(&modelID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,status,deployment_fingerprint,revision,discovered_at,updated_at) VALUES(?,?,'node','llama',?,'ready',?,1,1,1)`, id, modelID, config, "fp-"+id); err != nil {
		t.Fatal(err)
	}
}

func markLoaded(t *testing.T, s *Service, deploymentID string) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO local_hardware_profiles(id,node_id,fingerprint,os_name,architecture,cpu_json,memory_json,accelerators_json,runtimes_json,storage_json,detected_at) VALUES('hw','node','hwfp','linux','amd64','{}','{}','[]','[]','{}',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO local_model_install_plans(id,node_id,hardware_profile_id,role_name,use_case,model_ref,source_ref,runtime_name,quantization,context_tokens,fit_level,run_mode,memory_required_bytes,disk_required_bytes,download_scratch_bytes,plan_json,status,revision,created_at,updated_at) VALUES('plan','node','hw','assistant','chat','m','src','llama','Q4',1024,'good','cpu',1024,1024,0,'{}','ready',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO managed_local_runtimes(id,node_id,runtime_name,runtime_version,install_root,executable_path,source_url,source_sha256,status,managed_by_harness,revision,updated_at) VALUES('rt','node','llama','1','/r','/r/llama','http://example.invalid/rt','aa','ready',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO managed_local_models(id,node_id,plan_id,deployment_id,runtime_id,model_ref,source_ref,local_path,status,revision,updated_at) VALUES('mm','node','plan',?,'rt','m','src','/m','ready',1,1)`, deploymentID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO local_runtime_instances(id,node_id,deployment_id,runtime_id,managed_model_id,bind_address,port,pid,status,process_fingerprint,launch_json,health_json,revision,created_at,updated_at) VALUES('lri','node',?,'rt','mm','127.0.0.1',8088,4242,'healthy','fp','{}','{}',1,1,1)`, deploymentID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO runtime_compute_profiles(deployment_id,compute_mode,backend,ram_bytes,gpu_device_ids_json,vram_bytes,cached,resident,metadata_json,updated_at) VALUES(?,'cpu','cpu',1024,'[]',0,0,0,'{}',1)`, deploymentID); err != nil {
		t.Fatal(err)
	}
}

func requestFor(id, deployment string) inference.RuntimeScheduleRequest {
	node := "node"
	return inference.RuntimeScheduleRequest{WorkspaceID: "tenant", InferenceRequestID: id,
		Deployment: inference.ModelDeployment{ID: deployment, NodeID: &node}, Priority: "normal"}
}

func TestQueuePrefersLiveLoadedModelOverStaticFlag(t *testing.T) {
	s := coordinatorFixture(t)
	seedDeployment(t, s, "cold", `{"compute":{"mode":"cpu","resident":true},"placement":{"mode":"cpu_only","devices":[{"kind":"cpu"}]}}`)
	seedDeployment(t, s, "hot", `{"compute":{"mode":"cpu","resident":false},"placement":{"mode":"cpu_only","devices":[{"kind":"cpu"}]}}`)
	markLoaded(t, s, "hot")
	if _, err := s.db.Exec(`INSERT INTO runtime_compute_profiles(deployment_id,compute_mode,backend,ram_bytes,gpu_device_ids_json,vram_bytes,cached,resident,metadata_json,updated_at) VALUES('cold','cpu','cpu',1024,'[]',0,0,1,'{}',1)`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	holder, err := s.Acquire(ctx, localRequest("holder", "node"))
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		name string
		err  error
	}
	got := make(chan result, 2)
	start := func(name, deployment string) {
		go func() {
			lease, err := s.Acquire(ctx, requestFor("", deployment))
			if lease != nil {
				lease.Release(context.Background(), nil)
			}
			got <- result{name, err}
		}()
	}
	start("cold", "cold")
	waitQueuedCount(t, s, 1)
	start("hot", "hot")
	waitQueuedCount(t, s, 2)
	holder.Release(context.Background(), nil)
	select {
	case first := <-got:
		if first.err != nil || first.name != "hot" {
			t.Fatalf("loaded model was not preferred: %+v", first)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queue did not wake a waiter when the holder released")
	}
}

func TestQueueAgingLetsUnloadedModelRun(t *testing.T) {
	clk := &moveClock{}
	s := coordinatorAt(t, clk)
	seedDeployment(t, s, "cold", `{"placement":{"mode":"cpu_only","devices":[{"kind":"cpu"}]}}`)
	seedDeployment(t, s, "hot", `{"placement":{"mode":"cpu_only","devices":[{"kind":"cpu"}]}}`)
	markLoaded(t, s, "hot")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	holder, err := s.Acquire(ctx, localRequest("holder", "node"))
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 2)
	go func() {
		lease, err := s.Acquire(ctx, requestFor("", "cold"))
		if err != nil {
			got <- err.Error()
			return
		}
		got <- "cold"
		lease.Release(context.Background(), nil)
	}()
	waitQueuedCount(t, s, 1)
	clk.at = 16 * int64(time.Minute/time.Millisecond)
	go func() {
		lease, err := s.Acquire(ctx, requestFor("", "hot"))
		if err != nil {
			got <- err.Error()
			return
		}
		got <- "hot"
		lease.Release(context.Background(), nil)
	}()
	waitQueuedCount(t, s, 2)
	clk.at = 31 * int64(time.Minute/time.Millisecond)
	holder.Release(context.Background(), nil)
	select {
	case first := <-got:
		if first != "cold" {
			t.Fatalf("aging did not let the older unloaded model run: %s", first)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("aged waiter was not woken")
	}
}

func TestQueueIDsDoNotCollideInTheSameMillisecond(t *testing.T) {
	clk := &moveClock{at: 1000}
	s := coordinatorAt(t, clk)
	seedDeployment(t, s, "other", `{"placement":{"mode":"cpu_only","devices":[{"kind":"cpu"}]}}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := s.Acquire(ctx, requestFor("", "model"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release(context.Background(), nil)
	done := make(chan struct{})
	go func() {
		lease, err := s.Acquire(ctx, requestFor("", "other"))
		if lease != nil {
			lease.Release(context.Background(), nil)
		}
		if err != nil && ctx.Err() == nil {
			t.Errorf("second acquire: %v", err)
		}
		close(done)
	}()
	waitQueuedCount(t, s, 1)
	var n int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM runtime_execution_queue`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("queue rows = %d: %v", n, err)
	}
	var same int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM runtime_execution_queue WHERE id='runtime-1000'`).Scan(&same); err != nil || same != 0 {
		t.Fatalf("colliding runtime-1000 rows = %d: %v", same, err)
	}
}

func TestRecoverAfterRestartClearsPhantomRunningAndQueued(t *testing.T) {
	s := coordinatorFixture(t)
	if _, err := s.db.Exec(`INSERT INTO runtime_execution_queue(id,deployment_id,priority,state,safe_boundary,resource_claim_json,queued_at,revision) VALUES
		('run','model','normal','running','unsafe','{}',1,1),
		('wait','model','normal','queued','safe_pause','{}',2,1),
		('done','model','normal','completed','complete','{}',3,1)`); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverAfterRestart(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.Query(`SELECT id,state FROM runtime_execution_queue ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var id, state string
		if err = rows.Scan(&id, &state); err != nil {
			t.Fatal(err)
		}
		got[id] = state
	}
	if got["run"] != "failed" || got["wait"] != "failed" || got["done"] != "completed" {
		t.Fatalf("phantom rows remained: %+v", got)
	}
	var running int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM runtime_execution_queue WHERE state IN ('queued','running')`).Scan(&running); err != nil || running != 0 {
		t.Fatalf("live-looking rows after restart: %d %v", running, err)
	}
}

func TestPersistRunningFailureDoesNotGrantAPhantomLease(t *testing.T) {
	s := coordinatorFixture(t)
	if _, err := s.db.Exec(`CREATE TRIGGER deny_running BEFORE UPDATE ON runtime_execution_queue
		WHEN NEW.state='running' BEGIN SELECT RAISE(ABORT,'busy'); END`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_, err := s.Acquire(ctx, localRequest("phantom", "node"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("acquire returned %v", err)
	}
	s.mu.Lock()
	active := len(s.active)
	s.mu.Unlock()
	if active != 0 {
		t.Fatalf("phantom active leases: %d", active)
	}
	var state string
	if err = s.db.QueryRow(`SELECT state FROM runtime_execution_queue WHERE id='phantom'`).Scan(&state); err != nil || state == "running" {
		t.Fatalf("row state %q: %v", state, err)
	}
}

func waitQueuedCount(t *testing.T, s *Service, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM runtime_execution_queue WHERE state='queued'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("queued rows never reached %d", want)
}
