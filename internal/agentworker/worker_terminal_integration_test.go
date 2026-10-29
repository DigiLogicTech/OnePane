//go:build integration

package agentworker

import (
	"context"
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestWorkerTerminalStatesCommitExactlyWithOriginalAttempt(t *testing.T) {
	for _, kind := range []string{"blocked", "failed"} {
		t.Run(kind,func(t *testing.T) {
			worker,ctx,run,cleanup:=prepareUnknownDispatch(t)
			defer cleanup()
			result:=TickResult{TaskID:run.TaskID,RunID:run.ID}
			var targetTask, targetAttempt, targetWorker, auditType string
			if kind=="blocked"{
				result=worker.blockRun(ctx,run,result,"review required")
				targetTask,targetAttempt,targetWorker,auditType="blocked","waiting","blocked","task.blocked"
			}else{
				result=worker.failRun(ctx,run,result,errors.New("step limit reached"))
				targetTask,targetAttempt,targetWorker,auditType="failed","failed","failed","task.failed"
			}
			if result.Status!=targetWorker{t.Fatalf("terminal result: %+v",result)}
			var taskStatus,attemptStatus,runStatus string
			var eventCount int
			if err:=worker.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,run.TaskID).Scan(&taskStatus);err!=nil{t.Fatal(err)}
			if err:=worker.db.QueryRowContext(ctx,`SELECT status FROM task_attempts WHERE id=?`,run.AttemptID).Scan(&attemptStatus);err!=nil{t.Fatal(err)}
			if err:=worker.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,run.ID).Scan(&runStatus);err!=nil{t.Fatal(err)}
			if err:=worker.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type=?`,run.TaskID,auditType).Scan(&eventCount);err!=nil{t.Fatal(err)}
			if taskStatus!=targetTask||attemptStatus!=targetAttempt||runStatus!=targetWorker||eventCount!=1{
				t.Fatalf("inconsistent terminal state task=%s attempt=%s run=%s audits=%d",
					taskStatus,attemptStatus,runStatus,eventCount)
			}
			if worker.dispatchReconciliationFailed{t.Fatal("successful terminal state poisoned scheduling")}
		})
	}
}

func TestWorkerTerminalCancelledCommitHaltsUntilRecovery(t *testing.T) {
	worker,ctx,run,cleanup:=prepareUnknownDispatch(t)
	defer cleanup()
	cancelled,cancel:=context.WithCancel(ctx)
	cancel()
	r:=worker.blockRun(cancelled,run,TickResult{TaskID:run.TaskID,RunID:run.ID},"policy revoked")
	if r.Status!="failed"||!worker.dispatchReconciliationFailed{t.Fatalf("uncommitted block was reported as committed: %+v",r)}
	var taskStatus,workerStatus string
	if err:=worker.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,run.TaskID).Scan(&taskStatus);err!=nil{t.Fatal(err)}
	if err:=worker.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,run.ID).Scan(&workerStatus);err!=nil{t.Fatal(err)}
	if taskStatus!="running"||workerStatus!="running"{t.Fatalf("failed commit mutated records: %s/%s",taskStatus,workerStatus)}
	if _,err:=worker.Tick(ctx,1);err!=nil{t.Fatalf("in-process park failed: %v",err)}
	n,err:=worker.RecoverLostRuns(ctx)
	if err!=nil||n!=0||worker.dispatchReconciliationFailed{t.Fatalf("live park was not durable: n=%d err=%v halted=%v",n,err,worker.dispatchReconciliationFailed)}
	if err:=worker.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,run.TaskID).Scan(&taskStatus);err!=nil{t.Fatal(err)}
	if err:=worker.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,run.ID).Scan(&workerStatus);err!=nil{t.Fatal(err)}
	if taskStatus!="blocked"||workerStatus!="interrupted"{t.Fatalf("park replayed or dropped the task: %s/%s",taskStatus,workerStatus)}
}

func TestWorkerTerminalRejectsStaleAttemptWithoutPartialMutation(t *testing.T) {
	worker,ctx,run,cleanup:=prepareUnknownDispatch(t)
	defer cleanup()
	if _,err:=worker.db.ExecContext(ctx,`UPDATE task_attempts SET status='succeeded' WHERE id=?`,run.AttemptID);err!=nil{t.Fatal(err)}
	r:=worker.failRun(ctx,run,TickResult{TaskID:run.TaskID,RunID:run.ID},errors.New("invalid response"))
	if r.Status!="failed"||!worker.dispatchReconciliationFailed{
		t.Fatalf("terminal transition of stale Attempt was accepted: %+v",r)
	}
	var taskStatus,workerStatus string
	if err:=worker.db.QueryRowContext(ctx,`SELECT state FROM tasks WHERE id=?`,run.TaskID).Scan(&taskStatus);err!=nil{t.Fatal(err)}
	if err:=worker.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,run.ID).Scan(&workerStatus);err!=nil{t.Fatal(err)}
	if taskStatus!="running"||workerStatus!="running"{t.Fatalf("partial state committed: %s/%s",taskStatus,workerStatus)}
}

func TestWorkerTerminalRefusesInvalidTaskTransition(t *testing.T){
	worker,ctx,run,cleanup:=prepareUnknownDispatch(t)
	defer cleanup()
	if _,err:=worker.db.ExecContext(ctx,`UPDATE tasks SET state=? WHERE id=?`,task.StateCompletionRequested,run.TaskID);err!=nil{t.Fatal(err)}
	r:=worker.failRun(ctx,run,TickResult{TaskID:run.TaskID,RunID:run.ID},errors.New("verification failed"))
	if r.Status!="failed"||!worker.dispatchReconciliationFailed{t.Fatalf("invalid completion transition was hidden: %+v",r)}
	var status string
	if err:=worker.db.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,run.ID).Scan(&status);err!=nil{t.Fatal(err)}
	if status!="running"{t.Fatalf("Worker completed despite invalid Task state: %s",status)}
}
