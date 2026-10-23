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

func coordinatorFixture(t *testing.T) *Service {
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
	return NewService(db.SQL(), clock.Real{})
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
