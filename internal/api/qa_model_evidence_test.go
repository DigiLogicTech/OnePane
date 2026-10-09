package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "path/filepath"
 "strings"
 "testing"

 _ "modernc.org/sqlite"
)

func qaOpenModelTestDB(t *testing.T)*sql.DB{
 t.Helper()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"testbed-qa.db"))
 if err!=nil{t.Fatal(err)}
 for _,statement:=range []string{
  `CREATE TABLE model_deployments(id TEXT PRIMARY KEY,status TEXT,residency_state TEXT,updated_at INTEGER)`,
  `CREATE TABLE model_testbed_sessions(id TEXT PRIMARY KEY,deployment_id TEXT,status TEXT,started_at INTEGER,completed_at INTEGER,
    notes TEXT,placement_json TEXT)`,
  `CREATE TABLE model_testbed_turns(id TEXT PRIMARY KEY,session_id TEXT,synthetic_tool_probe INTEGER,
    request_json TEXT,response_json TEXT,metrics_json TEXT)`,
  `CREATE TABLE model_agentcheck_failure_observations(id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,deployment_id TEXT NOT NULL,stage TEXT NOT NULL,
    category TEXT NOT NULL,observed_at INTEGER NOT NULL)`,
 }{
  if _,err:=db.Exec(statement);err!=nil{_ = db.Close();t.Fatal(err)}
 }
 return db
}

