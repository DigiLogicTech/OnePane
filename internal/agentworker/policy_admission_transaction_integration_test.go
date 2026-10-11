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
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"ws",Name:"Game project",CreatedBy:AuthorityPrincipal,
  ProjectPolicyJSON:json.RawMessage(`{"feature_policy":{"AutonomousRoutines":"enabled","AutomaticFallback":"enabled"}}`),
 })
 if err!=nil{t.Fatal(err)}
 view,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"World",ActorPrincipalID:AuthorityPrincipal,
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
