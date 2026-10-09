//go:build integration

package agentworker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestSystemMembershipRunJournalAndCrashRecoveryAreDurable(t *testing.T) {
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

	clk := clock.Real{}
	tasks := task.NewService(db.SQL(), db, clk)
	svc := New(db.SQL(), db, clk, "node-local", tasks, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if err := svc.EnsureSystemPrincipals(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.ensureWorkspaceAccess(ctx, "ws"); err != nil {
		t.Fatal(err)
	}
	var members int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id='ws' AND principal_id IN (?,?,?) AND status='active'`, AuthorityPrincipal, WorkerPrincipal, VerifierPrincipal).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 3 {
		t.Fatalf("system memberships=%d", members)
	}

	taskRow, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "ws", Objective: "general autonomous job", SchedulingClass: task.ClassUserInteractive})
	if err != nil {
		t.Fatal(err)
	}
	taskRow, err = tasks.MarkReady(ctx, task.TransitionCommand{TaskID: taskRow.ID, ExpectedRevision: taskRow.Revision})
	if err != nil {
		t.Fatal(err)
	}
	run, res := svc.startRun(ctx, taskRow)
	if res.Error != "" {
		t.Fatal(res.Error)
	}
	if run.Status != RunRunning {
		t.Fatalf("run status=%s", run.Status)
	}
	if err := svc.journal(ctx, run.ID, "route", "succeeded", nil, nil, nil, nil, map[string]any{"candidate": "local"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.journal(ctx, run.ID, "model", "succeeded", nil, nil, nil, nil, map[string]any{"proposal": "wait"}); err != nil {
		t.Fatal(err)
	}
	var steps, minStep, maxStep int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*),MIN(step_number),MAX(step_number) FROM agent_worker_steps WHERE run_id=?`, run.ID).Scan(&steps, &minStep, &maxStep); err != nil {
		t.Fatal(err)
	}
	if steps != 2 || minStep != 1 || maxStep != 2 {
		t.Fatalf("journal count/range=%d/%d-%d", steps, minStep, maxStep)
	}

	count, err := svc.RecoverLostRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recovered=%d", count)
	}
	var runState string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM agent_worker_runs WHERE id=?`, run.ID).Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if runState != string(RunInterrupted) {
		t.Fatalf("run state=%s", runState)
	}
	taskRow, err = tasks.Get(ctx, taskRow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if taskRow.State != task.StateBlocked {
		t.Fatalf("task state=%s", taskRow.State)
	}
	var attemptState string
	if err := db.SQL().QueryRowContext(ctx, `SELECT status FROM task_attempts WHERE id=?`, run.AttemptID).Scan(&attemptState); err != nil {
		t.Fatal(err)
	}
	if attemptState != string(task.AttemptInterrupted) {
		t.Fatalf("attempt state=%s", attemptState)
	}
}

func TestCrashRecoveryRollsBackWhenAttemptCannotBeInterrupted(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"state.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=int64(1_700_000_000_000)
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 clk:=clock.Real{}
 tasks:=task.NewService(db.SQL(),db,clk)
 svc:=New(db.SQL(),db,clk,"node-local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=svc.ensureWorkspaceAccess(ctx,"ws");err!=nil{t.Fatal(err)}
 created,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"ws",Objective:"atomic recovery"})
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:created.ID,ExpectedRevision:created.Revision})
 if err!=nil{t.Fatal(err)}
 run,res:=svc.startRun(ctx,ready)
 if res.Error!=""{t.Fatal(res.Error)}
 // Simulate a corrupt/nonactive execution record so recovery must fail
 // AFTER finding a running Worker run but BEFORE writing any recovery event.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE task_attempts SET status='succeeded' WHERE id=?`,run.AttemptID);err!=nil{t.Fatal(err)}
 if count,err:=svc.RecoverLostRuns(ctx);err==nil||count!=0{
  t.Fatalf("recovery should fail atomically: count=%d err=%v",count,err)
 }
 var status string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,run.ID).Scan(&status);err!=nil{t.Fatal(err)}
 if status!=string(RunRunning){t.Fatalf("Worker prematurely marked interrupted: %q",status)}
 taskRow,err:=tasks.Get(ctx,created.ID)
 if err!=nil{t.Fatal(err)}
 if taskRow.State!=task.StateRunning{t.Fatalf("Task mutated despite recovery rollback: %s",taskRow.State)}
 var events,jobs int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT count(*) FROM events WHERE aggregate_id=? AND type='task.attempt_interrupted'`,created.ID).Scan(&events);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT count(*) FROM outbox_jobs WHERE job_type='task.recovery.required' AND workspace_id='ws'`).Scan(&jobs);err!=nil{t.Fatal(err)}
 if events!=0||jobs!=0{t.Fatalf("orphan recovery records events=%d jobs=%d",events,jobs)}
 // Once reconciliation restores the execution record, exactly one atomic
 // interruption is recorded and a repeat call does not duplicate the outbox.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE task_attempts SET status='running' WHERE id=?`,run.AttemptID);err!=nil{t.Fatal(err)}
 count,err:=svc.RecoverLostRuns(ctx)
 if err!=nil||count!=1{t.Fatalf("retry recovery count=%d err=%v",count,err)}
 count,err=svc.RecoverLostRuns(ctx)
 if err!=nil||count!=0{t.Fatalf("recovery must be idempotent: count=%d err=%v",count,err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT count(*) FROM events WHERE aggregate_id=? AND type='task.attempt_interrupted'`,created.ID).Scan(&events);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT count(*) FROM outbox_jobs WHERE job_type='task.recovery.required' AND workspace_id='ws'`).Scan(&jobs);err!=nil{t.Fatal(err)}
 if events!=1||jobs!=1{t.Fatalf("recovery records duplicated: events=%d jobs=%d",events,jobs)}
}
