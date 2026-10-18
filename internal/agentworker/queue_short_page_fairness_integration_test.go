//go:build integration

package agentworker

import (
 "context"
 "database/sql"
 "fmt"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func newShortPageQueueFixture(t *testing.T)(context.Context,*sql.DB,*task.Service,*Service){
 t.Helper()
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second)
 t.Cleanup(cancel)
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"short-page.db"))
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ =db.Close()})
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1)
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"local",tasks,
  nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 return ctx,db.SQL(),tasks,worker
}

// Even three healthy active Workers can starve if a short query page
// resets the cursor after the first success instead of the actual page end.
func TestActiveQueueShortPageRetainsCursorAfterLimit(t *testing.T){
 ctx,db,tasks,worker:=newShortPageQueueFixture(t)
 ids:=make([]string,0,3)
 for n:=0;n<3;n++ {
  created,err:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",Objective:fmt.Sprintf("active short page %d",n)})
  if err!=nil{t.Fatal(err)}
  ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision})
  if err!=nil{t.Fatal(err)}
  run,res:=worker.startRun(ctx,ready)
  if res.Error!=""{t.Fatal(res.Error)}
  if _,err:=db.ExecContext(ctx,`UPDATE agent_worker_runs
   SET updated_at=? WHERE id=?`,int64(n+1),run.ID);err!=nil{t.Fatal(err)}
  ids=append(ids,run.ID)
 }
 for n:=0;n<4;n++ {
  selected,err:=worker.activeRuns(ctx,1)
  expected:=ids[n%3]
  if err!=nil||len(selected)!=1||selected[0].ID!=expected{
   t.Fatalf("active short-page call %d returned %+v err=%v, want %s",n,selected,err,expected)
  }
 }
 var attempts int
 if err:=db.QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts`).Scan(&attempts);err!=nil{t.Fatal(err)}
 if attempts!=3{t.Fatalf("selection created extra attempts: %d",attempts)}
}

// READY selection itself is read-only. When the first valid record is
// repeatedly selected on a <64-row page, the second/third would never run.
func TestReadyQueueShortPageRetainsCursorAfterLimit(t *testing.T){
 ctx,db,tasks,worker:=newShortPageQueueFixture(t)
 ids:=make([]string,0,3)
 for n:=0;n<3;n++{
  created,err:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",Objective:fmt.Sprintf("ready short page %d",n),
   Priority:30-n*10})
  if err!=nil{t.Fatal(err)}
  if _,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision});err!=nil{t.Fatal(err)}
  ids=append(ids,created.ID)
 }
 for n:=0;n<4;n++{
  selected,err:=worker.readyTasks(ctx,1)
  expected:=ids[n%3]
  if err!=nil||len(selected)!=1||selected[0].ID!=expected{
   t.Fatalf("READY short-page call %d returned %+v err=%v, want %s",
    n,selected,err,expected)
  }
 }
 var attempts int
 if err:=db.QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts`).Scan(&attempts);err!=nil{t.Fatal(err)}
 if attempts!=0{t.Fatalf("READY selection created unauthorized attempts: %d",attempts)}
}

// A persistent high-priority CREATED failure should be reported once during
// a short page. Successful quota 1 must not rewind the next Tick to that
// failure while lower-priority siblings remain eligible for admission.
func TestCreatedQueueShortPageRetainsCursorAfterSuccess(t *testing.T){
 ctx,db,tasks,worker:=newShortPageQueueFixture(t)
 ids:=make([]string,0,3)
 for n,priority:=range []int{100,90,80}{
  created,err:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",Objective:fmt.Sprintf("created short page %d",n),
   Priority:priority})
  if err!=nil{t.Fatal(err)}
  ids=append(ids,created.ID)
 }
 if _,err:=db.ExecContext(ctx,`CREATE TRIGGER reject_first_created_ready
  BEFORE UPDATE OF state ON tasks WHEN NEW.state='ready' AND NEW.priority=100
  BEGIN SELECT RAISE(ABORT,'synthetic first CREATED admission failure'); END`);err!=nil{t.Fatal(err)}
 n,err:=worker.admitCreated(ctx,1)
 if n!=1||err==nil||!strings.Contains(err.Error(),ids[0]){
  t.Fatalf("first admission must report first failure and admit next: n=%d err=%v",n,err)
 }
 n,err=worker.admitCreated(ctx,1)
 if n!=1||err!=nil{
  t.Fatalf("second admission revisited first corrupt Task: n=%d err=%v",n,err)
 }
 for i,id:=range ids {
  got,err:=tasks.Get(ctx,id)
  if err!=nil{t.Fatal(err)}
  want:=task.StateReady
  if i==0{want=task.StateCreated}
  if got.State!=want{t.Fatalf("Task %s state=%s want=%s",id,got.State,want)}
 }
 n,err=worker.admitCreated(ctx,1)
 if n!=0||err==nil||!strings.Contains(err.Error(),ids[0]){
  t.Fatalf("next call did not safely wrap back to corrupt Task: n=%d err=%v",n,err)
 }
 var readyEvents int
 if err:=db.QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE event_type='task.ready' AND aggregate_id IN (?,?)`,ids[1],ids[2]).
  Scan(&readyEvents);err!=nil{t.Fatal(err)}
 if readyEvents!=2{t.Fatalf("successful admissions duplicated audit: %d",readyEvents)}
}
