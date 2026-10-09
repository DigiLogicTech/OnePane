package agentworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestNamedWorkspaceModelWaitSurvivesWorkerRestartWithoutDuplicateAttempt(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"model_wait.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"World Project",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 w,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"World",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 taskService:=task.NewService(db.SQL(),db,clock.Real{})
 svc:=New(db.SQL(),db,clock.Real{},"local",taskService,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 created,err:=taskService.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&w.ID,
  Objective:"Build world on local CPU when model available",
  Completion:json.RawMessage(`{"type":"operator_review"}`),
 })
 if err!=nil{t.Fatal(err)}
 ready,err:=taskService.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision,
 })
 if err!=nil{t.Fatal(err)}
 run,started:=svc.startRun(ctx,ready)
 if started.Error!=""{t.Fatal(started.Error)}
 waiting:=svc.waitForLocalModel(ctx,run,TickResult{TaskID:created.ID,RunID:run.ID},
  "awaiting eligible local model")
 if waiting.Status!="waiting_model"||waiting.Error==""{
  t.Fatalf("no durable model wait: %+v",waiting)
 }
 suspended,err:=taskService.Get(ctx,created.ID)
 if err!=nil{t.Fatal(err)}
 if suspended.State!=task.StateWaitingDependency{
  t.Fatalf("Task should wait, not fail: %s",suspended.State)
 }
 workerState,err:=svc.getRun(ctx,run.ID)
 if err!=nil{t.Fatal(err)}
 if workerState.Status!=RunWaiting||decodeModelWait(workerState.Continuation)==nil{
  t.Fatalf("wait continuation not durable: %+v",workerState)
 }
 // A new service instance simulates an ordinary worker/service restart.
 restarted:=New(db.SQL(),db,clock.Real{},"local",taskService,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=restarted.syncResumedRuns(ctx);err!=nil{t.Fatal(err)}
 stillWaiting,err:=taskService.Get(ctx,created.ID)
 if err!=nil{t.Fatal(err)}
 if stillWaiting.State!=task.StateWaitingDependency{
  t.Fatalf("worker restarted Task before retry window: %s",stillWaiting.State)
 }
 // Advance the stored retry time without sleeping or mutating user Task data.
 persisted:=modelWaitEnvelope{ModelWait:&modelWaitRecord{
  Attempt:1,RetryAtMS:now-1000,Reason:"waiting",
 }}
 raw,err:=json.Marshal(persisted)
 if err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  `UPDATE agent_worker_runs SET continuation_json=? WHERE id=?`,string(raw),run.ID);err!=nil{
  t.Fatal(err)
 }
 if err:=restarted.syncResumedRuns(ctx);err!=nil{t.Fatal(err)}
 resumed,err:=taskService.Get(ctx,created.ID)
 if err!=nil{t.Fatal(err)}
 if resumed.State!=task.StateRunning{
  t.Fatalf("due local-model wait not resumed: %s",resumed.State)
 }
 resumedRun,err:=restarted.getRun(ctx,run.ID)
 if err!=nil||resumedRun.Status!=RunRunning{
  t.Fatalf("existing worker run not resumed: %+v %v",resumedRun,err)
 }
 var attempts int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`,created.ID).Scan(&attempts);err!=nil{
  t.Fatal(err)
 }
 if attempts!=1{t.Fatalf("retry created duplicate Task Attempt: %d",attempts)}
 var snapshots int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_runs WHERE task_id=? AND status='running'`,created.ID).Scan(&snapshots);err!=nil&&err!=sql.ErrNoRows{
  t.Fatal(err)
 }
 if snapshots!=1{t.Fatalf("worker retry duplicated active Run: %d",snapshots)}
}
