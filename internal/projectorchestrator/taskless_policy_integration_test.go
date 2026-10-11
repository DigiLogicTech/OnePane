//go:build integration

package projectorchestrator

import (
 "context"
 "encoding/json"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestDeniedTasklessProjectPolicyWritesNoTurnAndSpawnsNoTask(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/taskless-policy.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','owner','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,stmt);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Policy restricted Project",CreatedBy:"owner",
 })
 if err!=nil{t.Fatal(err)}
 world,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"World",ActorPrincipalID:"owner",
 })
 if err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,clock.Real{},nil,nil,nil,nil,nil)
 cases:=[]struct{name,policy string}{
  {"Project Assistant denied",`{"feature_policy":{"Assistant":"disabled"}}`},
  {"Project AI denied",`{"feature_policy":{"ProjectAI":"disabled"}}`},
  {"Model routing denied",`{"feature_policy":{"ModelRouting":"disabled"}}`},
  {"Malformed Project policy",`{"feature_policy":null}`},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   if _,err:=db.SQL().ExecContext(ctx,`UPDATE projects SET project_policy_json=?
     WHERE id=?`,tc.policy,project.ID);err!=nil{t.Fatal(err)}
   if _,err:=svc.Turn(ctx,TurnCommand{
    ProjectID:project.ID,ProjectWorkspaceID:world.ID,ActorPrincipalID:"owner",
    Objective:"Execute Project work",AllowTaskCreation:true,ForceTask:true,
   });err==nil{t.Fatal("denied Project inference scope accepted")}
  })
 }
 var turns,tasks int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_orchestrator_turns").Scan(&turns);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM tasks").Scan(&tasks);err!=nil{t.Fatal(err)}
 if turns!=0||tasks!=0{t.Fatalf("denied Project Turn mutated history or tasks: turns=%d tasks=%d",turns,tasks)}
 // Child Workspace restriction must hold even if the Project allows.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE projects SET project_policy_json='{}' WHERE id=?`,project.ID);err!=nil{t.Fatal(err)}
 settings:=json.RawMessage(`{"feature_policy":{"ProjectAI":"disabled"}}`)
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces SET ai_settings_json=? WHERE id=?`,string(settings),world.ID);err!=nil{t.Fatal(err)}
 if _,err:=svc.Turn(ctx,TurnCommand{
  ProjectID:project.ID,ProjectWorkspaceID:world.ID,ActorPrincipalID:"owner",
  Objective:"Run isolated Project action",AllowTaskCreation:true,ForceTask:true,
 });err==nil{t.Fatal("Workspace-specific Project AI denial ignored")}
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM project_orchestrator_turns").Scan(&turns);err!=nil||turns!=0{
  t.Fatalf("Workspace denial persisted a turn: %d %v",turns,err)
 }
}
