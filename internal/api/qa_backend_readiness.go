package api

import (
 "context"
 "database/sql"
 "strconv"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/migrations"
)

// This is a read-only, point-in-time control-plane readiness observation,
// not a startup log, full integrity check, filesystem audit or service
// attestation. All returned fields are fixed machine-defined enums/counts.
// No SQL errors, paths, Node names, credentials or raw configuration leave.
const qaReadinessTimeout = 1500*time.Millisecond

type qaBackendReadiness struct {
 Scope string `json:"scope"`
 ObservedAtMS int64 `json:"observed_at_ms"`
 DatabaseResponse string `json:"database_response"`
 SchemaRecordStatus string `json:"schema_record_status"`
 RecordedSchemaVersion int64 `json:"recorded_schema_version,omitempty"`
 EmbeddedSchemaVersion int64 `json:"embedded_schema_version,omitempty"`
 LocalNodeRegistration string `json:"local_node_registration"`
 TaskServiceWiring string `json:"task_service_wiring"`
 ModelServiceWiring string `json:"model_service_wiring"`
 VaultServiceWiring string `json:"vault_service_wiring"`
 FederationServiceWiring string `json:"federation_service_wiring"`
 EvidenceLimitations []string `json:"evidence_limitations"`
}

func qaWired(v bool)string{
 if v{return "configured_not_probed"}
 return "not_configured"
}

// Bound the number of embedded migration filenames considered; names are
// developer-bundled, not values returned by any user-controlled request.
func qaEmbeddedMigrations()(map[int64]struct{},bool){
 entries,err:=migrations.FS.ReadDir(".")
 if err!=nil||len(entries)>256{return nil,false}
 out:=make(map[int64]struct{})
 for _,e:=range entries{
  name:=e.Name()
  if e.IsDir()||!strings.HasSuffix(name,".sql")||len(name)<9||name[4]!='_'{continue}
  n,err:=strconv.ParseInt(name[:4],10,64)
  if err!=nil||n<=0{return nil,false}
  out[n]=struct{}{}
 }
 return out,len(out)>0
}

// Unlike checking MAX(version) alone, compare *all* recorded versions with
// embedded migration version numbers. This does not recheck SQL checksums,
// schema contents or write safety: it is only a presence/coverage check.
func qaCheckMigrations(ctx context.Context,db *sql.DB)(string,int64,int64){
 embedded,ok:=qaEmbeddedMigrations()
 if !ok{return "unavailable",0,0}
 var latest int64
 for v:=range embedded{if v>latest{latest=v}}
 rows,err:=db.QueryContext(ctx,`SELECT version FROM schema_migrations ORDER BY version LIMIT 257`)
 if err!=nil{return "unavailable",0,latest}
 defer rows.Close()
 matched:=make(map[int64]struct{})
 allKnown:=true
 n:=0
 var recordedLatest int64
 for rows.Next(){
  n++
  if n>256{return "unavailable",0,latest}
  var version int64
  if rows.Scan(&version)!=nil{return "unavailable",0,latest}
  if version>recordedLatest{recordedLatest=version}
  if _,exists:=embedded[version];!exists{allKnown=false}
  matched[version]=struct{}{}
 }
 if rows.Err()!=nil{return "unavailable",0,latest}
 if allKnown&&len(matched)==len(embedded){return "recorded_versions_match_embedded",recordedLatest,latest}
 return "recorded_versions_incomplete_or_extra",recordedLatest,latest
}

func qaReadBackend(ctx context.Context,db *sql.DB,nodeID string,taskWired,modelWired,vaultWired,federationWired bool)qaBackendReadiness{
 result:=qaBackendReadiness{
  Scope:"canonical_local_backend",
  ObservedAtMS:time.Now().UTC().UnixMilli(),
  DatabaseResponse:"unavailable",
  SchemaRecordStatus:"unavailable",
  LocalNodeRegistration:"not_verified",
  TaskServiceWiring:qaWired(taskWired),
  ModelServiceWiring:qaWired(modelWired),
  VaultServiceWiring:qaWired(vaultWired),
  FederationServiceWiring:qaWired(federationWired),
  EvidenceLimitations:[]string{
   "read-only observed SQLite responsiveness and recorded migration coverage, not an integrity or write test",
   "migration version coverage does not verify migration checksums or underlying schema integrity",
   "configured components are not proof of scheduler, model, Vault or federation operational health",
   "HTTP handler is executing now, but readiness does not prove prior startup success or future availability",
   "no installer logs, OS journals, sensitive configuration, raw database errors or file paths are exported",
  },
 }
 if db==nil||nodeID==""{return result}
 deadline,cancel:=context.WithTimeout(ctx,qaReadinessTimeout)
 defer cancel()
 if db.PingContext(deadline)!=nil{return result}
 result.DatabaseResponse="responding_read_only"
 result.SchemaRecordStatus,result.RecordedSchemaVersion,result.EmbeddedSchemaVersion=qaCheckMigrations(deadline,db)
 var registered int
 if err:=db.QueryRowContext(deadline,`SELECT EXISTS(
 SELECT 1 FROM harness_nodes WHERE id=? AND local=1)`,nodeID).Scan(&registered);err==nil{
  if registered==1{result.LocalNodeRegistration="registered"}
  if registered==0{result.LocalNodeRegistration="missing"}
 }
 return result
}

// The matching check must be made against BOTH the persisted local flag
// and the server's internal Node identity, never only a browser parameter.
func qaAttachBackendReadiness(ctx context.Context,report *qaNodeEvidence,requestedID,serverLocalID string,
 probe func(context.Context)qaBackendReadiness){
 if report==nil||!report.IsLocal||requestedID==""||requestedID!=serverLocalID||probe==nil{return}
 result:=probe(ctx)
 report.BackendReadiness=&result
}
