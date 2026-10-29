//go:build integration

package recoveryinventory_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/recoveryinventory"
)

type provenancePlanRecorder struct {
	t     *testing.T
	db    *sql.DB
	plans []string
}

func (p *provenancePlanRecorder) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	p.t.Helper()
	rows, err := p.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		p.t.Fatal(err)
	}
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			p.t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		p.t.Fatal(err)
	}
	p.plans = append(p.plans, strings.Join(details, " | "))
	return p.db.QueryRowContext(ctx, query, args...)
}

func TestKnownTaskProvenanceActualGuardUsesIndexedAbsenceProbes(t *testing.T) {
	ctx := context.Background()
	db := recoveryinventory.OpenMigratedRecoveryFixture(t, ctx)
	var err error
	if _, err = db.SQL().Exec(`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Unrelated','active',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 256; i++ {
		if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,workspace_id,content_hash,media_type,size_bytes,storage_ref,confidentiality,residency,trust,status,metadata_json,created_at) VALUES(?,'ws','hash','text/plain',1,'not-read','internal','any','unverified_derived','archived',?,1)`, fmt.Sprintf("artifact%d", i), fmt.Sprintf(`{"task_id":"unrelated%d"}`, i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	recorder := &provenancePlanRecorder{t: t, db: db.SQL()}
	present, err := recoveryinventory.HasKnownTaskEffectProvenance(ctx, recorder, "absent-task")
	if err != nil || present {
		t.Fatalf("unexpected provenance: %t %v", present, err)
	}
	wantIndexes := []string{"idx_runtime_recovery_request_task", "idx_artifacts_recovery_task", "idx_library_versions_recovery_task", "idx_inference_agent_request_task"}
	if len(recorder.plans) != len(wantIndexes) {
		t.Fatalf("guard skipped known provenance families: %v", recorder.plans)
	}
	for i, index := range wantIndexes {
		indexed := strings.Contains(recorder.plans[i], "USING INDEX "+index+" ") || strings.Contains(recorder.plans[i], "USING COVERING INDEX "+index+" ")
		if !indexed || !strings.Contains(recorder.plans[i], "SEARCH ") || strings.Contains(recorder.plans[i], "TEMP B-TREE") {
			t.Fatalf("guard query lost bounded lookup: %s", recorder.plans[i])
		}
	}
	// Errors cannot be converted into an absence observation. Empty identity
	// must fail before a query; cancellation and unavailable storage propagate.
	if _, err = recoveryinventory.HasKnownTaskEffectProvenance(ctx, nil, ""); err == nil {
		t.Fatal("empty Task accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = recoveryinventory.HasKnownTaskEffectProvenance(cancelled, db.SQL(), "absent-task"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query became absence: %v", err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = recoveryinventory.HasKnownTaskEffectProvenance(ctx, db.SQL(), "absent-task"); err == nil {
		t.Fatal("unavailable DB became absence")
	}
}
