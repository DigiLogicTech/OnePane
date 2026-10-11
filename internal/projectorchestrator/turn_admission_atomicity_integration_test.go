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

// If admission cannot record the operator's intent or Orchestrator status,
// do not infer, create a Task, or silently leave half an audit transaction.
func TestOrchestratorTurnAdmissionIsAtomicBeforeTaskCreation(t *testing.T){
 for _,tc:=range []struct{name,trigger string}{
  {"user_turn_insert_rejected",`CREATE TRIGGER reject_user_orchestrator_turn BEFORE INSERT ON project_orchestrator_turns
    WHEN NEW.role='user' BEGIN SELECT RAISE(ABORT,'deliberate audit insert failure'); END`},
  {"working_status_update_rejected",`CREATE TRIGGER reject_orchestrator_working BEFORE UPDATE ON project_orchestrators
    WHEN NEW.status='working' BEGIN SELECT RAISE(ABORT,'deliberate status write failure'); END`},
 } {
  t.Run(tc.name,func(t *testing.T){
   ctx:=context.Background()
   db,err:=sqlitestore.Open(t.TempDir()+"/orchestrator-turn-admission.db")
   if err!=nil{t.Fatal(err)}
   defer db.Close()
   if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
   now:=clock.Real{}.UnixMilli()
   for _,stmt:=range []string{
    `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
       VALUES('tenant','Tenant','active',1,?,?)`,
    `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
       VALUES('operator','human','Operator','active',1,?,?)`,
    `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
       VALUES('tenant','operator','active',?,?)`,
   }{
    if _,err:=db.SQL().ExecContext(ctx,stmt,now,now);err!=nil{t.Fatal(err)}
   }
   projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
   p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
    WorkspaceID:"tenant",Name:"Auditable development",CreatedBy:"operator",
   })
   if err!=nil{t.Fatal(err)}
   ws,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
    ProjectID:p.ID,Name:"World",ActorPrincipalID:"operator",
   })
   if err!=nil{t.Fatal(err)}
   orchestrator:=NewService(db.SQL(),db,clock.Real{},unavailableOrchestratorReasoning{},
    nil,nil,task.NewService(db.SQL(),db,clock.Real{}),nil)
   if _,err:=orchestrator.Ensure(ctx,p.ID);err!=nil{t.Fatal(err)}
   if _,err:=db.SQL().ExecContext(ctx,tc.trigger);err!=nil{t.Fatal(err)}
   _,err=orchestrator.Turn(ctx,TurnCommand{
    ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"operator",
    Objective:"Create an immutable world map",ForceTask:true,AllowTaskCreation:true,
   })
   if err==nil||!strings.Contains(err.Error(),"persist Project Orchestrator user turn"){
    t.Fatalf("failed durable audit unexpectedly accepted autonomous Task: %v",err)
   }
   var taskCount,turnCount int
   if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM tasks WHERE project_id=?`,p.ID).Scan(&taskCount);err!=nil{t.Fatal(err)}
   if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM project_orchestrator_turns`).Scan(&turnCount);err!=nil{t.Fatal(err)}
   if taskCount!=0||turnCount!=0{
    t.Fatalf("Task/orphan turn was created after audit admission failed: tasks=%d turns=%d",taskCount,turnCount)
   }
   got,err:=orchestrator.Get(ctx,p.ID)
   if err!=nil{t.Fatal(err)}
   if got.Status!="ready"{t.Fatalf("failed admission left stale Orchestrator state %q",got.Status)}
  })
 }
}
