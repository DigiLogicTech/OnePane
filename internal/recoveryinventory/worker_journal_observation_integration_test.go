//go:build integration

package recoveryinventory_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/recoveryinventory"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func workerJournalFixture(t *testing.T) (*sqlitestore.DB, recoveryinventory.WorkerJournalScope) {
	t.Helper()
	db := recoveryinventory.OpenMigratedRecoveryFixture(t, context.Background())
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1),('foreign','Foreign','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('worker','system','Worker','active',1,1,1),('other','system','Other','active',1,1,1)`,
		`INSERT INTO tasks(id,workspace_id,objective,state,completion_json,created_at,updated_at) VALUES('task','tenant','PRIVATE objective','blocked','{}',1,1),('other-task','tenant','Other','blocked','{}',1,1)`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,worker_principal_id,status,metadata_json) VALUES('attempt','task',1,'worker','interrupted','{}'),('other-attempt','other-task',1,'worker','interrupted','{}')`,
		`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) VALUES('run','tenant','task','attempt','worker','interrupted','general','inference.general','L1',24,3,3,'{}','{"PRIVATE_CONTINUATION":"kept private"}',1,1)`,
	} {
		if _, err := db.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db, recoveryinventory.WorkerJournalScope{WorkspaceID: "tenant", TaskID: "task", AttemptID: "attempt", RunID: "run"}
}

func insertJournalStep(t *testing.T, tx interface {
	Exec(string, ...any) (sql.Result, error)
}, number int, detail string) {
	t.Helper()
	if _, err := tx.Exec(`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,result_ref,detail_json,started_at,completed_at) VALUES(?,'run',?,'model','succeeded','response-artifact',?,1,2)`, fmt.Sprintf("step-%03d", number), number, detail); err != nil {
		t.Fatal(err)
	}
}

func observeJournal(t *testing.T, db *sqlitestore.DB, scope recoveryinventory.WorkerJournalScope) (recoveryinventory.WorkerJournalObservation, error) {
	t.Helper()
	tx, err := db.SQL().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	return recoveryinventory.ObserveWorkerJournalSlice(context.Background(), tx, scope)
}

func TestRecoveryWorkerJournalScopedReadOnlyAndStable(t *testing.T) {
	db, scope := workerJournalFixture(t)
	insertJournalStep(t, db.SQL(), 2, `{"message":"second"}`)
	insertJournalStep(t, db.SQL(), 1, `{"message":"PRIVATE_RESPONSE"}`)
	first, err := observeJournal(t, db, scope)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != recoveryinventory.WorkerJournalObservationVersion || len(first.Steps) != 2 || first.Fingerprint == "" || first.Steps[0].ID != "step-001" || first.Steps[0].ResultReferences.References[0].Family != "artifacts" {
		t.Fatalf("wrong ordered observation: %+v", first)
	}
	again, err := observeJournal(t, db, scope)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("unchanged journal unstable: %v", err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "PRIVATE_") {
		t.Fatal("private journal/Task text escaped metadata observation")
	}
	var state, attempt, run string
	var events, cases int
	if err = db.SQL().QueryRow(`SELECT t.state,a.status,r.status,(SELECT COUNT(*) FROM events),(SELECT COUNT(*) FROM task_recovery_cases) FROM tasks t JOIN task_attempts a ON a.task_id=t.id JOIN agent_worker_runs r ON r.attempt_id=a.id WHERE r.id='run'`).Scan(&state, &attempt, &run, &events, &cases); err != nil {
		t.Fatal(err)
	}
	if state != "blocked" || attempt != "interrupted" || run != "interrupted" || events != 0 || cases != 0 {
		t.Fatal("observation changed recovery/execution/audits")
	}
	// Unsupported result encodings stay visible; they are not silently filtered.
	if _, err = db.SQL().Exec(`UPDATE agent_worker_steps SET detail_json='{"future_field":"unreviewed"}' WHERE step_number=1`); err != nil {
		t.Fatal(err)
	}
	unsupported, err := observeJournal(t, db, scope)
	if err != nil || len(unsupported.Steps) != 2 || unsupported.Steps[0].ResultReferences.State != "unsupported" || len(unsupported.Steps[0].ResultReferences.References) != 0 || unsupported.Fingerprint == first.Fingerprint {
		t.Fatalf("unsupported row hidden: %+v %v", unsupported, err)
	}
}

func TestRecoveryWorkerJournalRejectsWrongScopeAndOwnership(t *testing.T) {
	db, scope := workerJournalFixture(t)
	for _, field := range []string{"tenant", "task", "attempt", "run", "project", "workspace", "session"} {
		t.Run(field, func(t *testing.T) {
			changed := scope
			other := "foreign-context"
			switch field {
			case "tenant":
				changed.WorkspaceID = "foreign"
			case "task":
				changed.TaskID = "other-task"
			case "attempt":
				changed.AttemptID = "other-attempt"
			case "run":
				changed.RunID = "missing"
			case "project":
				changed.ProjectID = &other
			case "workspace":
				changed.ProjectWorkspaceID = &other
			case "session":
				changed.ArtifactSessionID = &other
			}
			got, err := observeJournal(t, db, changed)
			if !errors.Is(err, recoveryinventory.ErrWorkerJournalUnavailable) || got.Fingerprint != "" || len(got.Steps) != 0 {
				t.Fatalf("scope accepted: %+v %v", got, err)
			}
		})
	}
	for _, q := range []string{`UPDATE task_attempts SET worker_principal_id='other' WHERE id='attempt'`, `UPDATE agent_worker_runs SET task_id='other-task' WHERE id='run'`, `UPDATE agent_worker_runs SET workspace_id='foreign' WHERE id='run'`} {
		tx, err := db.SQL().BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(q); err != nil {
			t.Fatal(err)
		}
		got, err := recoveryinventory.ObserveWorkerJournalSlice(context.Background(), tx, scope)
		if !errors.Is(err, recoveryinventory.ErrWorkerJournalUnavailable) || got.Fingerprint != "" {
			t.Fatalf("foreign lineage accepted: %+v %v", got, err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecoveryWorkerJournalFingerprintsAllStoredStepMetadata(t *testing.T) {
	db, scope := workerJournalFixture(t)
	insertJournalStep(t, db.SQL(), 1, `{"message":"ok"}`)
	first, err := observeJournal(t, db, scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE agent_worker_steps SET detail_json=' {"message":"ok"}'`,
		`UPDATE agent_worker_steps SET request_ref='uninterpreted-request'`,
		`UPDATE agent_worker_steps SET candidate_kind='model_deployment',candidate_id='candidate'`,
		`UPDATE agent_worker_steps SET proposal_type='wait'`,
		`UPDATE agent_worker_steps SET completed_at=NULL`,
		`UPDATE agent_worker_steps SET result_ref='different-response'`,
		`UPDATE agent_worker_runs SET continuation_json='{"new":"PRIVATE changed without revision"}'`,
		`UPDATE agent_worker_runs SET route_policy_json='{"enabled":true}'`,
		`UPDATE agent_worker_runs SET last_error=''`,
		`UPDATE agent_worker_runs SET completed_at=3`,
		`UPDATE tasks SET revision=revision+1 WHERE id='task'`,
		`UPDATE task_attempts SET status='failed' WHERE id='attempt'`,
	} {
		tx, err := db.SQL().BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(q); err != nil {
			t.Fatal(err)
		}
		changed, err := recoveryinventory.ObserveWorkerJournalSlice(context.Background(), tx, scope)
		if err != nil || changed.Fingerprint == first.Fingerprint {
			t.Fatalf("mutable payload missing from fingerprint: %s %v", q, err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecoveryWorkerJournalWholeSnapshotBoundsAndGaps(t *testing.T) {
	db, scope := workerJournalFixture(t)
	for i := 1; i <= 256; i++ {
		insertJournalStep(t, db.SQL(), i, `{"message":"ok"}`)
	}
	got, err := observeJournal(t, db, scope)
	if err != nil || len(got.Steps) != 256 {
		t.Fatalf("256step boundary failed: %d %v", len(got.Steps), err)
	}
	insertJournalStep(t, db.SQL(), 257, `{"message":"overflow"}`)
	got, err = observeJournal(t, db, scope)
	if !errors.Is(err, recoveryinventory.ErrWorkerJournalLimit) || got.Fingerprint != "" || len(got.Steps) != 0 {
		t.Fatal("257steps partially accepted")
	}
	if _, err = db.SQL().Exec(`DELETE FROM agent_worker_steps WHERE step_number IN (1,257)`); err != nil {
		t.Fatal(err)
	}
	got, err = observeJournal(t, db, scope)
	if !errors.Is(err, recoveryinventory.ErrWorkerJournalUnavailable) || got.Fingerprint != "" {
		t.Fatal("journal gap accepted")
	}
	if _, err = db.SQL().Exec(`DELETE FROM agent_worker_steps`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		insertJournalStep(t, db.SQL(), i, `{"message":"`+strings.Repeat("x", 60000)+`"}`)
	}
	got, err = observeJournal(t, db, scope)
	if !errors.Is(err, recoveryinventory.ErrWorkerJournalLimit) || got.Fingerprint != "" || len(got.Steps) != 0 {
		t.Fatal("whole byte budget bypassed")
	}
	if _, err = db.SQL().Exec(`DELETE FROM agent_worker_steps`); err != nil {
		t.Fatal(err)
	}
	insertJournalStep(t, db.SQL(), 1, `{"message":"`+strings.Repeat("é", 33000)+`"}`)
	got, err = observeJournal(t, db, scope)
	if !errors.Is(err, recoveryinventory.ErrWorkerJournalLimit) || got.Fingerprint != "" {
		t.Fatal("SQL byte cap counted characters or released truncated row")
	}
}

func TestRecoveryWorkerJournalSchemaIndexAndCancellationFences(t *testing.T) {
	db, scope := workerJournalFixture(t)
	insertJournalStep(t, db.SQL(), 1, `{"message":"ok"}`)
	for _, q := range []string{
		`ALTER TABLE agent_worker_steps ADD COLUMN unreviewed_effect_json TEXT`,
		`DROP INDEX idx_agent_worker_steps_run`,
		`DROP INDEX idx_agent_worker_steps_run; CREATE INDEX idx_agent_worker_steps_run ON agent_worker_steps(step_number,run_id)`,
		`DROP INDEX idx_agent_worker_steps_run; CREATE INDEX idx_agent_worker_steps_run ON agent_worker_steps(run_id,step_number) WHERE status='succeeded'`,
		`DROP INDEX idx_agent_worker_steps_run; CREATE INDEX idx_agent_worker_steps_run ON agent_worker_steps(run_id COLLATE NOCASE,step_number)`,
	} {
		tx, err := db.SQL().BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(q); err != nil {
			t.Fatal(err)
		}
		got, err := recoveryinventory.ObserveWorkerJournalSlice(context.Background(), tx, scope)
		if err == nil || got.Fingerprint != "" || len(got.Steps) != 0 {
			t.Fatalf("unreviewed shape/index accepted: %s", q)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.SQL().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = recoveryinventory.ObserveWorkerJournalSlice(ctx, tx, scope); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observation accepted: %v", err)
	}
	if _, err = recoveryinventory.ObserveWorkerJournalSlice(context.Background(), nil, scope); err == nil {
		t.Fatal("nontransactional observation accepted")
	}
}

func TestRecoveryWorkerJournalProjectScopeAndOwnedReturnValues(t *testing.T) {
	db, scope := workerJournalFixture(t)
	for _, q := range []string{
		`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('project','tenant','P','active','{}','{}','worker',1,1),('other-project','tenant','Other P','active','{}','{}','worker',1,1)`,
		`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('view','project','View',1,1),('other-view','other-project','Other',1,1)`,
		`INSERT INTO artifact_sessions(id,workspace_id,project_id,title,format,status,created_by,created_at,updated_at) VALUES('session','tenant','project','Session','text','active','worker',1,1),('foreign-session','foreign',NULL,'Foreign','text','active','worker',1,1)`,
		`UPDATE tasks SET project_id='project',project_workspace_id='view',artifact_session_id='session' WHERE id='task'`,
	} {
		if _, err := db.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	p, w, s := "project", "view", "session"
	scope.ProjectID = &p
	scope.ProjectWorkspaceID = &w
	scope.ArtifactSessionID = &s
	first, err := observeJournal(t, db, scope)
	if err != nil {
		t.Fatal(err)
	}
	p = "mutated caller memory"
	w = "mutated caller memory"
	s = "mutated caller memory"
	if *first.Scope.ProjectID != "project" || *first.Scope.ProjectWorkspaceID != "view" || *first.Scope.ArtifactSessionID != "session" {
		t.Fatal("caller memory changed returned provenance")
	}
	p, w, s = "project", "view", "session"
	for _, scenario := range []string{"workspace", "session", "tenant"} {
		tx, err := db.SQL().BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		changed := scope
		switch scenario {
		case "workspace":
			value := "other-view"
			changed.ProjectWorkspaceID = &value
			_, err = tx.Exec(`UPDATE tasks SET project_workspace_id='other-view' WHERE id='task'`)
		case "session":
			value := "foreign-session"
			changed.ArtifactSessionID = &value
			_, err = tx.Exec(`UPDATE tasks SET artifact_session_id='foreign-session' WHERE id='task'`)
		case "tenant":
			_, err = tx.Exec(`UPDATE projects SET workspace_id='foreign' WHERE id='project'`)
		}
		if err != nil {
			t.Fatal(err)
		}
		got, err := recoveryinventory.ObserveWorkerJournalSlice(context.Background(), tx, changed)
		if !errors.Is(err, recoveryinventory.ErrWorkerJournalUnavailable) || got.Fingerprint != "" {
			t.Fatalf("foreign Project context accepted: %s %v", scenario, err)
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecoveryWorkerJournalRetainsTransactionSnapshotAndIgnoresTempShadows(t *testing.T) {
	db, scope := workerJournalFixture(t)
	insertJournalStep(t, db.SQL(), 1, `{"message":"before"}`)
	db.SQL().SetMaxOpenConns(2)
	tx, err := db.SQL().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	first, err := recoveryinventory.ObserveWorkerJournalSlice(context.Background(), tx, scope)
	if err != nil {
		t.Fatal(err)
	}
	// Separate SQLite connection commits a changed payload without a revision.
	if _, err = db.SQL().Exec(`UPDATE agent_worker_steps SET detail_json='{"message":"after"}' WHERE id='step-001'`); err != nil {
		t.Fatal(err)
	}
	again, err := recoveryinventory.ObserveWorkerJournalSlice(context.Background(), tx, scope)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("observation mixed two WAL snapshots: %v", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	current, err := observeJournal(t, db, scope)
	if err != nil || current.Fingerprint == first.Fingerprint {
		t.Fatalf("fresh transaction missed changed payload: %v", err)
	}
	tx, err = db.SQL().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, q := range []string{`CREATE TEMP TABLE tasks(id TEXT)`, `CREATE TEMP TABLE agent_worker_runs(id TEXT)`, `CREATE TEMP TABLE agent_worker_steps(id TEXT)`} {
		if _, err = tx.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	shadowed, err := recoveryinventory.ObserveWorkerJournalSlice(context.Background(), tx, scope)
	if err != nil || !reflect.DeepEqual(shadowed, current) {
		t.Fatalf("TEMP schema redirected main observation: %v", err)
	}
}

func TestRecoveryWorkerJournalRejectsUnhashableBytesAndOversizedRun(t *testing.T) {
	db, scope := workerJournalFixture(t)
	insertJournalStep(t, db.SQL(), 1, `{"message":"ok"}`)
	for _, q := range []string{
		`UPDATE agent_worker_steps SET request_ref=CAST(x'ff' AS TEXT)`,
		`UPDATE agent_worker_runs SET last_error=CAST(x'fe' AS TEXT)`,
		`UPDATE agent_worker_steps SET candidate_id=printf('%0600d',1)`,
		`UPDATE agent_worker_runs SET continuation_json=json_object('huge',printf('%070000d',1))`,
	} {
		tx, err := db.SQL().BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(q); err != nil {
			t.Fatal(err)
		}
		got, err := recoveryinventory.ObserveWorkerJournalSlice(context.Background(), tx, scope)
		if err == nil || got.Fingerprint != "" || len(got.Steps) != 0 {
			t.Fatal("lossy UTF-8 replacement/oversized projection became a fingerprint")
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecoveryWorkerJournalUsesOwnedIndexDespiteUnrelatedHistory(t *testing.T) {
	db, scope := workerJournalFixture(t)
	insertJournalStep(t, db.SQL(), 1, `{"message":"ok"}`)
	if _, err := db.SQL().Exec(`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) SELECT 'other-run',workspace_id,'other-task','other-attempt',worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at FROM agent_worker_runs WHERE id='run'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1024) INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,detail_json,started_at) SELECT 'other-'||x,'other-run',x,'wait','waiting','{}',1 FROM n`); err != nil {
		t.Fatal(err)
	}
	for _, analyze := range []bool{false, true} {
		if analyze {
			if _, err := db.SQL().Exec(`ANALYZE`); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := db.SQL().Query(`EXPLAIN QUERY PLAN SELECT id FROM main.agent_worker_steps INDEXED BY idx_agent_worker_steps_run WHERE run_id=? ORDER BY step_number LIMIT 257`, scope.RunID)
		if err != nil {
			t.Fatal(err)
		}
		var plans []string
		for rows.Next() {
			var id, parent, unused int
			var plan string
			if err = rows.Scan(&id, &parent, &unused, &plan); err != nil {
				t.Fatal(err)
			}
			plans = append(plans, plan)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err = rows.Close(); err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(plans, " ")
		if !strings.Contains(plan, "SEARCH") || !strings.Contains(plan, "idx_agent_worker_steps_run") || strings.Contains(plan, "TEMP B-TREE") {
			t.Fatalf("lost bounded ordered lookup: %s", plan)
		}
		got, err := observeJournal(t, db, scope)
		if err != nil || len(got.Steps) != 1 {
			t.Fatalf("unrelated history leaked or blocked original journal: %v", err)
		}
	}
}
