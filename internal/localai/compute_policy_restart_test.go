package localai

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage"

	_ "modernc.org/sqlite"
)

type scriptTransactor struct {
	failOn int
	calls  int
}

func (s *scriptTransactor) Within(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	s.calls++
	if s.failOn == s.calls {
		return errors.New("transaction failed")
	}
	if fn != nil {
		return fn(ctx, nil)
	}
	return nil
}

func TestCommitThenRestartLeavesRuntimeRunningWhenTransactionFails(t *testing.T) {
	tx := &scriptTransactor{failOn: 1}
	stopped := 0
	started := 0
	err := commitThenRestart(context.Background(), tx,
		func(context.Context) error { stopped++; return nil },
		func(context.Context) error { started++; return nil },
		true, false,
		func(context.Context, storage.Tx) error { return nil },
		func(context.Context, storage.Tx) error { return nil },
	)
	if err == nil || stopped != 0 || started != 0 {
		t.Fatalf("failed transaction stopped=%d started=%d err=%v", stopped, started, err)
	}
}

func TestCommitThenRestartRefusesInFlightBeforeAnyWrite(t *testing.T) {
	tx := &scriptTransactor{}
	err := commitThenRestart(context.Background(), tx,
		func(context.Context) error { t.Fatal("stopped an in-flight model"); return nil },
		func(context.Context) error { t.Fatal("started during an in-flight change"); return nil },
		true, true,
		func(context.Context, storage.Tx) error {
			t.Fatal("wrote placement while a request was in flight")
			return nil
		},
		func(context.Context, storage.Tx) error { return nil },
	)
	if !errors.Is(err, errPlacementInFlight) || tx.calls != 0 {
		t.Fatalf("in-flight change: %v calls=%d", err, tx.calls)
	}
}

func TestCommitThenRestartRollsBackWhenRestartFails(t *testing.T) {
	tx := &scriptTransactor{}
	starts := 0
	restores := 0
	err := commitThenRestart(context.Background(), tx,
		func(context.Context) error { return nil },
		func(context.Context) error {
			starts++
			if starts == 1 {
				return errors.New("new placement failed")
			}
			return nil
		},
		true, false,
		func(context.Context, storage.Tx) error { return nil },
		func(context.Context, storage.Tx) error { restores++; return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "previous runtime restored") || restores != 1 || starts != 2 {
		t.Fatalf("restart failure left the model stopped: starts=%d restores=%d err=%v", starts, restores, err)
	}
}

func TestSetComputePolicyDoesNotStopBusyModel(t *testing.T) {
	db, err := sql.Open("sqlite", t.TempDir()+"/policy.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE local_model_install_plans(id TEXT PRIMARY KEY, runtime_name TEXT, memory_required_bytes INTEGER)`,
		`CREATE TABLE managed_local_models(deployment_id TEXT, plan_id TEXT, node_id TEXT, status TEXT)`,
		`CREATE TABLE local_runtime_instances(deployment_id TEXT, status TEXT)`,
		`INSERT INTO local_model_install_plans VALUES('plan','llama',1024)`,
		`INSERT INTO managed_local_models VALUES('model','plan','node','ready')`,
		`INSERT INTO local_runtime_instances VALUES('model','busy')`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	sup := NewRuntimeSupervisor(db, nil, clock.Real{}, t.TempDir(), idleProcessBackend{})
	svc := &Service{db: db, clock: clock.Real{}, supervisor: sup}
	_, err = svc.SetComputePolicy(context.Background(), ComputePolicyCommand{
		DeploymentID: "model", Preference: "require_cpu", ActorPrincipalID: "operator",
	})
	if !errors.Is(err, errPlacementInFlight) {
		t.Fatalf("busy placement change: %v", err)
	}
	var status string
	if err = db.QueryRow(`SELECT status FROM local_runtime_instances WHERE deployment_id='model'`).Scan(&status); err != nil || status != "busy" {
		t.Fatalf("busy model became %q: %v", status, err)
	}
}

func TestComputePolicySourceStopsOnlyAfterCommit(t *testing.T) {
	raw, err := os.ReadFile("compute_policy.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "func commitThenRestart(")
	if start < 0 {
		t.Fatal("placement restart helper missing")
	}
	scope := text[start:]
	if end := strings.Index(scope, "\nfunc "); end > 0 {
		scope = scope[:end]
	}
	within := strings.Index(scope, "tx.Within(ctx, apply)")
	stop := strings.Index(scope, "stop(ctx)")
	if within < 0 || stop < within {
		t.Fatal("runtime is signalled before the placement transaction commits")
	}
	flight := strings.Index(scope, "if inFlight {")
	if flight < 0 || flight > within {
		t.Fatal("in-flight placement change is not refused before the transaction")
	}
}
