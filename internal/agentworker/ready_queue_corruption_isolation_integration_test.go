//go:build integration

package agentworker

import (
 "context"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestCorruptEarlierReadyTaskDoesNotStarveHealthyQueueOrTick(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"ready-corruption.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1) // Ready scanning must close its cursor before Task loads.
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
 projectService:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projectService.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Corruption isolation",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 pw,err:=projectService.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Queue",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 create:=func(objective string,priority int)task.Task{
  t.Helper()
  created,e:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",ProjectID:&p.ID,
   ProjectWorkspaceID:&pw.ID,Objective:objective,Priority:priority,
  })
  if e!=nil{t.Fatal(e)}
  ready,e:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision,
  })
  if e!=nil{t.Fatal(e)}
  return ready
 }
 broken:=create("High priority malformed legacy payload",100)
 good:=create("Lower priority valid Task",90)
 // Reproduce a damaged pre-upgrade SQLite row without changing the
 // production schema. The TASK reader must reject malformed JSON.
 conn,err:=db.SQL().Conn(ctx)
 if err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`PRAGMA ignore_check_constraints=ON`);err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`UPDATE tasks
  SET completion_json='broken-json' WHERE id=?`,broken.ID);err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`PRAGMA ignore_check_constraints=OFF`);err!=nil{t.Fatal(err)}
 if err=conn.Close();err!=nil{t.Fatal(err)}
 read,problem:=worker.readyTasks(ctx,1)
 if problem==nil||!strings.Contains(problem.Error(),broken.ID)||
  len(read)!=1||read[0].ID!=good.ID{
  t.Fatalf("corrupt high-priority Task stole READY slot: healthy=%+v err=%v",read,problem)
 }
 // Disable Project admission so Tick can prove it reached the correct
 // sibling without requiring real model/scheduler service dependencies.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces
  SET status='archived' WHERE id=?`,pw.ID);err!=nil{t.Fatal(err)}
 results,problem:=worker.Tick(ctx,1)
 if problem==nil||!strings.Contains(problem.Error(),broken.ID)||
  len(results)!=1||results[0].TaskID!=good.ID||
  results[0].Error==""{
  t.Fatalf("Tick did not process healthy sibling safely: %+v %v",results,problem)
 }
 var attempts int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts
  WHERE task_id IN (?,?)`,broken.ID,good.ID).Scan(&attempts);err!=nil{t.Fatal(err)}
 if attempts!=0{t.Fatalf("inactive Project Workspace opened %d unsafe Attempts",attempts)}
}
