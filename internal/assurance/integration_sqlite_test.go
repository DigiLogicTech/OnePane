//go:build integration

package assurance

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/observation"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

func TestResolvedVerificationFinalizesAfterRestartRecovery(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := int64(1_700_000_000_000)
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES(?, 'system','Assurance Verifier','active',1,?,?)`, VerifierPrincipal, now, now); err != nil {
		t.Fatal(err)
	}
	spec := `{"assurance_version":1,"source":"integration-test","proposed_result":{"ok":true},"criteria":{"result_checks":[{"pointer":"/ok","operator":"equals","value":true}]},"evidence":{}}`
	result := `{"checks":[{"kind":"result_json","status":"pass"}]}`
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO verifications(id,workspace_id,subject_ref,required_level,achieved_level,status,spec_json,result_json,verified_by,started_at,completed_at,revision) VALUES('ver','ws','test','V1','V1','pass',?,?,?,?,?,2)`, spec, result, VerifierPrincipal, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO assurance_runs(id,verification_id,workspace_id,status,required_level,achieved_level,result_json,revision,started_at,updated_at) VALUES('ar','ver','ws','running','V1','V1','{}',1,?,?)`, now, now); err != nil {
		t.Fatal(err)
	}

	clk := clock.Real{}
	svc := New(db.SQL(), db, clk, verification.NewService(db.SQL(), db, clk), task.NewService(db.SQL(), db, clk), artifact.NewService(db.SQL(), db, nil, clk), observation.NewService(db.SQL(), db, clk), nil)
	recovered, err := svc.RecoverInterrupted(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d", recovered)
	}
	var state string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM assurance_runs WHERE id='ar'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != string(RunQueued) {
		t.Fatalf("post-recovery state=%s", state)
	}

	out, err := svc.Tick(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Status != RunPassed {
		t.Fatalf("tick=%+v", out)
	}
	if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM assurance_runs WHERE id='ar'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != string(RunPassed) {
		t.Fatalf("final state=%s", state)
	}
}
