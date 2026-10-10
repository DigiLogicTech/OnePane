package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
 _ "modernc.org/sqlite"
)

func TestQAExecutionEvidenceJoinsOnlyPersistedCanonicalTaskAndTenant(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"execution.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 stmts:=[]string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT)`,
  `CREATE TABLE inference_requests(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,status TEXT,request_json TEXT)`,
  `CREATE TABLE tool_invocations(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,status TEXT,result_json TEXT)`,
  `CREATE TABLE operations(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,state TEXT,resource_ref TEXT)`,
  `CREATE TABLE verifications(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,status TEXT,result_json TEXT)`,
  `INSERT INTO tasks VALUES('world','tenant','game','world'),('story','tenant','game','story'),
   ('other-project','tenant','different','world'),('foreign-tenant','elsewhere','game','world')`,
  `INSERT INTO inference_requests VALUES
   ('m1','tenant','world','succeeded','{"secret":"CANARY_SECRETS"}'),
   ('m2','tenant','world','unknown','{"secret":"CANARY_SECRETS"}'),
   ('m3','tenant','world','cancelled','{"secret":"CANARY_SECRETS"}'),
   ('m4','tenant','story','failed','{"secret":"CANARY_SECRETS"}'),
   ('m5','tenant','other-project','succeeded','{"secret":"CANARY_SECRETS"}'),
   ('m6','elsewhere','world','succeeded','{"secret":"CANARY_SECRETS"}')`,
  `INSERT INTO tool_invocations VALUES
   ('t1','tenant','world','succeeded','{"secret":"CANARY_SECRETS"}'),
   ('t2','tenant','world','interrupted','{"secret":"CANARY_SECRETS"}'),
   ('t3','tenant','world','failed','{"secret":"CANARY_SECRETS"}'),
   ('t4','tenant','story','succeeded','{"secret":"CANARY_SECRETS"}'),
   ('t5','elsewhere','world','failed','{"secret":"CANARY_SECRETS"}')`,
  `INSERT INTO operations VALUES
   ('o1','tenant','world','committed','CANARY_SECRETS'),
   ('o2','tenant','world','unknown_outcome','CANARY_SECRETS'),
   ('o3','tenant','world','compensated','CANARY_SECRETS'),
   ('o4','tenant','story','committed','CANARY_SECRETS'),
   ('o5','elsewhere','world','failed','CANARY_SECRETS')`,
  `INSERT INTO verifications VALUES
   ('v1','tenant','world','pass','{"secret":"CANARY_SECRETS"}'),
   ('v2','tenant','world','inconclusive','{"secret":"CANARY_SECRETS"}'),
   ('v3','tenant','world','pending','{"secret":"CANARY_SECRETS"}'),
   ('v4','tenant','story','fail','{"secret":"CANARY_SECRETS"}'),
   ('v5','elsewhere','world','pass','{"secret":"CANARY_SECRETS"}')`,
 }
 for _,s:=range stmts{if _,err:=db.ExecContext(ctx,s);err!=nil{t.Fatalf("%v: %s",err,s)}}
 project,world,story:="game","world","story"
 visible:=[]task.Task{
  {ID:"world",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world},
  {ID:"story",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&story},
  {ID:"foreign-tenant",WorkspaceID:"elsewhere",ProjectID:&project,ProjectWorkspaceID:&world},
  {ID:"other-project",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world},
 }
 got,truncated,err:=loadQAExecutionSources(ctx,db,"tenant",project,world,visible)
 if err!=nil{t.Fatal(err)}
 if truncated{t.Fatal("unexpected truncation")}
 if len(got)!=4{t.Fatalf("expected only 4 source aggregates for one authorised Task: %+v",got)}
 expected:=map[string][6]int64{
  "model":{3,1,0,1,0,1},
  "tool":{3,1,1,1,0,0},
  "operation":{3,1,0,1,0,1},
  "verification":{3,1,0,1,1,0},
 }
 for _,g:=range got{
  if g.TaskRef!=qaOpaqueRef("task","world"){t.Fatalf("foreign Task leaked: %+v",g)}
  want,ok:=expected[g.Source];if !ok{t.Fatalf("bad source: %+v",g)}
  actual:=[6]int64{g.Total,g.Succeeded,g.Failed,g.Uncertain,g.Pending,g.Unclassified}
  if actual!=want||g.Capped{t.Fatalf("wrong categories for %s: got %v want %v",g.Source,actual,want)}
 }
 raw,_:=json.Marshal(got)
 for _,secret:=range []string{"CANARY_SECRETS","story","other-project","foreign-tenant","world","m1","t1","o1","v1","resource_ref","request_json"}{
  if strings.Contains(string(raw),secret){t.Fatalf("source text leaked %q: %s",secret,raw)}
 }
 forged:=[]task.Task{{ID:"story",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world}}
 no,_,err:=loadQAExecutionSources(ctx,db,"tenant",project,world,forged)
 if err!=nil||len(no)!=0{t.Fatalf("forged canonical membership accepted: %+v %v",no,err)}
 for _,x:=range []struct{tenant,project,workspace string}{
  {"elsewhere","game","world"},{"tenant","game","missing"},{"tenant","different","world"},
 }{
  no,_,err=loadQAExecutionSources(ctx,db,x.tenant,x.project,x.workspace,
   []task.Task{{ID:"world",WorkspaceID:x.tenant,ProjectID:&x.project,ProjectWorkspaceID:&x.workspace}})
  if err!=nil||len(no)!=0{t.Fatalf("wrong scope leaked: %+v %v",x,err)}
 }
 if no,_,err=loadQAExecutionSources(ctx,db,"tenant","game","",visible);err!=nil||len(no)!=0{
  t.Fatalf("empty canonical Workspace should fail closed: %+v %v",no,err)
 }
}

