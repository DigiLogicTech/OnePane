package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/task"
 _ "modernc.org/sqlite"
)

func qaAsyncFixture(t *testing.T)(*sql.DB,[]task.Task){
 t.Helper()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"async.db"));if err!=nil{t.Fatal(err)}
 queries:=[]string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT)`,
  `CREATE TABLE task_attempts(id TEXT PRIMARY KEY,task_id TEXT,status TEXT,started_at INTEGER)`,
  `CREATE TABLE agent_worker_runs(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,attempt_id TEXT,status TEXT,step_count INTEGER,started_at INTEGER,worker_principal_id TEXT)`,
  `CREATE TABLE verifications(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT,status TEXT,required_level TEXT,achieved_level TEXT,operation_id TEXT,started_at INTEGER,verified_by TEXT)`,
  `CREATE TABLE assurance_runs(verification_id TEXT,workspace_id TEXT,task_id TEXT,operation_id TEXT,worker_run_id TEXT,status TEXT,evidence_hash TEXT,achieved_level TEXT,result_json TEXT)`,
  `CREATE TABLE events(workspace_id TEXT,aggregate_type TEXT,aggregate_id TEXT,event_type TEXT,occurred_at INTEGER)`,
  `CREATE TABLE observations(id TEXT PRIMARY KEY,workspace_id TEXT,subject_ref TEXT,observation_type TEXT,probe_tool_id TEXT,probe_tool_version TEXT,
   source_principal_id TEXT,adapter_id TEXT,adapter_version TEXT,value_json TEXT,confidentiality TEXT,residency TEXT,trust TEXT,
   origin_node_id TEXT,integrity_hash TEXT,observed_at INTEGER,created_at INTEGER)`,
  `INSERT INTO tasks VALUES('task-world','tenant','game','world'),('task-story','tenant','game','story'),
   ('task-other','different','game','world')`,
  `INSERT INTO task_attempts VALUES('attempt-world','task-world','running',200),
   ('attempt-story','task-story','succeeded',300),('attempt-other','task-other','running',300),
   ('wrong-attempt','task-story','running',100)`,
  `INSERT INTO agent_worker_runs VALUES('run-world','tenant','task-world','attempt-world','running',4,230,'agent-worker'),
   ('run-story','tenant','task-story','attempt-story','succeeded',9,300,'agent-worker'),
   ('run-other','different','task-other','attempt-other','running',1,300,'agent-worker'),
   ('run-forged','tenant','task-world','wrong-attempt','running',1,201,'agent-worker')`,
  `INSERT INTO verifications VALUES
   ('verify-world','tenant','task-world','pass','V2','V3',NULL,400,'assurance-verifier'),
   ('verify-story','tenant','task-story','pass','V2','V2',NULL,405,'assurance-verifier'),
   ('verify-other','different','task-other','pass','V2','V2',NULL,405,'assurance-verifier')`,
 }
 for _,q:=range queries{if _,err:=db.Exec(q);err!=nil{t.Fatalf("fixture: %v (%s)",err,q)}}
 type probe struct{id,source,workspace,subject string;observed int64;badHash bool}
 ps:=[]probe{
  {"obs-good","independent-probe","tenant","task-world",240,false},
  {"obs-stale","independent-probe","tenant","task-world",100,false},
  {"obs-tampered","independent-probe","tenant","task-world",250,true},
  {"obs-self","agent-worker","tenant","task-world",260,false},
  {"obs-story","independent-probe","tenant","task-story",310,false},
  {"obs-other","independent-probe","different","task-other",310,false},
 }
 for _,p:=range ps{
  label:=policy.DataLabel{WorkspaceID:p.workspace,Confidentiality:policy.ConfidentialityInternal,
   Residency:policy.ResidencyAny,Trust:policy.TrustUnverifiedDerived}
  source:=p.source
  ob:=observation.Observation{ID:p.id,WorkspaceID:p.workspace,SubjectRef:p.subject,
   ObservationType:"external_state_probe",ProbeToolID:"independent.readback",
   ProbeToolVersion:"v1",SourcePrincipalID:&source,
   Value:json.RawMessage(`{"observed":true,"secret":"DONOTEXPOSE"}`),
   Label:label,ObservedAt:p.observed,CreatedAt:p.observed}
  hash,err:=observation.IntegrityHash(ob);if err!=nil{t.Fatal(err)}
  if p.badHash{hash="sha256:"+strings.Repeat("0",64)}
  if _,err:=db.Exec(`INSERT INTO observations VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
   p.id,p.workspace,p.subject,ob.ObservationType,ob.ProbeToolID,ob.ProbeToolVersion,p.source,
   nil,nil,string(ob.Value),"internal","any","unverified_derived",nil,hash,p.observed,p.observed);err!=nil{t.Fatal(err)}
 }
 digest:="sha256:"+strings.Repeat("a",64)
 result:=func(ids ...string)string{
  rows:=make([]map[string]any,0,len(ids))
  for _,id:=range ids{rows=append(rows,map[string]any{
   "role":"integration","status":"pass","observation_id":id})}
  b,_:=json.Marshal(map[string]any{"observations":rows});return string(b)
 }
 for _,x:=range []struct{verification,ten,task,run string;result string}{
  {"verify-world","tenant","task-world","run-world",result("obs-good","obs-stale","obs-tampered","obs-self","obs-other")},
  {"verify-story","tenant","task-story","run-story",result("obs-story")},
  {"verify-other","different","task-other","run-other",result("obs-other")},
 }{
  if _,err:=db.Exec(`INSERT INTO assurance_runs VALUES(?,?,?,?,?,?,?,?,?)`,
   x.verification,x.ten,x.task,nil,x.run,"passed",digest,"V3",x.result);err!=nil{t.Fatal(err)}
 }
 project,world:="game","world"
 return db,[]task.Task{{ID:"task-world",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world}}
}