func TestQAModelAgentCheckEvidenceOnlyIncludesAuthorizedDeploymentMetadata(t *testing.T){
 ctx:=context.Background()
 db:=qaOpenModelTestDB(t);defer db.Close()
 const secret="PRIVATE_PROMPT_BEARER_CANARY"
 for _,q:=range []string{
  `INSERT INTO model_deployments VALUES('allowed','ready','stopped',1700),('foreign','failed','resident',1800)`,
  `INSERT INTO model_testbed_sessions VALUES
     ('check-a','allowed','cancelled',1100,1200,'PRIVATE_PROMPT_BEARER_CANARY','{"token":"PRIVATE_PROMPT_BEARER_CANARY"}'),
     ('check-b','allowed','completed',1300,1500,'PRIVATE_PROMPT_BEARER_CANARY','{}'),
     ('check-other','foreign','completed',1600,1700,'PRIVATE_PROMPT_BEARER_CANARY','{}')`,
  `INSERT INTO model_agentcheck_failure_observations(session_id,deployment_id,stage,category,observed_at) VALUES
      ('check-a','allowed','inference_dispatch','deadline_exceeded',1175),
      ('check-a','allowed','session_abort','abort_requested',1299),
      ('check-b','allowed','runtime_unload','execution_failure',1560),
      ('check-other','foreign','runtime_acquire','not_found',1650)`,
  `INSERT INTO model_testbed_turns VALUES
     ('turn-a','check-a',0,'{"prompt":"PRIVATE_PROMPT_BEARER_CANARY"}','{"answer":"PRIVATE_PROMPT_BEARER_CANARY"}','{}'),
     ('turn-b','check-b',0,'{"prompt":"PRIVATE_PROMPT_BEARER_CANARY"}','{"answer":"PRIVATE_PROMPT_BEARER_CANARY"}','{}'),
     ('turn-c','check-b',1,'{"prompt":"PRIVATE_PROMPT_BEARER_CANARY"}','{"answer":"PRIVATE_PROMPT_BEARER_CANARY"}','{}'),
     ('turn-x','check-other',1,'{"prompt":"PRIVATE_PROMPT_BEARER_CANARY"}','{"answer":"PRIVATE_PROMPT_BEARER_CANARY"}','{}')`,
 }{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 got,err:=loadQAModelEvidence(ctx,db,"allowed")
 if err!=nil{t.Fatal(err)}
 if got.SchemaVersion!=2||got.Scope!="authorised_managed_model_deployment"||got.DeploymentRef==""||
  got.DeploymentRef=="allowed"||got.DeploymentStatus!="ready"||got.ResidencyState!="stopped"{
  t.Fatalf("unexpected model projection %+v",got)
 }
 if got.CapturedSessions!=2||got.SessionsTruncated{
  t.Fatalf("unexpected session inventory %+v",got)
 }
 a,b:=got.Sessions[0],got.Sessions[1]
 if a.Status!="completed"||a.RecordedTurns!=2||a.RecordedToolProbes!=1||
   a.SessionRef=="check-b"||a.FailureDetails!="structured_failure_category_only"||
   a.LastFailureStage!="runtime_unload"||a.LastFailureCategory!="execution_failure"||
   a.FailureObservedAt==nil||*a.FailureObservedAt!=1560{
   t.Fatalf("unexpected successful session evidence %+v",a)
 }
 if b.Status!="cancelled"||b.RecordedTurns!=1||b.RecordedToolProbes!=0||
  b.SessionRef=="check-a"||b.LastFailureStage!="inference_dispatch"||
  b.LastFailureCategory!="deadline_exceeded"||b.FailureObservedAt==nil||*b.FailureObservedAt!=1175{
  t.Fatalf("unexpected cancelled session evidence %+v",b)
 }
 raw,err:=json.Marshal(got)
 if err!=nil{t.Fatal(err)}
 for _,bad:=range []string{secret,"check-a","check-b","check-other","foreign","turn-a","turn-b","turn-x",
  "placement_json","request_json","response_json","notes"}{
  if strings.Contains(string(raw),bad){t.Fatalf("private Agent Check data %s leaked: %s",bad,raw)}
 }
 _,err=loadQAModelEvidence(ctx,db,"invisible")
 if !errors.Is(err,sql.ErrNoRows){t.Fatalf("unknown deployment must not enumerate data: %v",err)}
}

func TestQAModelEvidenceIsBoundedAndHasNoInferredGPUStatus(t *testing.T){
 ctx:=context.Background()
 db:=qaOpenModelTestDB(t);defer db.Close()
 if _,err:=db.ExecContext(ctx,`INSERT INTO model_deployments VALUES('allowed','qualifying',NULL,100)`);err!=nil{t.Fatal(err)}
 for n:=1;n<=qaModelSessionCap+3;n++{
  if _,err:=db.ExecContext(ctx,`INSERT INTO model_testbed_sessions(id,deployment_id,status,started_at,completed_at,notes,placement_json)
    VALUES(?,'allowed','active',?,NULL,'my-token-secret','{"gpu_layers":9999}')`,
    "session-"+strings.Repeat("z",n),int64(n));err!=nil{t.Fatal(err)}
 }
 got,err:=loadQAModelEvidence(ctx,db,"allowed")
 if err!=nil{t.Fatal(err)}
 if got.CapturedSessions!=qaModelSessionCap||!got.SessionsTruncated||got.ResidencyState!="not_observed"{
  t.Fatalf("invalid bounded snapshot %+v",got)
 }
 for _,s:=range got.Sessions{
  if s.Status!="active"||s.EndedAt!=nil||s.RecordedTurns!=0||s.FailureDetails==""{
   t.Fatalf("fabricated observed evidence %+v",s)
  }
 }
 raw,_:=json.Marshal(got)
 for _,secret:=range []string{"my-token-secret","gpu_layers","9999","session-zz"}{
  if strings.Contains(string(raw),secret){t.Fatalf("leaked untrusted data %s",secret)}
 }
 if qaResidencyStatus("GPU: 100%")!="not_observed"||
    qaSessionStatus("cancelled: operator secret")!="unavailable"{
  t.Fatal("non-enumerated state text cannot be exported")
 }
}

// A forged observation with the right session ID but wrong deployment must
// not modify the selected model's diagnostic result.
func TestQAModelFailureEvidenceDoesNotCrossDeployment(t *testing.T){
 ctx:=context.Background()
 db:=qaOpenModelTestDB(t);defer db.Close()
 for _,q:=range []string{
  `INSERT INTO model_deployments VALUES('allowed','ready','stopped',100),('foreign','failed','failed',101)`,
  `INSERT INTO model_testbed_sessions(id,deployment_id,status,started_at,completed_at)
   VALUES('sess','allowed','cancelled',100,200)`,
  `INSERT INTO model_agentcheck_failure_observations(session_id,deployment_id,stage,category,observed_at)
   VALUES('sess','foreign','runtime_acquire','not_found',190)`,
 }{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 out,err:=loadQAModelEvidence(ctx,db,"allowed")
 if err!=nil{t.Fatal(err)}
 if len(out.Sessions)!=1||out.Sessions[0].LastFailureStage!=""||
    out.Sessions[0].FailureDetails!="not_recorded_as_structured_evidence"{
  t.Fatalf("cross-deployment diagnostic forged: %+v",out)
 }
 if qaModelFailureStage("operator-access-token")!="unavailable"||
  qaModelFailureCategory("Bearer secret")!="unknown"{
  t.Fatal("unknown machine-coded strings must never escape the allowlist")
 }
}
