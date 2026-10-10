package projectorchestrator

import (
 "context"
 "database/sql"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
 _ "modernc.org/sqlite"
)

func TestTaskOperationHealthPreservesUnknownExternalOutcome(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 if _,err=db.ExecContext(ctx,`CREATE TABLE operations(
  id TEXT,workspace_id TEXT,task_id TEXT,state TEXT)`);err!=nil{t.Fatal(err)}
 check:=func(state task.State)(string,bool){
  t.Helper()
  tx,e:=db.BeginTx(ctx,&sql.TxOptions{ReadOnly:true})
  if e!=nil{t.Fatal(e)}
  defer tx.Rollback()
  health,e:=taskExternalEffectHealthTx(ctx,tx,"task-a","tenant",state)
  if e!=nil{t.Fatal(e)}
  return health,needsExternalEffectReview(health)
 }
 if h,review:=check(task.StateRunning);h!="none"||review{
  t.Fatalf("empty Task has false external mutation: %s %t",h,review)
 }
 add:=func(id,ws,taskID,state string){
  t.Helper()
  if _,e:=db.ExecContext(ctx,`INSERT INTO operations(id,workspace_id,task_id,state)
   VALUES(?,?,?,?)`,id,ws,taskID,state);e!=nil{t.Fatal(e)}
 }
 add("other-workspace","other","task-a","unknown_outcome")
 add("other-task","tenant","task-b","blocked_unknown_outcome")
 if h,review:=check(task.StateRunning);h!="none"||review{
  t.Fatalf("foreign operation influenced authorized Task: %s %t",h,review)
 }
 add("running","tenant","task-a","executing")
 if h,review:=check(task.StateRunning);h!="external_effect_in_progress"||review{
  t.Fatalf("active observed tool was mistaken for crashed outcome: %s %t",h,review)
 }
 if h,review:=check(task.StatePaused);h!="inflight_effect_outlived_task"||!review{
  t.Fatalf("orphaned tool mutation appeared safe: %s %t",h,review)
 }
 add("unknown","tenant","task-a","blocked_unknown_outcome")
 if h,review:=check(task.StateRunning);h!="unresolved_unknown_outcome"||!review{
  t.Fatalf("unknown external side effect escaped review: %s %t",h,review)
 }
 if _,err=db.ExecContext(ctx,`DELETE FROM operations WHERE id='unknown'`);err!=nil{t.Fatal(err)}
 add("failed-compensate","tenant","task-a","compensation_failed")
 if h,review:=check(task.StateRunning);h!="failed_compensation"||!review{
  t.Fatalf("failed compensation appeared safe: %s %t",h,review)
 }
}

func TestUnknownToolMutationChangesDAGRecoveryCheckpointWithoutExecution(t *testing.T){
 graph:=TaskGraph{ID:"graph",ProjectID:"project",ManifestSHA256:"immutable",
  Nodes:[]TaskGraphNode{{Key:"build",TaskID:"task-a",
   State:task.StateRunning,ExternalEffectHealth:"external_effect_in_progress"}}}
 graph.Progress,graph.Status=evaluateTaskGraph(&graph)
 first:=recoveryCheckpointForGraph(graph)
 if first.Nodes[0].RecoveryClass!="observe_existing_attempt"||
  first.NeedsOperatorReview||first.AutomaticRetryAllowed{
  t.Fatalf("live execution was misclassified: %+v",first)
 }
 graph.Nodes[0].ExternalEffectHealth="unresolved_unknown_outcome"
 graph.Progress,graph.Status=evaluateTaskGraph(&graph)
 second:=recoveryCheckpointForGraph(graph)
 if second.Nodes[0].RecoveryClass!="operator_review"||
  !second.NeedsOperatorReview||second.AutomaticRetryAllowed||
  second.ObservationSHA256==first.ObservationSHA256{
  t.Fatalf("mutation outcome omitted from safe checkpoint: %+v",second)
 }
}
