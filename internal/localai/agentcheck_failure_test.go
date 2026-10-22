package localai

import (
 "context"
 "database/sql"
 "errors"
 "io/fs"
 "os"
 "runtime"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/migrations"
 _ "modernc.org/sqlite"
)

func TestTypedAgentCheckFailureCategoriesAreNotFreeText(t *testing.T){
 tests:=[]struct{stage string;cause error;want string}{
  {"inference_dispatch",errors.New("Bearer PRIVATE_TOKEN_IN_EXCEPTION"),"execution_failure"},
  {"runtime_acquire",context.DeadlineExceeded,"deadline_exceeded"},
  {"inference_dispatch",context.Canceled,"cancelled"},
  {"deployment_read",sql.ErrNoRows,"not_found"},
  {"model_read",fs.ErrNotExist,"not_found"},
  {"response_validation",errors.New("raw SECRET_TOKEN"),"invalid_response"},
  {"completion_persist",errors.New("database locked with SECRET_TOKEN"),"storage_failure"},
  {"session_abort",errors.New("secret-laden user abort: SECRET_TOKEN"),"abort_requested"},
  {"completion_validation",errors.New("opaque"),"unknown"},
 }
 for _,tc:=range tests{
  if got:=agentCheckCategory(tc.stage,tc.cause);got!=tc.want{t.Errorf("%s: got %s, want %s",tc.stage,got,tc.want)}
 }
 if knownAgentCheckStage("runtime_token_bearer") {t.Fatal("untrusted machine stage was accepted")}
}

func TestTypedAgentCheckFailureJournalIsImmutableAndRedacted(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"typed-failures.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,q:=range []string{
  `PRAGMA foreign_keys=ON`,
  `CREATE TABLE model_deployments(id TEXT PRIMARY KEY)`,
  `CREATE TABLE model_testbed_sessions(id TEXT PRIMARY KEY,deployment_id TEXT REFERENCES model_deployments(id))`,
  `INSERT INTO model_deployments VALUES('allowed'),('foreign')`,
  `INSERT INTO model_testbed_sessions VALUES('s-allowed','allowed'),('s-foreign','foreign')`,
 }{
  if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 migration,err:=migrations.FS.ReadFile("0041_agentcheck_failure_observations.sql")
 if err!=nil{t.Fatal(err)}
 if _,err:=db.ExecContext(ctx,string(migration));err!=nil{t.Fatalf("real additive migration invalid: %v",err)}
 svc:=&Service{db:db,clock:clock.Real{}}
 sess:=TestbedSession{ID:"s-allowed",DeploymentID:"allowed"}
 secret:="PRIVATE_BEARER_very_sensitive"
 svc.recordAgentCheckFailure(ctx,sess,"inference_dispatch",errors.New(secret))
 svc.recordAgentCheckFailure(ctx,sess,"runtime_release",context.DeadlineExceeded)
 svc.recordAgentCheckFailure(ctx,sess,"session_abort",errors.New(secret))
 svc.recordAgentCheckFailure(ctx,sess,"stage="+secret,errors.New(secret))
 svc.recordAgentCheckFailure(ctx,sess,"model_read",nil)
 var count int
 if err:=db.QueryRowContext(ctx,`SELECT COUNT(*) FROM model_agentcheck_failure_observations WHERE session_id='s-allowed'`).Scan(&count);err!=nil{t.Fatal(err)}
 if count!=3{t.Fatalf("unknown/nil failures must be excluded, got %d",count)}
 rows,err:=db.QueryContext(ctx,`SELECT stage,category FROM model_agentcheck_failure_observations WHERE session_id='s-allowed' ORDER BY id`)
 if err!=nil{t.Fatal(err)}
 defer rows.Close()
 expected:=[]string{"inference_dispatch|execution_failure","runtime_release|deadline_exceeded","session_abort|abort_requested"}
 for i:=0;rows.Next();i++{
  var stage,category string
  if err:=rows.Scan(&stage,&category);err!=nil{t.Fatal(err)}
  if i>=len(expected)||stage+"|"+category!=expected[i]{t.Fatalf("unexpected typed observation %d: %s / %s",i,stage,category)}
 }
 if err:=rows.Err();err!=nil{t.Fatal(err)}
 _=rows.Close()
 var all string
 if err:=db.QueryRowContext(ctx,`SELECT group_concat(stage||category) FROM model_agentcheck_failure_observations`).Scan(&all);err!=nil{t.Fatal(err)}
 if strings.Contains(all,secret){t.Fatal("raw error content leaked into journal")}
 if _,err:=db.ExecContext(ctx,`UPDATE model_agentcheck_failure_observations SET category='unknown' WHERE session_id='s-allowed'`);err==nil{
  t.Fatal("typed failure journal must be immutable")
 }
 if _,err:=db.ExecContext(ctx,`DELETE FROM model_agentcheck_failure_observations`);err==nil{
  t.Fatal("typed failure journal must not be deletable")
 }
 if _,err:=db.ExecContext(ctx,`INSERT INTO model_agentcheck_failure_observations(session_id,deployment_id,stage,category,observed_at)
 VALUES('s-foreign','foreign','Bearer','unknown',100)`);err==nil{
  t.Fatal("enum check must reject untrusted stage")
 }
}