func TestQAAsyncLineageAndRehashedIndependentWitnesses(t *testing.T){
 db,visible:=qaAsyncFixture(t);defer db.Close()
 // Regression: probe integrity rechecks cannot deadlock a single-connection SQLite pool.
 db.SetMaxOpenConns(1)
 ctx:=context.Background()
 chains,truncated,err:=loadQAAsyncLineage(ctx,db,"tenant","game","world",visible)
 if err!=nil||truncated||len(chains)!=1{t.Fatalf("lineage: %+v truncated=%v err=%v",chains,truncated,err)}
 chain:=chains[0]
 if chain.TaskRef!=qaOpaqueRef("task","task-world")||chain.AttemptRef!=qaOpaqueRef("attempt","attempt-world")||
  chain.WorkerRunRef!=qaOpaqueRef("run","run-world")||chain.StepsRecorded!=4||
  chain.WorkerState!="running"||chain.LinkKind!="persisted_task_attempt_worker_foreign_keys"{
  t.Fatalf("wrong durable linkage: %+v",chain)
 }
 witnesses,truncated,err:=loadQAProbeWitnesses(ctx,db,"tenant","game","world",visible)
 if err!=nil||truncated||len(witnesses)!=4{t.Fatalf("witness query: %+v truncated=%v err=%v",witnesses,truncated,err)}
 byID:=map[string]qaProbeWitness{}
 for _,e:=range witnesses{for _,id:=range []string{"obs-good","obs-stale","obs-tampered","obs-self"}{
  if e.ObservationRef==qaOpaqueRef("observation",id){byID[id]=e}
 }}
 good:=byID["obs-good"]
 if !good.CorroboratingIntegrationProbe||!good.IntegrityRechecked||!good.IndependentSource||
  !good.ObservedAfterAttempt||!good.RecordedAssurancePass{t.Fatalf("valid source was rejected: %+v",good)}
 for _,id:=range []string{"obs-stale","obs-tampered","obs-self"}{
  if byID[id].CorroboratingIntegrationProbe{t.Fatalf("false independent witness: %s %+v",id,byID[id])}
 }
 if byID["obs-stale"].ObservedAfterAttempt{t.Fatal("stale observation marked fresh")}
 if byID["obs-tampered"].IntegrityRechecked{t.Fatal("tampered observation hash accepted")}
 if byID["obs-self"].IndependentSource{t.Fatal("self-observation marked independently sourced")}
 b,_:=json.Marshal(struct{Runs []qaAsyncLineage;Probes []qaProbeWitness}{chains,witnesses})
 for _,secret:=range []string{"DONOTEXPOSE","task-world","attempt-world","run-world","obs-good",
  "independent-probe","agent-worker","external_state_probe","verify-world"}{
  if strings.Contains(string(b),secret){t.Fatalf("raw identifier or private probe content leaked (%q): %s",secret,b)}
 }
 // User-provided Task objects cannot invent canonical Workspace membership.
 forgedProject,forgedWorkspace:="game","world"
 forged:=[]task.Task{{ID:"task-story",WorkspaceID:"tenant",ProjectID:&forgedProject,ProjectWorkspaceID:&forgedWorkspace}}
 if got,_,err:=loadQAAsyncLineage(ctx,db,"tenant","game","world",forged);err!=nil||len(got)!=0{
  t.Fatalf("forged async lineage: %+v %v",got,err)
 }
 if got,_,err:=loadQAProbeWitnesses(ctx,db,"tenant","game","world",forged);err!=nil||len(got)!=0{
  t.Fatalf("forged probe link: %+v %v",got,err)
 }
 if got,_,err:=loadQAProbeWitnesses(ctx,db,"tenant","other","world",visible);err!=nil||len(got)!=0{
  t.Fatalf("cross-project probe link: %+v %v",got,err)
 }
}

