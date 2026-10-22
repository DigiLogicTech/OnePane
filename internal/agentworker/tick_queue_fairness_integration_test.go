//go:build integration

package agentworker

import (
 "context"
 "database/sql"
 "fmt"
 "path/filepath"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func tickQueueFairnessFixture(t *testing.T,activeCount int)(
 context.Context,*sql.DB,*Service,[]string,string){
 t.Helper()
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second)
 t.Cleanup(cancel)
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"tick-fairness.db"))
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ =db.Close()})
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1)
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
  WorkspaceID:"tenant",Name:"Fair scheduling acceptance",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 view,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Isolated fair queue",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"node-local",tasks,
  nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 activeIDs:=make([]string,0,activeCount)
 for n:=0;n<activeCount+1;n++{
  created,err:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",ProjectID:&p.ID,
   ProjectWorkspaceID:&view.ID,Objective:fmt.Sprintf("fair queued work %d",n)})
  if err!=nil{t.Fatal(err)}
  ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision})
  if err!=nil{t.Fatal(err)}
  if n==activeCount{
   // Archive later to reject startRun without creating new Attempts.
   if _,err:=db.SQL().ExecContext(ctx,`UPDATE project_workspaces
    SET status='archived' WHERE id=?`,view.ID);err!=nil{t.Fatal(err)}
   return ctx,db.SQL(),worker,activeIDs,ready.ID
  }
  run,res:=worker.startRun(ctx,ready)
  if res.Error!=""{t.Fatal(res.Error)}
  activeIDs=append(activeIDs,run.TaskID)
 }
 t.Fatal("unreachable queue fixture")
 return nil,nil,nil,nil,""
}

func assertOneReadyAttemptRejected(t *testing.T,ctx context.Context,db *sql.DB,readyID string){
 t.Helper()
 var count int
 if err:=db.QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts
  WHERE task_id=?`,readyID).Scan(&count);err!=nil{t.Fatal(err)}
 if count!=0{t.Fatalf("archived Workspace obtained %d unauthorized Attempts",count)}
}

// Active Workers currently fill both Tick slots. An eligible READY Task
// needs a bounded opportunity on the SAME Tick, without bypassing its
// Workspace permission and without starving the other active Worker.
func TestTickReservesReadyOpportunityUnderSaturatedActiveQueue(t *testing.T){
 ctx,db,worker,activeIDs,readyID:=tickQueueFairnessFixture(t,2)
 seenActive:=map[string]bool{}
 for n:=0;n<2;n++ {
  results,err:=worker.Tick(ctx,2)
  if err!=nil{t.Fatalf("tick %d: %v",n,err)}
  if len(results)!=2{t.Fatalf("tick %d returned %d results: %+v",n,len(results),results)}
  active,ready:=0,0
  for _,result:=range results {
   if result.TaskID==readyID{
    ready++
    if result.Error==""{t.Fatalf("archived READY Task was not denied: %+v",result)}
   }else{
    active++
    seenActive[result.TaskID]=true
   }
  }
  if ready!=1||active!=1{
   t.Fatalf("active queue starved READY slot: active=%d ready=%d results=%+v",active,ready,results)
  }
  assertOneReadyAttemptRejected(t,ctx,db,readyID)
 }
 if !seenActive[activeIDs[0]]||!seenActive[activeIDs[1]]{
  t.Fatalf("active Worker never got a fair slot: seen=%v",seenActive)
 }
 var attempts int
 if err:=db.QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts`).
  Scan(&attempts);err!=nil{t.Fatal(err)}
 if attempts!=2{t.Fatalf("Tick duplicated active Attempts: %d",attempts)}
}

// With only one step available, permanently ineligible READY Tasks must
// not starve active Workers. Alternate opportunities between the queues.
func TestTickSingleSlotAlternatesActiveAndReady(t *testing.T){
 ctx,db,worker,activeIDs,readyID:=tickQueueFairnessFixture(t,3)
 first,err:=worker.Tick(ctx,1)
 if err!=nil||len(first)!=1||first[0].TaskID==readyID{
  t.Fatalf("first single-slot tick should service active Worker: %+v %v",first,err)
 }
 second,err:=worker.Tick(ctx,1)
 if err!=nil||len(second)!=1||second[0].TaskID!=readyID||
  second[0].Error==""{
  t.Fatalf("second single-slot tick did not service denied READY Task: %+v %v",second,err)
 }
 third,err:=worker.Tick(ctx,1)
 if err!=nil||len(third)!=1||third[0].TaskID==readyID{
  t.Fatalf("third single-slot tick did not rotate back to active Worker: %+v %v",third,err)
 }
 seen:=map[string]bool{first[0].TaskID:true,third[0].TaskID:true}
 if len(seen)!=2{t.Fatalf("repeated active Worker instead of fair cursor rotation: %v",seen)}
 for id:=range seen{
  found:=false
  for _,active:=range activeIDs{if active==id{found=true}}
  if !found{t.Fatalf("unknown Worker in Tick: %s",id)}
 }
 assertOneReadyAttemptRejected(t,ctx,db,readyID)
}
