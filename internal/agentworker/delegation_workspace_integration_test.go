//go:build integration

package agentworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/agentprotocol"
 "github.com/DigiLogicTech/OnePane/internal/authority"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestDelegationPreservesCanonicalProjectWorkspaceAndLocalOnlyRouting(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"delegation.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 seed:=[]string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 }
 for _,q:=range seed {if _,err=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{WorkspaceID:"tenant",Name:"Game Project",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 world,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:project.ID,Name:"World",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 story,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:project.ID,Name:"Story",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"node-local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 parent,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",ProjectID:&project.ID,ProjectWorkspaceID:&world.ID,
  Objective:"Create game world",ActorPrincipalID:ptr("operator"),
  Completion:json.RawMessage(`{"type":"operator_review","onepane_routing":{"enabled":true,
   "project_workspace_id":"`+world.ID+`","workspace_access":{"mode":"brokered","project_workspace_id":"`+world.ID+`",
    "remote_models":false,"filesystem":"workspace-only","internet":false,"lan":false,"browser":false,"computer":false,"secrets":"none"}}}`),
 })
 if err!=nil{t.Fatal(err)}
 ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{TaskID:parent.ID,ExpectedRevision:parent.Revision})
 if err!=nil{t.Fatal(err)}
 run,res:=worker.startRun(ctx,ready)
 if res.Error!="" {t.Fatal(res.Error)}
 // A delegated model can request arbitrary completion text, but must not
 // select Story, enable cloud inference, or receive direct host/Vault tools.
 proposal:=map[string]any{
  "objective":"Build world mesh in separate agent",
  "completion":map[string]any{"type":"operator_review","onepane_routing":map[string]any{
   "project_workspace_id":story.ID,
   "workspace_access":map[string]any{"mode":"direct","project_workspace_id":story.ID,
    "remote_models":true,"internet":true,"filesystem":"host","secrets":"all"},
  }},
 }
 raw,err:=json.Marshal(proposal)
 if err!=nil{t.Fatal(err)}
 reply:=agentprotocol.Response{ProposalType:agentprotocol.ProposalDelegate,Proposal:raw}
 result:=worker.handleDelegate(ctx,run,ready,reply,TickResult{TaskID:ready.ID,RunID:run.ID})
 if result.Error!=""||result.Status!="waiting_dependency" {
  t.Fatalf("delegation failed: %+v",result)
 }
 var childID string
 err=db.SQL().QueryRowContext(ctx,`SELECT depends_on_task_id FROM task_dependencies WHERE task_id=? AND dependency_type='hard'`,parent.ID).Scan(&childID)
 if err!=nil{t.Fatal(err)}
 child,err:=tasks.Get(ctx,childID)
 if err!=nil{t.Fatal(err)}
 if child.WorkspaceID!="tenant"||child.ProjectID==nil||*child.ProjectID!=project.ID||
  child.ProjectWorkspaceID==nil||*child.ProjectWorkspaceID!=world.ID||
  child.ParentTaskID==nil||*child.ParentTaskID!=parent.ID {
  t.Fatalf("delegated child escaped canonical Workspace: %+v",child)
 }
 if got:=routingPolicyFromCompletion(child.Completion);got.ProjectWorkspaceID!=world.ID ||
  got.WorkspaceAccess.ProjectWorkspaceID!=world.ID ||got.WorkspaceAccess.RemoteModels==nil||
  *got.WorkspaceAccess.RemoteModels||got.WorkspaceAccess.Mode!="brokered"||
  got.WorkspaceAccess.Internet||got.WorkspaceAccess.Secrets!="none" {
  t.Fatalf("delegated model widened parent policy: %+v",got)
 }
 if effectiveRemoteModelAllowance(child,routingPolicyFromCompletion(child.Completion),true) {
  t.Fatal("delegation re-enabled cloud inference without operator opt-in")
 }
 if err:=workspaceToolAllowedForTask(child,"network.external",authority.ActionExternalSend,"web.fetch","https://example.org");err==nil{
  t.Fatal("delegated World child received external network permission")
 }
 worldRows,err:=tasks.ListProjectWorkspace(ctx,"tenant",project.ID,world.ID,30)
 if err!=nil{t.Fatal(err)}
 found:=false
 for _,item:=range worldRows{if item.ID==child.ID{found=true}}
 if !found{t.Fatalf("delegated child missing from its canonical World Task queue: %s",child.ID)}
 storyRows,err:=tasks.ListProjectWorkspace(ctx,"tenant",project.ID,story.ID,30)
 if err!=nil{t.Fatal(err)}
 for _,item:=range storyRows{if item.ID==child.ID{t.Fatal("World child leaked into Story Task inventory")}}
 var dbWorkspace sql.NullString
 if err:=db.SQL().QueryRowContext(ctx,`SELECT project_workspace_id FROM tasks WHERE id=?`,child.ID).Scan(&dbWorkspace);err!=nil{t.Fatal(err)}
 if !dbWorkspace.Valid||dbWorkspace.String!=world.ID{t.Fatalf("canonical owner lost in SQLite: %+v",dbWorkspace)}
}
