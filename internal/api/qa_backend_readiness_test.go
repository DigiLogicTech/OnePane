package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "path/filepath"
 "strings"
 "testing"
 
 _ "modernc.org/sqlite"
)

func qaReadinessTestDB(t *testing.T)*sql.DB{
 t.Helper()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"readiness.db"))
 if err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `CREATE TABLE harness_nodes(id TEXT PRIMARY KEY,local INTEGER NOT NULL,identity_fingerprint TEXT,endpoint_json TEXT)`,
  `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT,checksum TEXT,applied_at INTEGER)`,
  `INSERT INTO harness_nodes VALUES('local',1,'PRIVATE_NODE_IDENTITY_CANARY','{"secret":"PRIVATE_CONFIG_TOKEN"}'),
 ('remote',0,'PRIVATE_NODE_IDENTITY_CANARY_2','{"secret":"PRIVATE_CONFIG_TOKEN"}')`,
 }{if _,err:=db.Exec(q);err!=nil{_ = db.Close();t.Fatal(err)}}
 return db
}

func TestQABackendReadinessRecordsOnlyOnDemandKnownState(t *testing.T){
 db:=qaReadinessTestDB(t);defer db.Close()
 embedded,ok:=qaEmbeddedMigrations()
 if !ok||len(embedded)<40{t.Fatal("expected embedded migration catalog")}
 for v:=range embedded{
  if _,err:=db.Exec(`INSERT INTO schema_migrations(version,name,checksum,applied_at) VALUES(?,'PRIVATE_MIGRATION_NAME','PRIVATE_CHECKSUM_SECRET',50)`,v);err!=nil{t.Fatal(err)}
 }
 got:=qaReadBackend(context.Background(),db,"local",true,true,false,false)
 if got.Scope!="canonical_local_backend"||got.DatabaseResponse!="responding_read_only"||
  got.SchemaRecordStatus!="recorded_versions_match_embedded"||
  got.LocalNodeRegistration!="registered"||got.TaskServiceWiring!="configured_not_probed"||
  got.VaultServiceWiring!="not_configured"||got.RecordedSchemaVersion<=0||
  got.RecordedSchemaVersion!=got.EmbeddedSchemaVersion{
  t.Fatalf("readiness must only claim actual observed state: %+v",got)
 }
 raw,err:=json.Marshal(got);if err!=nil{t.Fatal(err)}
 for _,secret:=range []string{"PRIVATE_NODE_IDENTITY_CANARY","PRIVATE_MIGRATION_NAME",
 "PRIVATE_CHECKSUM_SECRET","PRIVATE_CONFIG_TOKEN","local","remote","path_to_db","schema_migrations"}{
  if strings.Contains(string(raw),secret)&&secret!="local"{
   t.Fatalf("untrusted readiness data leaked: %s: %s",secret,raw)
  }
 }
 if _,err:=db.Exec(`DELETE FROM schema_migrations WHERE version=?`,got.RecordedSchemaVersion);err!=nil{t.Fatal(err)}
 truncated:=qaReadBackend(context.Background(),db,"local",true,true,true,true)
 if truncated.SchemaRecordStatus!="recorded_versions_incomplete_or_extra"||
   truncated.DatabaseResponse!="responding_read_only"{
  t.Fatalf("incomplete migrations incorrectly accepted: %+v",truncated)
 }
 missing:=qaReadBackend(context.Background(),db,"unregistered",true,true,true,true)
 if missing.LocalNodeRegistration!="missing"{
  t.Fatalf("missing local registration incorrectly passed: %+v",missing)
 }
}

func TestQABackendReadinessMissingAndClosedDatabaseNeverClaimReady(t *testing.T){
 absent:=qaReadBackend(context.Background(),nil,"local",true,true,false,false)
 if absent.DatabaseResponse!="unavailable"||absent.SchemaRecordStatus!="unavailable"||
  absent.LocalNodeRegistration!="not_verified"||absent.RecordedSchemaVersion!=0{
  t.Fatalf("missing database was reported ready: %+v",absent)
 }
 db:=qaReadinessTestDB(t)
 _=db.Close()
 closed:=qaReadBackend(context.Background(),db,"local",true,false,false,false)
 if closed.DatabaseResponse!="unavailable"||closed.LocalNodeRegistration!="not_verified"{
  t.Fatalf("closed database was reported ready: %+v",closed)
 }
}

func TestQABackendReadinessNeverProbesDifferentNode(t *testing.T){
 count:=0
 probe:=func(context.Context)qaBackendReadiness{count++;return qaBackendReadiness{Scope:"canonical_local_backend",DatabaseResponse:"responding_read_only"}}
 for _,tc:=range []struct{name,requested,internal string;registered bool;allowed bool}{
  {"valid local","local","local",true,true},
  {"remote node","remote","local",false,false},
  {"forged local flag","remote","local",true,false},
  {"unconfigured server ID","local","",true,false},
  {"registered remote with matching ID","local","local",false,false},
 }{
  t.Run(tc.name,func(t *testing.T){
   count=0
   report:=qaNodeEvidence{IsLocal:tc.registered}
   qaAttachBackendReadiness(context.Background(),&report,tc.requested,tc.internal,probe)
   if tc.allowed{
    if count!=1||report.BackendReadiness==nil||report.BackendReadiness.DatabaseResponse!="responding_read_only"{
     t.Fatalf("local readiness not attached: %+v, count %d",report,count)
    }
   }else if count!=0||report.BackendReadiness!=nil{
    t.Fatalf("remote Node was incorrectly probed: %+v, count %d",report,count)
   }
  })
 }
}
