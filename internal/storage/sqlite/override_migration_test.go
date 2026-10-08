package sqlite

import (
 "context"
 "path/filepath"
 "strings"
 "testing"
)

// The fit-level constraint must not force operators to falsify an actual
// too-tight estimate as marginal just to request an experimental install.
func TestOperatorOverrideMigrationKeepsHonestFitAndForeignKeys(t *testing.T) {
 db,err:=Open(filepath.Join(t.TempDir(),"onepane-rc3.sqlite"))
 if err!=nil {t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(context.Background());err!=nil {t.Fatalf("migrate RC3 schema: %v",err)}
 var ddl string
 if err:=db.SQL().QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='local_model_install_plans'").Scan(&ddl);err!=nil {t.Fatal(err)}
 if !strings.Contains(ddl,"'too_tight'") {t.Fatalf("RC3 plan constraint doesn't preserve too-tight estimates: %s",ddl)}
 if strings.Contains(ddl,"local_model_install_plans_rebuilt") {t.Fatal("temporary table name leaked into durable schema")}
 var n int
 if err:=db.SQL().QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=35").Scan(&n);err!=nil||n!=1 {t.Fatalf("RC3 migration missing: %d, %v",n,err)}
 rows,err:=db.SQL().Query("PRAGMA foreign_key_check")
 if err!=nil {t.Fatal(err)}
 defer rows.Close()
 if rows.Next(){t.Fatal("foreign key violation after RC3 plan migration")}
 if err:=rows.Err();err!=nil {t.Fatal(err)}
 var integrity string
 if err:=db.SQL().QueryRow("PRAGMA quick_check").Scan(&integrity);err!=nil||integrity!="ok" {t.Fatalf("post-migrate quick_check=%s: %v",integrity,err)}
}


func TestOperatorOverrideUpgradesPopulatedRC2WithoutLosingChildReferences(t *testing.T) {
 ctx:=context.Background()
 path:=filepath.Join(t.TempDir(),"populated-rc2.sqlite")
 db,err:=Open(path);if err!=nil {t.Fatal(err)}
 defer db.Close()
 if _,err=db.SQL().ExecContext(ctx,`CREATE TABLE IF NOT EXISTS schema_migrations (
 version INTEGER PRIMARY KEY,name TEXT NOT NULL,checksum TEXT NOT NULL,applied_at INTEGER NOT NULL) STRICT`);err!=nil{t.Fatal(err)}
 migrations,err:=loadMigrations();if err!=nil{t.Fatal(err)}
 for _,m:=range migrations {
  if m.version>=35{continue}
  if err=db.applyMigration(ctx,m);err!=nil {t.Fatalf("seed pre-RC3 migration %04d: %v",m.version,err)}
 }
 // A real local model plan is the missing CI fixture: the old migration
 // succeeds on empty databases but fails with child FKs on user upgrades.
 seeds:=[]string{
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,protocol_json,capabilities_json,revision,created_at,updated_at) VALUES('node','Node',1,'fp','local','{}','{}',1,1,1)`,
  `INSERT INTO local_hardware_profiles(id,node_id,fingerprint,os_name,architecture,cpu_json,memory_json,accelerators_json,runtimes_json,storage_json,detected_at) VALUES('hw','node','fp','windows','amd64','{}','{}','[]','[]','{}',1)`,
  `INSERT INTO local_model_install_plans(id,node_id,hardware_profile_id,role_name,use_case,model_ref,source_ref,runtime_name,quantization,context_tokens,fit_level,run_mode,memory_required_bytes,disk_required_bytes,download_scratch_bytes,plan_json,status,revision,created_at,updated_at) VALUES('plan','node','hw','worker','coding','Qwen/Qwen3.5-9B','hf://model','llamacpp','Q4_0',8192,'good','gpu',1024,1024,0,'{}','ready',1,1,1)`,
  `INSERT INTO managed_local_models(id,node_id,plan_id,model_ref,source_ref,local_path,status,revision,updated_at) VALUES('managed','node','plan','Qwen/Qwen3.5-9B','hf://model','D:/OnePane/Models/qwen.gguf','ready',1,1)`,
 }
 for i,q:=range seeds{
  if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatalf("seed record %d: %v",i,err)}
 }
 if err=db.Migrate(ctx);err!=nil {t.Fatalf("RC2 to RC4 migration with real references: %v",err)}
 var modelRef,fit string
 if err=db.SQL().QueryRowContext(ctx,`SELECT model_ref,fit_level FROM local_model_install_plans WHERE id='plan'`).Scan(&modelRef,&fit);err!=nil{t.Fatalf("lost plan after upgrade: %v",err)}
 if modelRef!="Qwen/Qwen3.5-9B"||fit!="good"{t.Fatalf("plan changed: %q %q",modelRef,fit)}
 var childPlan string
 if err=db.SQL().QueryRowContext(ctx,`SELECT plan_id FROM managed_local_models WHERE id='managed'`).Scan(&childPlan);err!=nil{t.Fatalf("lost managed model: %v",err)}
 if childPlan!="plan"{t.Fatalf("child relationship changed: %q",childPlan)}
 var fk int
 if err=db.SQL().QueryRowContext(ctx,`PRAGMA foreign_keys`).Scan(&fk);err!=nil||fk!=1{t.Fatalf("FK enforcement disabled after migration: %d, %v",fk,err)}
 rows,err:=db.SQL().QueryContext(ctx,`PRAGMA foreign_key_check`);if err!=nil{t.Fatal(err)}
 defer rows.Close()
 if rows.Next(){t.Fatal("foreign key integrity lost after populated upgrade")}
 if err=rows.Err();err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE local_model_install_plans SET fit_level='too_tight' WHERE id='plan'`);err!=nil{
  t.Fatalf("experimental override still cannot store honest too-tight fit: %v",err)
 }
}