func TestQAExecutionEvidenceFailsClosedIfSourceUnavailable(t *testing.T){
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"missing-table.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT)`,
  `INSERT INTO tasks VALUES('world','tenant','game','world')`,
 }{if _,err=db.Exec(q);err!=nil{t.Fatal(err)}}
 project,world:="game","world"
 _,_,err=loadQAExecutionSources(context.Background(),db,"tenant",project,world,
  []task.Task{{ID:"world",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world}})
 if err==nil{t.Fatal("partial evidence falsely returned when model/tool/verification sources are missing")}
}

func TestQAExecutionEvidenceCapTruthfullyReportsOmittedTaskGroups(t *testing.T){
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"cap.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT)`,
  `CREATE TABLE inference_requests(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,status TEXT)`,
  `CREATE TABLE tool_invocations(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,status TEXT)`,
  `CREATE TABLE operations(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,state TEXT)`,
  `CREATE TABLE verifications(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,status TEXT)`,
 }{if _,err:=db.Exec(q);err!=nil{t.Fatal(err)}}
 project,workspace:="project","world"
 visible:=make([]task.Task,0,18)
 for n:=0;n<18;n++{
  id:=fmt.Sprintf("task-%02d",n)
  visible=append(visible,task.Task{ID:id,WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&workspace})
  for _,q:=range []string{
   `INSERT INTO tasks VALUES(?,'tenant','project','world')`,
   `INSERT INTO inference_requests VALUES(?,'tenant',?,'succeeded')`,
   `INSERT INTO tool_invocations VALUES(?,'tenant',?,'succeeded')`,
   `INSERT INTO operations VALUES(?,'tenant',?,'committed')`,
   `INSERT INTO verifications VALUES(?,'tenant',?,'pass')`,
  }{
   if strings.HasPrefix(q,"INSERT INTO tasks"){
    if _,err:=db.Exec(q,id);err!=nil{t.Fatal(err)}
   }else{
    if _,err:=db.Exec(q,fmt.Sprintf("%s-%s",id,q[12:15]),id);err!=nil{t.Fatal(err)}
   }
  }
 }
 rows,truncated,err:=loadQAExecutionSources(context.Background(),db,"tenant",project,workspace,visible)
 if err!=nil{t.Fatal(err)}
 if len(rows)!=qaExecutionSourceCap||!truncated{t.Fatalf("must advertise source truncation; rows=%d cap=%d truncated=%v",len(rows),qaExecutionSourceCap,truncated)}
}