func TestLegacyAgentCheckErrorMigrationSelectivelyRedacts(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"legacy-manual-qa.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 stmts:=[]string{
  `CREATE TABLE model_spec_sheets(deployment_id TEXT PRIMARY KEY,qualification_json TEXT NOT NULL CHECK(json_valid(qualification_json)),revision INTEGER NOT NULL,updated_at INTEGER NOT NULL)`,
  `CREATE TABLE model_identity_specs(model_uid TEXT PRIMARY KEY,qualification_json TEXT NOT NULL CHECK(json_valid(qualification_json)),updated_at INTEGER NOT NULL)`,
 }
 for _,q:=range stmts{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 const secret="PRIVATE_BEARER_FROM_OLD_AGENT_CHECK_123"
 v2:=`{"profile_version":"onepane.manual-agent-check/v2","status":"blocked","evidence":{"errors":["`+secret+`"],"plain_ok":false,"infrastructure_error":true},"session_id":"legacy"}`
 // Qualifying data from other profiles must not be rewritten by this narrowly
 // scoped privacy migration. Removing their fields could lose independent
 // experiment results which the user has not approved deleting.
 other:=`{"profile_version":"other.profile/v1","evidence":{"errors":["keep-source-owned"]}}`
 if _,err:=db.ExecContext(ctx,`INSERT INTO model_spec_sheets VALUES('legacy',?,7,42),('other',?,9,54)`,v2,other);err!=nil{t.Fatal(err)}
 nested:=`{"profile_version":"onepane.agent-check/v1","completed":true,"qualified_at":100,"qualification":`+v2+`}`
 if _,err:=db.ExecContext(ctx,`INSERT INTO model_identity_specs VALUES('legacy-model',?,70),('unrelated','{"qualification":{"profile_version":"other/v1","evidence":{"errors":["untouched"]}}}',80)`,nested);err!=nil{t.Fatal(err)}
 migration,err:=migrations.FS.ReadFile("0042_redact_legacy_agentcheck_errors.sql")
 if err!=nil{t.Fatal(err)}
 apply:=func(){if _,err:=db.ExecContext(ctx,string(migration));err!=nil{t.Fatal(err)}}
 apply()
 var cleaned string
 var revision int
 if err:=db.QueryRowContext(ctx,`SELECT qualification_json,revision FROM model_spec_sheets WHERE deployment_id='legacy'`).Scan(&cleaned,&revision);err!=nil{t.Fatal(err)}
 if strings.Contains(cleaned,secret)||strings.Contains(cleaned,`"errors"`)||revision!=8||
  !strings.Contains(cleaned,`"plain_ok":false`)||!strings.Contains(cleaned,"redacted_on_upgrade"){
  t.Fatalf("historical spec incorrectly redacted: revision=%d payload=%s",revision,cleaned)
 }
 if err:=db.QueryRowContext(ctx,`SELECT qualification_json FROM model_identity_specs WHERE model_uid='legacy-model'`).Scan(&cleaned);err!=nil{t.Fatal(err)}
 if strings.Contains(cleaned,secret)||strings.Contains(cleaned,`"errors"`)||
  !strings.Contains(cleaned,`"qualified_at":100`)||!strings.Contains(cleaned,"redacted_on_upgrade"){
  t.Fatalf("historical identity snapshot incorrectly redacted: %s",cleaned)
 }
 var untouched string
 var revisionOther int
 if err:=db.QueryRowContext(ctx,`SELECT qualification_json,revision FROM model_spec_sheets WHERE deployment_id='other'`).Scan(&untouched,&revisionOther);err!=nil{t.Fatal(err)}
 if untouched!=other||revisionOther!=9{t.Fatal("unrelated research qualification altered")}
 if err:=db.QueryRowContext(ctx,`SELECT qualification_json FROM model_identity_specs WHERE model_uid='unrelated'`).Scan(&untouched);err!=nil{t.Fatal(err)}
 if !strings.Contains(untouched,"untouched"){t.Fatal("unrelated identity qualification altered")}
 // Migration re-execution must be safe and must not churn revision values.
 apply()
 if err:=db.QueryRowContext(ctx,`SELECT revision FROM model_spec_sheets WHERE deployment_id='legacy'`).Scan(&revision);err!=nil{t.Fatal(err)}
 if revision!=8{t.Fatalf("redaction migration must be idempotent, revision=%d",revision)}
}


