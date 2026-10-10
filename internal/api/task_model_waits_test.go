package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
 _ "modernc.org/sqlite"
)

func TestTaskWaitProjectionRejectsMalformedAndUnrelatedContinuations(t *testing.T){
 good:=`{"model_wait":{"attempt":3,"retry_at_ms":1800000012345,"reason":"awaiting local model"}}`
 x,ok:=parseTaskModelWait(good)
 if !ok||x.Kind!="model_resources"||x.Attempt!=3||x.RetryAtMS!=1800000012345{
  t.Fatalf("valid model wait not parsed: %+v %v",x,ok)
 }
 for _,v:=range []string{
  "",`{}`,`{"operation_id":"op-1"}`,
  `{"model_wait":{"attempt":0,"retry_at_ms":100}}`,
  `{"model_wait":{"attempt":1,"retry_at_ms":0}}`,
  `{"model_wait":"not-an-object"}`,`garbage`,
 }{
  if _,ok:=parseTaskModelWait(v);ok{t.Fatalf("invalid wait treated as model status: %s",v)}
 }
}

func TestTaskWaitProjectionIsBoundToVisibleTenantAndTaskState(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"task_wait_visibility.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE tasks (id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,state TEXT NOT NULL)`,
  `CREATE TABLE agent_worker_runs (id TEXT PRIMARY KEY,task_id TEXT NOT NULL,workspace_id TEXT NOT NULL,status TEXT NOT NULL,continuation_json TEXT NOT NULL,updated_at INTEGER NOT NULL)`,
  `INSERT INTO tasks VALUES('world-wait','tenant','waiting_dependency'),('other-tenant','elsewhere','waiting_dependency'),('finished','tenant','complete'),('approval','tenant','waiting_approval')`,
 }{
  if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 raw,_:=json.Marshal(map[string]any{"model_wait":map[string]any{"attempt":2,"retry_at_ms":1800000099999,"reason":"temporary local model shortage"}})
 for _,record:=range []struct{id,task,tenant,status string;date int64}{
  {"r1","world-wait","tenant","waiting",20},
  {"r2","other-tenant","elsewhere","waiting",20},
  {"r3","finished","tenant","waiting",20},
  {"r4","approval","tenant","waiting",20},
  {"r5","world-wait","tenant","running",30},
  {"r6","world-wait","elsewhere","waiting",99},
 }{
  if _,err:=db.ExecContext(ctx,
   `INSERT INTO agent_worker_runs(id,task_id,workspace_id,status,continuation_json,updated_at) VALUES(?,?,?,?,?,?)`,
   record.id,record.task,record.tenant,record.status,string(raw),record.date);err!=nil{t.Fatal(err)}
 }
 rows:=[]task.Task{
  {ID:"world-wait",WorkspaceID:"tenant",State:task.StateWaitingDependency},
  {ID:"other-tenant",WorkspaceID:"elsewhere",State:task.StateWaitingDependency},
  {ID:"finished",WorkspaceID:"tenant",State:task.StateComplete},
  {ID:"approval",WorkspaceID:"tenant",State:task.StateWaitingApproval},
 }
 waits,err:=loadTaskModelWaits(ctx,db,"tenant",rows)
 if err!=nil{t.Fatal(err)}
 if len(waits)!=1||waits["world-wait"].Attempt!=2||waits["world-wait"].Kind!="model_resources"{
  t.Fatalf("Task status leaked foreign tenancy, approval or terminal state: %+v",waits)
 }
 var empty []task.Task
 none,err:=loadTaskModelWaits(ctx,db,"tenant",empty)
 if err!=nil||len(none)!=0{t.Fatalf("empty inventory should not read any runs: %+v %v",none,err)}
}


func TestTaskWaitProjectionForApprovedOCIResourceWaitHidesIdentifiers(t *testing.T) {
 digest:=strings.Repeat("a",64)
 raw:=`{"toolchain_wait":{"project_id":"SECRET-PROJECT","project_workspace_id":"SECRET-WORKSPACE","runtime_id":"SECRET-RUNTIME","application_id":"SECRET-APP","manifest_sha256":"`+digest+`","retry_at_ms":1800000099999}}`
 got,ok:=parseTaskModelWait(raw)
 if !ok||got.Kind!="toolchain_resources"||got.RetryAtMS!=1800000099999||got.Attempt!=0{
  t.Fatalf("approved OCI wait was not surfaced: %+v %v",got,ok)
 }
 safe,err:=json.Marshal(got)
 if err!=nil{t.Fatal(err)}
 for _,secret:=range []string{"SECRET-PROJECT","SECRET-WORKSPACE","SECRET-RUNTIME","SECRET-APP",digest}{
  if strings.Contains(string(safe),secret){t.Fatalf("wait metadata leaked internal runtime identity: %s",safe)}
 }
 }
 for _,bad:=range []string{
  `{"toolchain_wait":{"retry_at_ms":1800000099999}}`,
  `{"toolchain_wait":{"project_id":"p","project_workspace_id":"w","runtime_id":"r","application_id":"a","manifest_sha256":"not-sha","retry_at_ms":5}}`,
  `{"model_wait":{"attempt":1,"retry_at_ms":5},"toolchain_wait":{"project_id":"p","project_workspace_id":"w","runtime_id":"r","application_id":"a","manifest_sha256":"`+digest+`","retry_at_ms":5}}`,
  `{"toolchain_wait":"forged-ready"}`,
 }{
  if _,ok:=parseTaskModelWait(bad);ok{t.Fatalf("accepted corrupt or ambiguous resource wait: %s",bad)}
 }
}
