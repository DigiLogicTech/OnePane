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

// With 130 malformed Workers, a single scheduler tick must only inspect
// 128 entries. The second call must reach a healthy run behind the damage.
// No bad attempt should be restarted, retried or silently marked complete.
func TestActiveQueueHasBoundedScanAndFairRotation(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),120*time.Second)
 defer cancel()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"active-tick-budget.db"))
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
 damagedIDs:=make([]string,0,broken)
 healthyID:=""
 for n:=0;n<=broken;n++ {
  created,err:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",Objective:fmt.Sprintf("bounded active model queue %03d",n)})
  if err!=nil{t.Fatal(err)}
  ready,err:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision})
  if err!=nil{t.Fatal(err)}
  run,res:=worker.startRun(ctx,ready)
  if res.Error!=""{t.Fatal(res.Error)}
  if _,err:=db.SQL().ExecContext(ctx,`UPDATE agent_worker_runs
   SET updated_at=? WHERE id=?`,int64(n+1),run.ID);err!=nil{t.Fatal(err)}
  if n<broken{damagedIDs=append(damagedIDs,run.ID)}else{healthyID=run.ID}
 }
 // Simulate a damaged pre-upgrade database. CHECK constraints remain on for
 // every ordinary connection and are only bypassed on this repair handle.
 conn,err:=db.SQL().Conn(ctx)
 if err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`PRAGMA ignore_check_constraints=ON`);err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`UPDATE agent_worker_runs
  SET continuation_json='broken-json' WHERE updated_at<=?`,broken);err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`PRAGMA ignore_check_constraints=OFF`);err!=nil{t.Fatal(err)}
 if err=conn.Close();err!=nil{t.Fatal(err)}
 selected,problem:=worker.activeRuns(ctx,1)
 if len(selected)!=0||problem==nil||
  !strings.Contains(problem.Error(),damagedIDs[0])||
  !strings.Contains(problem.Error(),"96 additional corrupt active Workers require operator review"){
  t.Fatalf("first Tick exceeded 128 record budget: selected=%d err=%v",len(selected),problem)
 }
 if strings.Contains(problem.Error(),damagedIDs[32]){
  t.Fatalf("first Tick retained more than 32 damaged identities")
 }
 selected,problem=worker.activeRuns(ctx,1)
 if len(selected)!=1||selected[0].ID!=healthyID||problem==nil||
  !strings.Contains(problem.Error(),damagedIDs[129])||
  strings.Contains(problem.Error(),"additional corrupt active Workers"){
  t.Fatalf("fair continuation did not reach healthy Worker: selected=%+v err=%v",selected,problem)
 }
 var stillRunning,attempts int
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_worker_runs
  WHERE status='running'`).Scan(&stillRunning);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts`).Scan(&attempts);err!=nil{t.Fatal(err)}
 if stillRunning!=broken+1||attempts!=broken+1{
  t.Fatalf("queue scan changed effect state: running=%d attempts=%d",stillRunning,attempts)
 }
 // After reaching the tail, the cursor must wrap. Corrupt active runs are
 // still visible for operator repair; they are never silently discarded.
 selected,problem=worker.activeRuns(ctx,1)
 if len(selected)!=0||problem==nil||!strings.Contains(problem.Error(),damagedIDs[0]){
  t.Fatalf("queue wrap lost corrupt active records: selected=%+v err=%v",selected,problem)
 }
}
