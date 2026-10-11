//go:build integration

package inference

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

type revokingScopeCoordinator struct{
 db *sqliteStore.DB
 workspaceID string
}
type noopScopeLease struct{}
func (noopScopeLease)ReportBoundary(context.Context,string)error{return nil}
func (noopScopeLease)Release(context.Context,error){}
func (c revokingScopeCoordinator)Acquire(ctx context.Context,_ RuntimeScheduleRequest)(RuntimeScheduleLease,error){
 _,err:=c.db.SQL().ExecContext(ctx,`UPDATE project_workspaces
 SET ai_settings_json='{"feature_policy":{"ModelRouting":"disabled"}}'
 WHERE id=?`,c.workspaceID)
 if err!=nil{return nil,err}
 return noopScopeLease{},nil
}

func TestInferenceEnforcesStoredCanonicalWorkspaceAndLiveRevocation(t *testing.T){
 ctx:=context.Background()
 root:=t.TempDir()
 db,err:=sqliteStore.Open(root+"/scope-inference.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=time.Now().UTC().UnixMilli()
 for _,seed:=range []struct{stmt string;args []any}{
  {`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
     VALUES('tenant','Tenant','active',1,?,?)`,[]any{now,now}},
  {`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
     VALUES('agent','agent','Agent','active',1,?,?)`,[]any{now,now}},
  {`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
     VALUES('tenant','agent','active',?,?)`,[]any{now,now}},
  {`INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,
     protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
     VALUES('node','local',1,'fp','local','LOCAL_TRUSTED','{}','{}',?,1,?,?)`,[]any{now,now,now}},
 }{
  if _,err:=db.SQL().ExecContext(ctx,seed.stmt,seed.args...);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Game",CreatedBy:"agent",
  ProjectPolicyJSON:json.RawMessage(`{"feature_policy":{"ProjectAI":"enabled","ModelRouting":"enabled"}}`),
 })
 if err!=nil{t.Fatal(err)}
 view,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"World",ActorPrincipalID:"agent",
  AISettingsJSON:json.RawMessage(`{"feature_policy":{"ModelRouting":"disabled"}}`),
 })
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 taskRow,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&project.ID,ProjectWorkspaceID:&view.ID,
  Objective:"local scoped inference",
 })
 if err!=nil{t.Fatal(err)}
 store,err:=artifact.NewLocalStore(root+"/artifacts")
 if err!=nil{t.Fatal(err)}
 artifacts:=artifact.NewService(db.SQL(),db,store,clock.Real{})
 transports:=NewTransportRegistry()
 if err:=RegisterBuiltinTransports(transports);err!=nil{t.Fatal(err)}
 infer:=NewService(db.SQL(),db,clock.Real{})
 if err:=infer.ConfigureExecution(artifacts,transports,integrationNoSecretResolver{},"node");err!=nil{t.Fatal(err)}
 model,err:=infer.RegisterModel(ctx,RegisterModelCommand{ModelRef:"builtin/fake",TrustState:ModelUserTrusted})
 if err!=nil{t.Fatal(err)}
 node:="node"
 runtime:="fake"
 dep,err:=infer.RegisterDeployment(ctx,RegisterDeploymentCommand{ModelID:model.ID,NodeID:&node,RuntimeName:&runtime})
 if err!=nil{t.Fatal(err)}
 dep,err=infer.SetDeploymentStatus(ctx,SetDeploymentStatusCommand{DeploymentID:dep.ID,ExpectedRevision:dep.Revision,Status:DeploymentQualifying})
 if err!=nil{t.Fatal(err)}
 dep,err=infer.SetDeploymentStatus(ctx,SetDeploymentStatusCommand{DeploymentID:dep.ID,ExpectedRevision:dep.Revision,Status:DeploymentReady,ResidencyState:residencyptr(ResidencyResident)})
 if err!=nil{t.Fatal(err)}
 label:=policy.DataLabel{WorkspaceID:"tenant",Confidentiality:policy.ConfidentialityInternal,
  Residency:policy.ResidencyOriginNode,Trust:policy.TrustUserInstruction,OriginNodeID:"node"}
 cmd:=ExecuteCommand{WorkspaceID:"tenant",PrincipalID:"agent",TaskID:&taskRow.ID,DeploymentID:dep.ID,
  InputLabel:label,RequestJSON:json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`)}
 denied,err:=infer.Execute(ctx,cmd)
 if !errors.Is(err,ErrInformationFlow)||denied.Status!=RequestFailed||denied.ResponseArtifactID!=nil{
  t.Fatalf("disabled Workspace routed model: status=%s err=%v",denied.Status,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces
 SET ai_settings_json='{"feature_policy":{"ModelRouting":"enabled"}}'
 WHERE id=?`,view.ID);err!=nil{t.Fatal(err)}
 allowed,err:=infer.Execute(ctx,cmd)
 if err!=nil||allowed.Status!=RequestSucceeded||allowed.ResponseArtifactID==nil{
  t.Fatalf("explicitly enabled Workspace route rejected: status=%s err=%v",allowed.Status,err)
 }
 if err:=infer.ConfigureRuntimeCoordinator(revokingScopeCoordinator{db:db,workspaceID:view.ID});err!=nil{t.Fatal(err)}
 revoked,err:=infer.Execute(ctx,cmd)
 if err==nil||revoked.Status!=RequestFailed||revoked.ErrorCode==nil||*revoked.ErrorCode!="feature_policy_revoked"||revoked.ResponseArtifactID!=nil{
  t.Fatalf("revoked Workspace reached transport: status=%s code=%v err=%v",revoked.Status,revoked.ErrorCode,err)
 }
}
