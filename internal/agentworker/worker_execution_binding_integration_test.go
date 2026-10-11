//go:build integration

package agentworker

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// A Worker row saying RUNNING does not confer a Task/Attempt execution lease.
// Corrupted or externally changed Attempt ownership must be noticed BEFORE
// calling scheduler, inference or the tool gateway, and at the final CAS.
func TestAgentWorkerDispatchRejectsStaleOrForeignActiveAttempt(t *testing.T) {
	ctx:=context.Background()
	db,err:=sqlite.Open(filepath.Join(t.TempDir(),"worker-dispatch.db"))
	if err!=nil{t.Fatal(err)}
	defer db.Close()
	if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
	now:=clock.Real{}.UnixMilli()
	if _,err:=db.SQL().ExecContext(ctx,
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
		VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
	clk:=clock.Real{}
	tasks:=task.NewService(db.SQL(),db,clk)
	// No scheduler/inference/tool dependencies: every corrupted case must
	// fail before any of these components can be invoked.
	worker:=New(db.SQL(),db,clk,"node-local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
	if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
	start:=func(objective string)Run{
		t.Helper()
		created,err:=tasks.Create(ctx,task.CreateCommand{
			WorkspaceID:"tenant",Objective:objective,
		})
		if err!=nil{t.Fatal(err)}
		ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
			TaskID:created.ID,ExpectedRevision:created.Revision,
		})
		if err!=nil{t.Fatal(err)}
		run,result:=worker.startRun(ctx,ready)
		if result.Error!=""||run.Status!=RunRunning{
			t.Fatalf("could not start %s: %+v",objective,result)
		}
		return run
	}
	victim:=start("Binding must remain immutable")
	other:=start("Independent legitimate Task")
	if err:=worker.requireActiveWorkerExecutionBinding(ctx,victim);err!=nil{t.Fatalf("healthy binding denied: %v",err)}
	deny:=func(label string){
		t.Helper()
		t.Run(label,func(t *testing.T){
			before,err:=worker.getRun(ctx,victim.ID)
			if err!=nil{t.Fatal(err)}
			if result:=worker.step(ctx,victim);result.Status!="failed"||
				!strings.Contains(result.Error,"operator recovery review"){
				t.Fatalf("drift reached model routing or failed open: %+v",result)
			}
			actual,err:=worker.getRun(ctx,victim.ID)
			if err!=nil{t.Fatal(err)}
			if actual.StepCount!=before.StepCount||actual.Status!=RunRunning{
				t.Fatalf("failed preflight wrote Worker state: before=%+v after=%+v",before,actual)
			}
			if err:=worker.markReasoningDispatch(ctx,&before,"local","candidate");!errors.Is(err,ErrInvalidWorkerState){
				t.Fatalf("dispatch CAS admitted invalid Attempt: %v",err)
			}
			after,err:=worker.getRun(ctx,victim.ID)
			if err!=nil{t.Fatal(err)}
			if after.StepCount!=before.StepCount||after.Revision!=before.Revision{
				t.Fatalf("denied dispatch consumed reasoning budget or revision: %+v",after)
			}
		})
	}
	if _,err:=db.SQL().ExecContext(ctx,
		`UPDATE task_attempts SET status='interrupted' WHERE id=?`,victim.AttemptID);err!=nil{t.Fatal(err)}
	deny("interrupted_original_attempt")
	if _,err:=db.SQL().ExecContext(ctx,
		`UPDATE task_attempts SET status='running',worker_principal_id=?
		 WHERE id=?`,AuthorityPrincipal,victim.AttemptID);err!=nil{t.Fatal(err)}
	deny("different_worker_principal")
	if _,err:=db.SQL().ExecContext(ctx,
		`UPDATE task_attempts SET worker_principal_id=?,metadata_json='{}'
		 WHERE id=?`,WorkerPrincipal,victim.AttemptID);err!=nil{t.Fatal(err)}
	deny("missing_agent_worker_provenance")
	if _,err:=db.SQL().ExecContext(ctx,
		`UPDATE task_attempts SET metadata_json='{"agent_worker":"v1"}'
		 WHERE id=?`,victim.AttemptID);err!=nil{t.Fatal(err)}
	if _,err:=db.SQL().ExecContext(ctx,
		`UPDATE agent_worker_runs SET task_id=? WHERE id=?`,
		other.TaskID,victim.ID);err!=nil{t.Fatal(err)}
	deny("attempt_from_another_task")
	if _,err:=db.SQL().ExecContext(ctx,
		`UPDATE agent_worker_runs SET task_id=? WHERE id=?`,
		victim.TaskID,victim.ID);err!=nil{t.Fatal(err)}
	if _,err:=db.SQL().ExecContext(ctx,
		`UPDATE workspaces SET status='suspended' WHERE id='tenant'`);err!=nil{t.Fatal(err)}
	deny("tenant_disabled_after_task_start")
	if _,err:=db.SQL().ExecContext(ctx,
		`UPDATE workspaces SET status='active' WHERE id='tenant'`);err!=nil{t.Fatal(err)}
	if err:=worker.requireActiveWorkerExecutionBinding(ctx,victim);err!=nil{
		t.Fatalf("restored matching Worker/Task/Attempt rejected: %v",err)
	}
	good,err:=worker.getRun(ctx,victim.ID)
	if err!=nil{t.Fatal(err)}
	if err:=worker.markReasoningDispatch(ctx,&good,"local","approved");err!=nil{
		t.Fatalf("legitimate first dispatch denied: %v",err)
	}
	if good.StepCount!=1{t.Fatalf("healthy binding did not record exactly one dispatch: %+v",good)}
	otherGood,err:=worker.getRun(ctx,other.ID)
	if err!=nil{t.Fatal(err)}
	if err:=worker.markReasoningDispatch(ctx,&otherGood,"local","independent");err!=nil{
		t.Fatalf("unaffected sibling cannot dispatch: %v",err)
	}
	if otherGood.StepCount!=1{t.Fatalf("independent Worker unexpectedly counted steps: %+v",otherGood)}
}
