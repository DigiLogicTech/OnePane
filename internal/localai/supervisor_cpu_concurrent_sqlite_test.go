package localai

import (
 "context"
 "database/sql"
 "strings"
 "testing"
 "time"

 _ "modernc.org/sqlite"
)

func TestResidentCPUMemoryAdmissionSnapshotsSingleSQLiteConnection(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 for _,q:=range []string{
  `CREATE TABLE local_runtime_instances(deployment_id TEXT PRIMARY KEY,node_id TEXT,status TEXT,pid INTEGER)`,
  `CREATE TABLE managed_local_models(deployment_id TEXT,plan_id TEXT)`,
  `CREATE TABLE local_model_install_plans(id TEXT PRIMARY KEY,memory_required_bytes INTEGER,plan_json TEXT)`,
  `INSERT INTO local_model_install_plans VALUES
  ('cpu',2147483648,'{"placement":{"mode":"cpu_only"}}'),
  ('gpu',3221225472,'{"placement":{"mode":"single_device"}}'),
  ('bad',2147483648,'not-json')`,
  `INSERT INTO managed_local_models VALUES
  ('cpu-a','cpu'),('cpu-b','cpu'),('gpu-a','gpu'),
  ('failed-live','cpu'),('failed-exited','cpu'),('bad-plan','bad')`,
  `INSERT INTO local_runtime_instances VALUES
  ('cpu-a','local','healthy',101),('cpu-b','local','busy',102),
  ('gpu-a','local','healthy',103),
  ('failed-live','local','orphaned',104),
  ('failed-exited','local','failed',NULL),
  ('bad-plan','other-node','healthy',105)`,
 }{
  if _,err=db.Exec(q);err!=nil{t.Fatalf("fixture: %v; query=%s",err,q)}
 }
 s:=&RuntimeSupervisor{db:db}
 ctx:=context.Background()
 const gib=int64(1)<<30
 used,err:=s.estimatedResidentCPUMemory(ctx,"local","incoming")
 if err!=nil||used!=6*gib{
  t.Fatalf("expected 6 GiB including unresolved live-PID orphan, got %d: %v",used,err)
 }
 if cpuModelFitsConcurrentRAM(2*gib,8*gib,used){
  t.Fatal("aggregate RAM estimate admitted concurrent overcommit")
 }
 editCtx,cancel:=context.WithTimeout(ctx,500*time.Millisecond)
 defer cancel()
 if _,err=db.ExecContext(editCtx,`UPDATE local_runtime_instances SET status='stopped' WHERE deployment_id='cpu-b'`);err!=nil{
  t.Fatalf("CPU residency snapshot held SQLite connection: %v",err)
 }
 used,err=s.estimatedResidentCPUMemory(ctx,"local","incoming")
 if err!=nil||used!=4*gib||!cpuModelFitsConcurrentRAM(2*gib,8*gib,used){
  t.Fatalf("released model not reflected in safe fit: used=%d err=%v",used,err)
 }
 if _,err=db.Exec(`UPDATE local_runtime_instances SET node_id='local' WHERE deployment_id='bad-plan'`);err!=nil{t.Fatal(err)}
 if _,err=s.estimatedResidentCPUMemory(ctx,"local","incoming");err==nil||!strings.Contains(err.Error(),"unverified"){
  t.Fatalf("corrupt resident placement silently fabricated free RAM: %v",err)
 }
}

func TestResidentCPUMemoryOverflowOrCancellationFailsClosed(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 for _,q:=range []string{
  `CREATE TABLE local_runtime_instances(deployment_id TEXT,node_id TEXT,status TEXT,pid INTEGER)`,
  `CREATE TABLE managed_local_models(deployment_id TEXT,plan_id TEXT)`,
  `CREATE TABLE local_model_install_plans(id TEXT,memory_required_bytes INTEGER,plan_json TEXT)`,
  `INSERT INTO local_model_install_plans VALUES
  ('large',4611686018427387904,'{"placement":{"mode":"cpu_only"}}')`,
  `INSERT INTO managed_local_models VALUES ('a','large'),('b','large')`,
  `INSERT INTO local_runtime_instances VALUES ('a','local','healthy',10),('b','local','healthy',11)`,
 }{
  if _,err=db.Exec(q);err!=nil{t.Fatal(err)}
 }
 s:=&RuntimeSupervisor{db:db}
 if _,err=s.estimatedResidentCPUMemory(context.Background(),"local","incoming");err==nil||!strings.Contains(err.Error(),"overflowing"){
  t.Fatalf("resident CPU bytes wrapped into apparent headroom: %v",err)
 }
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err=s.estimatedResidentCPUMemory(ctx,"local","incoming");err==nil{
  t.Fatal("cancelled CPU admission did not fail closed")
 }
}
