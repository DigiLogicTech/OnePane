//go:build integration

package agentworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "strings"
 "sync"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/featurepolicy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/storage"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// Inject a policy revocation after the read-only preflight but before the
// durable transaction. The production admission path must re-read from tx.
type revokeBeforeWorkerAdmission struct{
 storage.Transactor
 db *sql.DB
 viewID string
 patch string
 once sync.Once
}
func (x *revokeBeforeWorkerAdmission) Within(ctx context.Context, fn func(context.Context,storage.Tx)error)error{
 var injectErr error
 x.once.Do(func(){
  _,injectErr=x.db.ExecContext(ctx,"UPDATE project_workspaces SET ai_settings_json=? WHERE id=?",x.patch,x.viewID)
 })
 if injectErr!=nil{return injectErr}
 return x.Transactor.Within(ctx,fn)
}

func configuredWorkerAdmission(t *testing.T)(context.Context,*sqlite.DB,*task.Service,*Service,task.Task,string){
 t.Helper()
 ctx,db,tasks,svc,ready:=setupAdmissionFixture(t)
 // This fixture explicitly represents the human who approved autonomy
 // before testing revocation during Worker admission. System principals
 // cannot mint new Project or Workspace execution privileges.
 for _,q:=range []string{
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES('policy-reviewer','human','Operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('ws','policy-reviewer','active',1,1)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"ws",Name:"Game project",CreatedBy:"policy-reviewer",
  ProjectPolicyJSON:json.RawMessage(`{"feature_policy":{"AutonomousRoutines":"enabled","AutomaticFallback":"enabled"}}`),
 })
 if err!=nil{t.Fatal(err)}
 view,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"World",ActorPrincipalID:"policy-reviewer",
  AISettingsJSON:json.RawMessage(`{"feature_policy":{"AutonomousRoutines":"enabled","AutomaticFallback":"enabled"}}`),
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,"UPDATE tasks SET project_id=?,project_workspace_id=? WHERE id=?",p.ID,view.ID,ready.ID);err!=nil{t.Fatal(err)}
 ready.ProjectID=&p.ID
 ready.ProjectWorkspaceID=&view.ID
 grants:=featurepolicy.StoredScopes{DB:db.SQL()}
 for _,action:=range []string{"tasks.autonomous","inference.fallback"}{
  allowed,err:=grants.AuthorizeTask(ctx,"ws",ready.ID,action)
  if err!=nil||!allowed{t.Fatalf("setup did not grant %s: allowed=%v err=%v",action,allowed,err)}
 }
 return ctx,db,tasks,svc,ready,view.ID
}

func TestWorkerAdmissionRechecksAutonomyInSameTransaction(t *testing.T){
 ctx,db,tasks,svc,ready,viewID:=configuredWorkerAdmission(t)
 svc.tx=&revokeBeforeWorkerAdmission{
  Transactor:db,db:db.SQL(),viewID:viewID,
  patch:`{"feature_policy":{"AutonomousRoutines":"disabled"}}`,
 }
 run,res:=svc.startRun(ctx,ready)
 if run.ID!=""||res.Error==""||!strings.Contains(res.Error,"revoked before admission"){
  t.Fatalf("revoked policy permitted an admitted Worker: run=%+v result=%+v",run,res)
 }
 assertAdmissionUnchanged(t,ctx,db,tasks,ready)
}

func TestWorkerAdmissionRejectsChangedFallbackAtCommitBoundary(t *testing.T){
 ctx,db,tasks,svc,ready,viewID:=configuredWorkerAdmission(t)
 svc.tx=&revokeBeforeWorkerAdmission{
  Transactor:db,db:db.SQL(),viewID:viewID,
  patch:`{"feature_policy":{"AutonomousRoutines":"enabled","AutomaticFallback":"disabled"}}`,
 }
 run,res:=svc.startRun(ctx,ready)
 if run.ID!=""||res.Error==""||!strings.Contains(res.Error,"routing policy changed"){
  t.Fatalf("fallback revocation permitted stale routing: run=%+v result=%+v",run,res)
 }
 assertAdmissionUnchanged(t,ctx,db,tasks,ready)
}

func TestWorkerResumeRejectsRevokedAutonomyAtomically(t *testing.T){
 ctx,db,tasks,svc,ready,viewID:=configuredWorkerAdmission(t)
 run,result:=svc.startRun(ctx,ready)
 if result.Error!=""||run.ID==""{t.Fatalf("start fixture: %+v %+v",run,result)}
 if err:=svc.suspendForResource(ctx,run,json.RawMessage(`{}`),
  "temporary local resource wait","route",map[string]any{"kind":"local"},nil);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces
  SET ai_settings_json='{"feature_policy":{"AutonomousRoutines":"disabled"}}'
  WHERE id=?`,viewID);err!=nil{t.Fatal(err)}
 waiting,err:=tasks.Get(ctx,ready.ID)
 if err!=nil{t.Fatal(err)}
 if waiting.State!=task.StateWaitingDependency{t.Fatalf("not waiting: %s",waiting.State)}
 var continuation,status string
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT continuation_json,status FROM agent_worker_runs WHERE id=?",run.ID).
  Scan(&continuation,&status);err!=nil{t.Fatal(err)}
 if status!=string(RunWaiting){t.Fatalf("unexpected Worker status: %s",status)}
 err=svc.resumeWaitingWorkerAndAttempt(ctx,run.ID,ready.ID,waiting.Revision,continuation)
 if err==nil||!strings.Contains(err.Error(),"revoked before resume"){
  t.Fatalf("revoked Workspace resumed Worker: %v",err)
 }
 after,err:=tasks.Get(ctx,ready.ID)
 if err!=nil{t.Fatal(err)}
 if after.State!=task.StateWaitingDependency||after.Revision!=waiting.Revision{
  t.Fatalf("denied resume changed Task: %+v",after)
 }
 var workerStatus,attemptStatus string
 if err:=db.SQL().QueryRowContext(ctx,"SELECT status FROM agent_worker_runs WHERE id=?",run.ID).Scan(&workerStatus);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,"SELECT status FROM task_attempts WHERE id=?",run.AttemptID).Scan(&attemptStatus);err!=nil{t.Fatal(err)}
 if workerStatus!=string(RunWaiting)||attemptStatus!=string(task.AttemptWaiting){
  t.Fatalf("denied resume changed Worker or Attempt: %s %s",workerStatus,attemptStatus)
 }
}
