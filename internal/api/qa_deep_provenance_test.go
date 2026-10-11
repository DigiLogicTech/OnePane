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

func TestQADeepProvenanceRequiresSameTaskAttemptWorkspaceAndVerifier(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"provenance.db"));if err!=nil{t.Fatal(err)}
 defer db.Close()
 statements:=[]string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT)`,
  `CREATE TABLE agent_worker_runs(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,attempt_id TEXT,worker_principal_id TEXT)`,
  `CREATE TABLE agent_worker_steps(id TEXT PRIMARY KEY,run_id TEXT,step_kind TEXT,status TEXT,result_ref TEXT,started_at INTEGER)`,
  `CREATE TABLE observations(id TEXT PRIMARY KEY,workspace_id TEXT,observation_type TEXT,subject_ref TEXT,trust TEXT)`,
  `CREATE TABLE tool_invocations(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,attempt_id TEXT,status TEXT)`,
  `CREATE TABLE operations(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,attempt_id TEXT,state TEXT)`,
  `CREATE TABLE verifications(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,status TEXT,required_level TEXT,achieved_level TEXT,operation_id TEXT,verified_by TEXT,started_at INTEGER)`,
  `CREATE TABLE assurance_runs(verification_id TEXT,workspace_id TEXT,task_id TEXT,operation_id TEXT,worker_run_id TEXT,status TEXT,evidence_hash TEXT,achieved_level TEXT)`,
  `INSERT INTO tasks VALUES('world','tenant','game','world'),('story','tenant','game','story'),
   ('outsider','other-tenant','game','world')`,
  `INSERT INTO agent_worker_runs VALUES('run-world','tenant','world','attempt-world','agent'),
   ('run-story','tenant','story','attempt-story','agent'),
   ('run-outside','other-tenant','world','attempt-world','agent')`,
  `INSERT INTO tool_invocations VALUES('tool-good','tenant','world','attempt-world','succeeded'),
   ('tool-wrong-attempt','tenant','world','bad-attempt','succeeded'),
   ('tool-story','tenant','story','attempt-story','succeeded'),
   ('tool-outside','other-tenant','world','attempt-world','succeeded')`,
  `INSERT INTO observations VALUES('ob-good','tenant','agent_tool_result','tool_invocation:tool-good','unverified_derived'),
   ('ob-wrong','tenant','agent_tool_result','tool_invocation:tool-wrong-attempt','unverified_derived'),
   ('ob-story','tenant','agent_tool_result','tool_invocation:tool-story','unverified_derived'),
   ('ob-outside','other-tenant','agent_tool_result','tool_invocation:tool-outside','unverified_derived')`,
  `INSERT INTO operations VALUES('op-good','tenant','world','attempt-world','committed'),
   ('op-story','tenant','story','attempt-story','committed'),
   ('op-outside','other-tenant','world','attempt-world','committed')`,
  `INSERT INTO agent_worker_steps VALUES
   ('step-tool','run-world','tool','succeeded','ob-good',100),
   ('step-bad-attempt','run-world','tool','succeeded','ob-wrong',110),
   ('step-tool-story','run-story','tool','succeeded','ob-story',120),
   ('step-tool-outside','run-outside','tool','succeeded','ob-outside',130),
   ('step-op','run-world','operation','succeeded','op-good',140),
   ('step-op-story','run-story','operation','succeeded','op-story',150),
   ('step-op-outside','run-outside','operation','succeeded','op-outside',160)`,
  `INSERT INTO verifications VALUES
   ('verify-good','tenant','world','pass','V1','V2','op-good','assurance-verifier',150),
   ('verify-without-evidence','tenant','world','pass','V2','V2',NULL,'assurance-verifier',140),
   ('verify-self','tenant','world','pass','V1','V1',NULL,'agent',130),
   ('verify-story','tenant','story','pass','V1','V1','op-story','assurance-verifier',120),
   ('verify-outside','other-tenant','world','pass','V1','V1','op-outside','assurance-verifier',110)`,
 }
 for _,q:=range statements{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatalf("setup %v: %s",err,q)}}
 digest:="sha256:"+strings.Repeat("a",64)
 for _,row:=range []struct{v,ten,task,op,run,status,hash,achieved string}{
  {"verify-good","tenant","world","op-good","run-world","passed",digest,"V2"},
  {"verify-without-evidence","tenant","world","","run-world","passed","","V2"},
  {"verify-self","tenant","world","","run-world","passed",digest,"V1"},
  {"verify-story","tenant","story","op-story","run-story","passed",digest,"V1"},
  {"verify-outside","other-tenant","world","op-outside","run-outside","passed",digest,"V1"},
 }{
  nullable:=func(s string)any{if s==""{return nil};return s}
  if _,err:=db.ExecContext(ctx,`INSERT INTO assurance_runs VALUES(?,?,?,?,?,?,?,?)`,
   row.v,row.ten,row.task,nullable(row.op),row.run,row.status,nullable(row.hash),row.achieved);err!=nil{t.Fatal(err)}
 }
 proj,canonical:="game","world"
 visible:=[]task.Task{{ID:"world",WorkspaceID:"tenant",ProjectID:&proj,ProjectWorkspaceID:&canonical}}
 worker,truncated,err:=loadQAWorkerToolEvidence(ctx,db,"tenant",proj,canonical,visible)
 if err!=nil||truncated||len(worker)!=2{t.Fatalf("unexpected Worker evidence: %+v truncated=%v err=%v",worker,truncated,err)}
 var hasTool,hasOp bool
 for _,e:=range worker{
  if e.TaskRef!=qaOpaqueRef("task","world")||e.RunRef!=qaOpaqueRef("run","run-world"){t.Fatalf("foreign data leaked: %+v",e)}
  if e.Source=="tool"{
   hasTool=true
   if e.TargetRef!=qaOpaqueRef("tool","tool-good")||e.ObservationRef!=qaOpaqueRef("observation","ob-good")||e.ObservationTrust!="unverified_derived"{t.Fatalf("false Tool linkage: %+v",e)}
  }
  if e.Source=="operation"{
   hasOp=true
   if e.TargetRef!=qaOpaqueRef("operation","op-good")||e.TargetStatus!="committed"{t.Fatalf("false Operation linkage: %+v",e)}
  }
 }
 if !hasTool||!hasOp{t.Fatal("no persisted journal links")}
 assurance,at,err:=loadQAAssuranceEvidence(ctx,db,"tenant",proj,canonical,visible)
 if err!=nil||at||len(assurance)!=3{t.Fatalf("unexpected assurance: %+v %v %v",assurance,at,err)}
 hasPass:=false
 for _,a:=range assurance{
  if a.TaskRef!=qaOpaqueRef("task","world"){t.Fatalf("other Task leaked %+v",a)}
  if a.VerificationRef==qaOpaqueRef("verification","verify-good"){
   hasPass=true
   if !a.RecordedAssurancePass||!a.EvidenceDigestRecorded||!a.DistinctVerifierRecorded||
    a.OperationRef!=qaOpaqueRef("operation","op-good")||a.OperationState!="committed"{
    t.Fatalf("expected qualified *recorded* assurance pass: %+v",a)
   }
  }else if a.RecordedAssurancePass{t.Fatalf("fabricated assurance on absent evidence or same verifier: %+v",a)}
 }
 if !hasPass{t.Fatal("missing durable verification record")}
 raw,_:=json.Marshal(struct{Worker []qaWorkerToolEvidence;Assurance []qaAssuranceEvidence}{worker,assurance})
 for _,canary:=range []string{"tool-good","ob-good","run-world","attempt-world","verify-good","assurance-verifier","op-good","tool-story","verify-story","other-tenant"}{
  if strings.Contains(string(raw),canary){t.Fatalf("raw persisted identity leaked %q: %s",canary,raw)}
 }
 // Forged visible Task claims cannot bypass SQL's persisted canonical scope.
 story:="story"
 forged:=[]task.Task{{ID:"story",WorkspaceID:"tenant",ProjectID:&proj,ProjectWorkspaceID:&canonical}}
 if rows,_,err:=loadQAWorkerToolEvidence(ctx,db,"tenant",proj,canonical,forged);err!=nil||len(rows)!=0{t.Fatalf("forged Worker Task leaked: %+v %v",rows,err)}
 if rows,_,err:=loadQAAssuranceEvidence(ctx,db,"tenant",proj,canonical,forged);err!=nil||len(rows)!=0{t.Fatalf("forged assurance leaked: %+v %v",rows,err)}
 _=story
}

func TestQADeepEvidenceFailsClosedIfRequiredTablesMissing(t *testing.T){
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"partial.db"));if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT)`,
  `INSERT INTO tasks VALUES('world','tenant','game','world')`,
 }{if _,err:=db.Exec(q);err!=nil{t.Fatal(err)}}
 project,world:="game","world"
 visible:=[]task.Task{{ID:"world",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world}}
 if _,_,err=loadQAWorkerToolEvidence(context.Background(),db,"tenant",project,world,visible);err==nil{t.Fatal("Worker evidence returned despite missing source tables")}
 if _,_,err=loadQAAssuranceEvidence(context.Background(),db,"tenant",project,world,visible);err==nil{t.Fatal("assurance evidence returned despite missing source tables")}
}
