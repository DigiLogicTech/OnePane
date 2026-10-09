package api

import (
 "context"
 "database/sql"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
 _ "modernc.org/sqlite"
)

func TestTaskWorkerProgressIsScopedAndLatestRunOnly(t *testing.T) {
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"execution_projection.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 schema:=[]string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,project_id TEXT,project_workspace_id TEXT)`,
  `CREATE TABLE agent_worker_runs(id TEXT PRIMARY KEY,task_id TEXT NOT NULL,workspace_id TEXT NOT NULL,
   status TEXT NOT NULL,step_count INTEGER NOT NULL,max_steps INTEGER NOT NULL,updated_at INTEGER NOT NULL,started_at INTEGER NOT NULL)`,
  `CREATE TABLE agent_worker_steps(id TEXT PRIMARY KEY,run_id TEXT NOT NULL,step_number INTEGER NOT NULL,step_kind TEXT NOT NULL,status TEXT NOT NULL)`,
  `INSERT INTO tasks VALUES
   ('world-task','tenant','project','world'),
   ('story-task','tenant','project','story'),
   ('other-project','tenant','other-project','world'),
   ('other-tenant','elsewhere','project','world')`,
  `INSERT INTO agent_worker_runs VALUES
   ('old','world-task','tenant','succeeded',2,24,10,10),
   ('new','world-task','tenant','interrupted',4,24,30,20),
   ('story','story-task','tenant','running',2,24,40,40),
   ('other-project','other-project','tenant','running',1,24,50,50),
   ('foreign','other-tenant','elsewhere','running',1,24,60,60),
   ('wrong-run-scope','story-task','elsewhere','running',8,24,90,90)`,
  `INSERT INTO agent_worker_steps VALUES
   ('s-old','old',2,'model','succeeded'),
   ('s-new','new',1,'tool','unknown'),
   ('s-story','story',1,'route','succeeded')`,
 }
 for _,q:=range schema{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 project,world:="project","world"
 otherProject,story,otherTenant:="other-project","story","elsewhere"
 visible:=[]task.Task{
  {ID:"world-task",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world},
  {ID:"story-task",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&story},
  {ID:"other-project",WorkspaceID:"tenant",ProjectID:&otherProject,ProjectWorkspaceID:&world},
  {ID:"other-tenant",WorkspaceID:otherTenant,ProjectID:&project,ProjectWorkspaceID:&world},
 }
 progress,err:=loadTaskExecutionProgress(ctx,db,"tenant","project","world",visible)
 if err!=nil{t.Fatal(err)}
 if len(progress)!=1{t.Fatalf("leaked foreign Workspace or tenant progress: %+v",progress)}
 x,ok:=progress["world-task"]
 if !ok||x.RunID!="new"||x.Status!="interrupted"||x.StepsUsed!=4||
  x.MaxSteps!=24||x.LastStepKind!="tool"||x.LastStepStatus!="unknown"||!x.ReviewRequired{
  t.Fatalf("latest persisted checkpoint not recovered: %+v found=%v",x,ok)
 }
 noScope,err:=loadTaskExecutionProgress(ctx,db,"tenant","project","",visible)
 if err!=nil||len(noScope)!=0{t.Fatalf("unscoped projection allowed: %+v %v",noScope,err)}
 noIDs,err:=loadTaskExecutionProgress(ctx,db,"tenant","project","world",nil)
 if err!=nil||len(noIDs)!=0{t.Fatalf("empty Task page must not read worker journal: %+v %v",noIDs,err)}
 // Never trust a caller's visible list to override authoritative Task scope
 // or a run's persisted workspace ownership.
 mismatch:=[]task.Task{{ID:"story-task",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world}}
 denied,err:=loadTaskExecutionProgress(ctx,db,"tenant","project","world",mismatch)
 if err!=nil||len(denied)!=0{t.Fatalf("tampered visible list leaked Story Task: %+v %v",denied,err)}
}

func TestTaskWorkerProgressInvalidCountersFailClosed(t *testing.T) {
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"bad_counters.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT)`,
  `CREATE TABLE agent_worker_runs(id TEXT PRIMARY KEY,task_id TEXT,workspace_id TEXT,status TEXT,step_count INTEGER,max_steps INTEGER,updated_at INTEGER,started_at INTEGER)`,
  `CREATE TABLE agent_worker_steps(id TEXT PRIMARY KEY,run_id TEXT,step_number INTEGER,step_kind TEXT,status TEXT)`,
  `INSERT INTO tasks VALUES('t','tenant','p','w')`,
  `INSERT INTO agent_worker_runs VALUES('bad','t','tenant','running',25,24,1,1)`,
 }{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 p,w:="p","w"
 rows:=[]task.Task{{ID:"t",WorkspaceID:"tenant",ProjectID:&p,ProjectWorkspaceID:&w}}
 found,err:=loadTaskExecutionProgress(ctx,db,"tenant",p,w,rows)
 if err!=nil||len(found)!=0{t.Fatalf("invalid checkpoint counters must not be represented as verified progress: %+v %v",found,err)}
}
