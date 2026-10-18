//go:build integration

package agentworker

import (
 "context"
 "fmt"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// A damaged high-priority READY queue larger than 128 rows must not block
// an independent healthy Task or consume unbounded CPU on each worker Tick.
func TestReadyQueueHasBoundedScanAndFairRotation(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),120*time.Second)
 defer cancel()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"ready-tick-budget.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1)
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"node-local",tasks,
  nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 const broken=130
 for n:=0;n<broken;n++{
  created,err:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",Objective:fmt.Sprintf("damaged READY model task %03d",n),
   Priority:100})
  if err!=nil{t.Fatal(err)}
  if _,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision});err!=nil{t.Fatal(err)}
 }
 created,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",Objective:"legitimate waiting local model work",Priority:1})
 if err!=nil{t.Fatal(err)}
 good,err:=tasks.MarkReady(ctx,task.TransitionCommand{
  TaskID:created.ID,ExpectedRevision:created.Revision})
 if err!=nil{t.Fatal(err)}
 // Simulate legacy stored-state corruption only for the 130 Tasks.
 conn,err:=db.SQL().Conn(ctx)
 if err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`PRAGMA ignore_check_constraints=ON`);err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`UPDATE tasks SET completion_json='broken-json'
  WHERE priority=100`);err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`PRAGMA ignore_check_constraints=OFF`);err!=nil{t.Fatal(err)}
 if err=conn.Close();err!=nil{t.Fatal(err)}
 selected,problem:=worker.readyTasks(ctx,1)
 if len(selected)!=0||problem==nil||
  !strings.Contains(problem.Error(),"96 additional corrupt READY Tasks require operator review"){
  t.Fatalf("first READY Tick scanned outside its 128-row budget: selected=%+v error=%v",
   selected,problem)
 }
 selected,problem=worker.readyTasks(ctx,1)
 if len(selected)!=1||selected[0].ID!=good.ID||problem==nil||
  strings.Contains(problem.Error(),"additional corrupt READY Tasks"){
  t.Fatalf("later healthy READY Task was starved: selected=%+v error=%v",selected,problem)
 }
 var attempts,ready int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts`).
  Scan(&attempts);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM tasks WHERE state='ready'`).
  Scan(&ready);err!=nil{t.Fatal(err)}
 if attempts!=0||ready!=broken+1{
  t.Fatalf("read-only inspection changed Task authority: attempts=%d ready=%d",attempts,ready)
 }
 selected,problem=worker.readyTasks(ctx,1)
 if len(selected)!=0||problem==nil||
  !strings.Contains(problem.Error(),"96 additional corrupt READY Tasks require operator review"){
  t.Fatalf("READY cursor did not safely wrap: selected=%+v error=%v",selected,problem)
 }
}