func TestRuntimeLaunchFailureNeverEchoesRawLogOrUnsafePaths(t *testing.T){
 dataDir:=t.TempDir()
 logs:=filepath.Join(dataDir,"components","logs")
 if err:=os.MkdirAll(logs,0o700);err!=nil{t.Fatal(err)}
 secret:="Bearer VERY_PRIVATE_LOCAL_MODEL_TOKEN"
 logPath:=filepath.Join(logs,"local-runtime-safe-id.log")
 if err:=os.WriteFile(logPath,[]byte(secret+"\n/home/private/model/path"),0o600);err!=nil{
  t.Fatal(err)
 }
 supervisor:=&RuntimeSupervisor{dataDir:dataDir}
 hint:=supervisor.runtimeLogReviewHint("safe-id")
 if hint==""||strings.Contains(hint,secret)||
  strings.Contains(hint,"/home/private")||strings.Contains(hint,"log tail:"){
  t.Fatalf("model launch error leaked private local log: %q",hint)
 }
 for _,id:=range []string{"../private","safe/id","safe\\id",".","",strings.Repeat("x",129)}{
  if got:=supervisor.runtimeLogReviewHint(id);got!=""{
   t.Fatalf("unsafe runtime ID %q found a local log: %q",id,got)
  }
 }
 if got:=supervisor.runtimeLogReviewHint("no-log");got!=""{
  t.Fatalf("nonexistent log was falsely advertised: %s",got)
 }
 if err:=os.Remove(logPath);err!=nil{t.Fatal(err)}
 external:=filepath.Join(t.TempDir(),"external.log")
 if err:=os.WriteFile(external,[]byte(secret),0o600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(external,logPath);err==nil{
  if got:=supervisor.runtimeLogReviewHint("safe-id");got!=""{
   t.Fatalf("linked external logs were accepted: %s",got)
  }
 }
}


func TestManagedRuntimeLogNeverClobbersOrFollowsPrivateFiles(t *testing.T){
 base:=t.TempDir()
 p:=filepath.Join(base,"secure","managed.log")
 first,err:=openManagedRuntimeLog(p)
 if err!=nil{t.Fatal(err)}
 secret:="PRIVATE_DIAGNOSTIC_DO_NOT_TRUNCATE"
 if _,err=first.WriteString(secret);err!=nil{t.Fatal(err)}
 if err=first.Close();err!=nil{t.Fatal(err)}
 second,err:=openManagedRuntimeLog(p)
 if err!=nil{t.Fatal(err)}
 if _,err=second.WriteString(" next attempt");err!=nil{t.Fatal(err)}
 if err=second.Close();err!=nil{t.Fatal(err)}
 contents,err:=os.ReadFile(p)
 if err!=nil||!strings.HasPrefix(string(contents),secret)||
  !strings.HasSuffix(string(contents)," next attempt"){
  t.Fatalf("log restart discarded original private diagnostics: %v",err)
 }
 if runtime.GOOS!="windows"{
  external:=filepath.Join(t.TempDir(),"external-secret")
  if err=os.WriteFile(external,[]byte(secret),0o600);err!=nil{t.Fatal(err)}
  if err=os.Remove(p);err!=nil{t.Fatal(err)}
  if err=os.Symlink(external,p);err!=nil{t.Fatal(err)}
  if file,e:=openManagedRuntimeLog(p);e==nil{
   _=file.Close();t.Fatal("runtime followed external symlink with write access")
  }
  preserved,e:=os.ReadFile(external)
  if e!=nil||string(preserved)!=secret{t.Fatal("external secret was mutated via runtime log")}
  if err=os.Remove(p);err!=nil{t.Fatal(err)}
  if err=os.WriteFile(p,[]byte(secret),0o644);err!=nil{t.Fatal(err)}
  if file,e:=openManagedRuntimeLog(p);e==nil{
   _=file.Close();t.Fatal("shared-readable managed logs were admitted")
  }
 }
}