func TestQAProbeWitnessesMissingSourcesFailClosed(t *testing.T){
 db,visible:=qaAsyncFixture(t);defer db.Close()
 if _,err:=db.Exec(`DROP TABLE observations`);err!=nil{t.Fatal(err)}
 _,_,err:=loadQAProbeWitnesses(context.Background(),db,"tenant","game","world",visible)
 if err==nil{t.Fatal("silently emitted a partial probe report with missing observation store")}
}

func TestQAAsyncLineageReturnsExplicitTruncation(t *testing.T){
 db,visible:=qaAsyncFixture(t);defer db.Close()
 for i:=0;i<26;i++{
  attempt:=fmt.Sprintf("attempt-%02d",i)
  run:=fmt.Sprintf("run-%02d",i)
  if _,err:=db.Exec(`INSERT INTO task_attempts VALUES(?,'task-world','succeeded',?)`,attempt,300+i);err!=nil{t.Fatal(err)}
  if _,err:=db.Exec(`INSERT INTO agent_worker_runs VALUES(?,'tenant','task-world',?,'succeeded',1,?,'agent-worker')`,
   run,attempt,300+i);err!=nil{t.Fatal(err)}
 }
 got,truncated,err:=loadQAAsyncLineage(context.Background(),db,"tenant","game","world",visible)
 if err!=nil||!truncated||len(got)!=24{t.Fatalf("bounded lineage: len=%d truncated=%v err=%v",len(got),truncated,err)}
}

func TestQAProbeWitnessRequiresVerifierSeparationAndOperationExecutionTime(t *testing.T){
 db,visible:=qaAsyncFixture(t);defer db.Close()
 ctx:=context.Background()
 if _,err:=db.Exec(`UPDATE verifications SET verified_by='agent-worker' WHERE id='verify-world'`);err!=nil{t.Fatal(err)}
 witnesses,_,err:=loadQAProbeWitnesses(ctx,db,"tenant","game","world",visible)
 if err!=nil{t.Fatal(err)}
 for _,w:=range witnesses{
  if w.RecordedAssurancePass||w.CorroboratingIntegrationProbe{
   t.Fatalf("worker self-verification was treated as independent assurance: %+v",w)
  }
 }
 if _,err:=db.Exec(`UPDATE verifications SET verified_by='assurance-verifier',operation_id='op-not-observed' WHERE id='verify-world'`);err!=nil{t.Fatal(err)}
 if _,err:=db.Exec(`UPDATE assurance_runs SET operation_id='op-not-observed' WHERE verification_id='verify-world'`);err!=nil{t.Fatal(err)}
 witnesses,_,err=loadQAProbeWitnesses(ctx,db,"tenant","game","world",visible)
 if err!=nil{t.Fatal(err)}
 for _,w:=range witnesses{
  if w.ObservedAfterOperationStart||w.CorroboratingIntegrationProbe{
   t.Fatalf("operation without a persisted execution marker was accepted: %+v",w)
  }
 }
 if _,err:=db.Exec(`INSERT INTO events VALUES('tenant','operation','op-not-observed','operation.executing',220)`);err!=nil{t.Fatal(err)}
 witnesses,_,err=loadQAProbeWitnesses(ctx,db,"tenant","game","world",visible)
 if err!=nil{t.Fatal(err)}
 var matched bool
 for _,w:=range witnesses{
  if w.ObservationRef==qaOpaqueRef("observation","obs-good"){
   matched=true
   if !w.ObservedAfterOperationStart||!w.CorroboratingIntegrationProbe{
    t.Fatalf("fresh independently sourced observation wrongly failed operation timestamp check: %+v",w)
   }
  }
 }
 if !matched{t.Fatal("missing intended matched observation")}
}
