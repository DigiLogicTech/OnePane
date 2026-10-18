//go:build integration

package agentworker

import (
 "context"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestCreatedAdmissionPagesPastMoreThanSixtyFourBrokenTasks(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"created-admission-fairness.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1) // Reads must close before READY write transactions.
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 var firstBroken string
 for i:=0;i<66;i++{
  created,e:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",Objective:"Injected failed admission",
   Priority:100-i,
  })
  if e!=nil{t.Fatal(e)}
  if i==0{firstBroken=created.ID}
 }
 healthy,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",Objective:"Legitimate lower-priority Task must progress",
  Priority:1,
 })
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER fail_high_priority_ready
  BEFORE UPDATE OF state ON tasks
  WHEN NEW.state='ready' AND NEW.priority>=35
  BEGIN SELECT RAISE(ABORT,'synthetic per-Task admission failure'); END`);err!=nil{t.Fatal(err)}
 n,problem:=worker.admitCreated(ctx,2)
 if n!=1||problem==nil||!strings.Contains(problem.Error(),firstBroken)||
  !strings.Contains(problem.Error(),"additional failed CREATED Task admissions"){
  t.Fatalf("broken high-priority Tasks monopolized bounded admission: n=%d err=%v",n,problem)
 }
 state,err:=tasks.Get(ctx,healthy.ID)
 if err!=nil||state.State!=task.StateReady{
  t.Fatalf("healthy Task did not reach READY despite 66 earlier failures: %+v %v",state,err)
 }
 var failures int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM tasks
  WHERE state='created' AND priority>=35`).Scan(&failures);err!=nil{t.Fatal(err)}
 if failures!=66{t.Fatalf("failed admissions were silently promoted or removed: %d",failures)}
 var eventCount int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_type='task' AND aggregate_id=? AND event_type='task.ready'`,
  healthy.ID).Scan(&eventCount);err!=nil{t.Fatal(err)}
 if eventCount!=1{t.Fatalf("successful independent Task READY event count=%d",eventCount)}
 n,problem=worker.admitCreated(ctx,2)
 if n!=0||problem==nil||!strings.Contains(problem.Error(),firstBroken){
  t.Fatalf("repeated recovery hid still-corrupt Tasks: n=%d err=%v",n,problem)
 }
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_type='task' AND aggregate_id=? AND event_type='task.ready'`,
  healthy.ID).Scan(&eventCount);err!=nil{t.Fatal(err)}
 if eventCount!=1{t.Fatalf("repeated admission duplicated healthy audit: %d",eventCount)}
}
