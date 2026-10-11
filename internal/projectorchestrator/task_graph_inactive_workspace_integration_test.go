//go:build integration

package projectorchestrator

import (
 "context"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestTaskGraphArchivedWorkspaceNeverEligibleForRecovery(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/inactive-graph-workspace.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 } {
  if _,err:=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Archive-safe development",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 world,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"World",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 story,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Story",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),db,clock.Real{},nil,nil,nil,
  task.NewService(db.SQL(),db,clock.Real{}),nil)
 graph,err:=svc.CreateTaskGraph(ctx,CreateTaskGraphCommand{
  ProjectID:p.ID,Name:"Isolated work",IdempotencyKey:"archived-workspace-fence",
  ActorPrincipalID:"operator",Nodes:[]TaskGraphNodeSpec{
   {Key:"world",ProjectWorkspaceID:world.ID,Objective:"Render world"},
   {Key:"story",ProjectWorkspaceID:story.ID,Objective:"Write story"},
  },
 })
 if err!=nil{t.Fatal(err)}
 if graph.Progress.Admissible!=2{t.Fatalf("before archive, both should be admissible: %+v",graph.Progress)}
 // Archive after graph approval without mutating the Task's canonical ID.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces
  SET status='archived' WHERE id=?`,world.ID);err!=nil{t.Fatal(err)}
 got,err:=svc.TaskGraph(ctx,p.ID,graph.ID)
 if err!=nil{t.Fatal(err)}
 status:=map[string]TaskGraphNode{}
 for _,n:=range got.Nodes{status[n.Key]=n}
 if !status["world"].ScopeDrift||status["world"].Readiness!="needs_attention"{
  t.Fatalf("archived World remained eligible: %+v",status["world"])
 }
 if status["story"].ScopeDrift||status["story"].Readiness!="admission_eligible"{
  t.Fatalf("unaffected Story branch was blocked: %+v",status["story"])
 }
 if got.Progress.Admissible!=1||got.Progress.NeedsAttention!=1||got.Status!="needs_attention"{
  t.Fatalf("archived scope not reflected in graph-level recovery: %+v",got)
 }
 checkpoint,err:=svc.TaskGraphRecoveryCheckpoint(ctx,p.ID,graph.ID)
 if err!=nil{t.Fatal(err)}
 if !checkpoint.NeedsOperatorReview||checkpoint.AutomaticRetryAllowed{
  t.Fatalf("archived Workspace wrongly recoverable automatically: %+v",checkpoint)
 }
 for _,n:=range checkpoint.Nodes{
  if n.Key=="world"&&(!strings.Contains(n.OperatorAction,"Workspace")||
   n.CanExistingWorkerAdmit||n.RecoveryClass!="operator_review"){
   t.Fatalf("archived Workspace offered worker admission: %+v",n)
  }
 }
 // Restore canonical identity; no graph rebuild or synthetic completion.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces
  SET status='active' WHERE id=?`,world.ID);err!=nil{t.Fatal(err)}
 restored,err:=svc.TaskGraph(ctx,p.ID,graph.ID)
 if err!=nil{t.Fatal(err)}
 if restored.Progress.Admissible!=2||restored.Progress.NeedsAttention!=0{
  t.Fatalf("restored Workspace did not recover existing Task eligibility: %+v",restored)
 }
}
