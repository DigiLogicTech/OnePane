//go:build integration

package agentworker

import (
	"context"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/recoveryinventory"
)

// Interpret the row actually persisted by the production Worker, including
// failure injection and independently stored responses. Never manufacture a
// second expected journal from the classifier's own vocabulary.
func assertStoredWorkerReference(t *testing.T, w *Service, ctx context.Context, runID, kind, status, outcome, family, expectedID string) {
	t.Helper()
	var ref, detail string
	if err := w.db.QueryRowContext(ctx, `SELECT result_ref,detail_json FROM agent_worker_steps WHERE run_id=? AND step_kind=? AND status=? AND (?='' OR json_extract(detail_json,'$.outcome')=?)`, runID, kind, status, outcome, outcome).Scan(&ref, &detail); err != nil {
		t.Fatal(err)
	}
	got := recoveryinventory.ClassifyWorkerStepReferences(kind, status, &ref, []byte(detail))
	if got.State != "known_candidate" || len(got.References) != 1 || got.References[0].Family != family || got.References[0].ID != expectedID {
		t.Fatalf("production journal lost its typed result reference: %+v", got)
	}
}
