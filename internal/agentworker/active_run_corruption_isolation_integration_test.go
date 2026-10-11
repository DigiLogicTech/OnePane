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

func TestCorruptActiveWorkerDoesNotStarveIndependentRunOrTick(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlite.Open(filepath.Join(t.TempDir(),"active-worker-corruption.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 clk:=clock.Real{}
 now:=clk.UnixMilli()
 for _,statement:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',?,?)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,statement,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clk)
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Isolated recoverable Project",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 pw,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Task Lab",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clk)
 worker:=New(db.SQL(),db,clk,"local",tasks,nil,nil,nil,nil,nil,nil,nil,nil,nil,nil)
 if err:=worker.ensureWorkspaceAccess(ctx,"tenant");err!=nil{t.Fatal(err)}
 start:=func(name string)Run{
  t.Helper()
  created,e:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",ProjectID:&p.ID,ProjectWorkspaceID:&pw.ID,Objective:name,
  })
  if e!=nil{t.Fatal(e)}
  ready,e:=tasks.MarkReady(ctx,task.TransitionCommand{
   TaskID:created.ID,ExpectedRevision:created.Revision,
  })
  if e!=nil{t.Fatal(e)}
  r,res:=worker.startRun(ctx,ready)
  if res.Error!=""||r.Status!=RunRunning{t.Fatalf("start %s: %+v %+v",name,r,res)}
  return r
 }
 bad:=start("Persisted corrupt prior Worker")
 good:=start("Independent Worker must still receive a tick")
 // The production schema prevents corrupt writes. Simulate a damaged
 // pre-upgrade database using a connection-scoped SQLite repair pragma.
 conn,err:=db.SQL().Conn(ctx)
 if err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`PRAGMA ignore_check_constraints=ON`);err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`UPDATE agent_worker_runs
  SET continuation_json='broken-json',updated_at=0 WHERE id=?`,bad.ID);err!=nil{t.Fatal(err)}
 if _,err=conn.ExecContext(ctx,`PRAGMA ignore_check_constraints=OFF`);err!=nil{t.Fatal(err)}
 if err=conn.Close();err!=nil{t.Fatal(err)}
 active,problem:=worker.activeRuns(ctx,1)
 if problem==nil||!strings.Contains(problem.Error(),bad.ID)||
  len(active)!=1||active[0].ID!=good.ID{
  t.Fatalf("corrupt Worker stole bounded active slot: run=%+v error=%v",active,problem)
 }
 // An archived Project is blocked BEFORE any inference/tool dispatch.
 // Processing this independent healthy row must not require a scheduler
 // or replay an external operation.
 if _,err:=db.SQL().ExecContext(ctx,`UPDATE projects SET status='archived'
  WHERE id=?`,p.ID);err!=nil{t.Fatal(err)}
 results,problem:=worker.Tick(ctx,1)
 if problem==nil||!strings.Contains(problem.Error(),bad.ID)||
  len(results)!=1||results[0].RunID!=good.ID||results[0].Status!="blocked"{
  t.Fatalf("damaged Worker starved healthy archived-Project review: %+v %v",results,problem)
 }
 var badState,goodState string
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,
  bad.ID).Scan(&badState);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=?`,
  good.ID).Scan(&goodState);err!=nil{t.Fatal(err)}
 if badState!="running"||goodState!="blocked"{
  t.Fatalf("corrupt run mutated or independent work did not block: bad=%s good=%s",badState,goodState)
 }
}
