//go:build integration

package agentworker

import (
 "context"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// Admission may encounter persistent errors from many earlier high-priority
// Tasks. Cap each Tick's CPU/SQL work but rotate the cursor so independently
// healthy lower-priority Tasks still advance on a later bounded Tick.
func TestCreatedAdmissionTickBudgetIsBoundedAndFairAcrossTicks(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second)
 defer cancel()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"created-budget.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 db.SQL().SetMaxOpenConns(1)
 now:=clock.Real{}.UnixMilli()
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO workspaces(
  id,name,status,revision,created_at,updated_at)
  VALUES('tenant','Tenant','active',1,?,?)`,now,now);err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 worker:=New(db.SQL(),db,clock.Real{},"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 const bad=130
 for i:=0;i<bad;i++ {
  if _,err:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",Objective:"Bad high-priority Task awaiting repair",
   Priority:100});err!=nil{t.Fatal(err)}
 }
 healthy,err:=tasks.Create(ctx,task.CreateCommand{
  WorkspaceID:"tenant",Objective:"Healthy Task after two pages of corruption",Priority:1})
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`CREATE TRIGGER fail_high_priority_ready
  BEFORE UPDATE OF state ON tasks
  WHEN NEW.state='ready' AND NEW.priority=100
  BEGIN SELECT RAISE(ABORT,'synthetic corrupt Task admission'); END`);err!=nil{t.Fatal(err)}
 // The first Tick must stop after exactly 128 failed checks; scanning all
 // 130 failures would repeat unbounded CPU/IO with a long unhealthy queue.
 promoted,problem:=worker.admitCreated(ctx,1)
 if promoted!=0||problem==nil||
  !strings.Contains(problem.Error(),"96 additional failed CREATED Task admissions"){
  t.Fatalf("one Tick ignored 128-check budget: promoted=%d err=%v",promoted,problem)
 }
 state,err:=tasks.Get(ctx,healthy.ID)
 if err!=nil||state.State!=task.StateCreated{
  t.Fatalf("healthy Task was prematurely selected: %+v %v",state,err)
 }
 // The saved cursor starts the next tick beyond the first 128 corrupt
 // candidates. Two more reject, then the Task behind them is admitted.
 promoted,problem=worker.admitCreated(ctx,1)
 if promoted!=1||problem==nil||
  strings.Contains(problem.Error(),"additional failed CREATED"){
  t.Fatalf("second Tick did not fairly reach healthy Task: promoted=%d err=%v",promoted,problem)
 }
 state,err=tasks.Get(ctx,healthy.ID)
 if err!=nil||state.State!=task.StateReady{
  t.Fatalf("healthy Task still blocked: %+v %v",state,err)
 }
 var events,broken int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_id=? AND event_type='task.ready'`,healthy.ID).Scan(&events);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM tasks
  WHERE state='created' AND priority=100`).Scan(&broken);err!=nil{t.Fatal(err)}
 if events!=1||broken!=bad{t.Fatalf("lost audit or expanded bad Tasks: events=%d broken=%d",events,broken)}
 // Re-running must not promote a previously broken Task or duplicate the
 // independent Task's ready event. The cursor loops safely over the backlog.
 promoted,problem=worker.admitCreated(ctx,1)
 if promoted!=0||problem==nil{t.Fatalf("repeated admission hid failed Tasks: %d %v",promoted,problem)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_id=? AND event_type='task.ready'`,healthy.ID).Scan(&events);err!=nil{t.Fatal(err)}
 if events!=1{t.Fatalf("healthy Task was admitted more than once: events=%d",events)}
}
