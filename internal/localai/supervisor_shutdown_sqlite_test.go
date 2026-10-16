package localai

import(
 "context"
 "database/sql"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 _ "modernc.org/sqlite"
)

// A one-connection SQLite pool exposes the old reader-versus-Stop deadlock.
// PID 0 means both managed model processes are independently already gone;
// Shutdown must persist stopped residency without signalling anything.
func TestShutdownClosesReadCursorBeforeModelStateChanges(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 for _,q:=range []string{
  `CREATE TABLE model_deployments(
   id TEXT PRIMARY KEY,runtime_name TEXT,residency_state TEXT,
   updated_at INTEGER,revision INTEGER)`,
  `CREATE TABLE local_runtime_instances(
   id TEXT PRIMARY KEY,node_id TEXT,deployment_id TEXT,
   runtime_id TEXT,managed_model_id TEXT,bind_address TEXT,
   port INTEGER,pid INTEGER,status TEXT,process_fingerprint TEXT,
   launch_json TEXT,health_json TEXT,started_at INTEGER,
   last_seen_at INTEGER,stopped_at INTEGER,failure_reason TEXT,
   revision INTEGER,created_at INTEGER,updated_at INTEGER)`,
  `INSERT INTO model_deployments VALUES
   ('model-a','llamacpp@cpu','healthy',1,1),
   ('model-b','colibri','healthy',1,1)`,
  `INSERT INTO local_runtime_instances
   (id,node_id,deployment_id,runtime_id,managed_model_id,
    bind_address,port,pid,status,process_fingerprint,
    launch_json,health_json,revision,created_at,updated_at)
   VALUES
   ('instance-a','node','model-a','runtime','model','127.0.0.1',11001,0,'healthy','fp','{}','{}',1,1,1),
   ('instance-b','node','model-b','runtime','model','127.0.0.1',11002,0,'healthy','fp','{}','{}',1,1,1)`,
 }{
  if _,err=db.Exec(q);err!=nil{t.Fatal(err)}
 }
 s:=&RuntimeSupervisor{db:db,clock:clock.Real{}}
 ctx,cancel:=context.WithTimeout(context.Background(),time.Second)
 defer cancel()
 if err:=s.Shutdown(ctx);err!=nil{
  t.Fatalf("shutdown blocked on its own single SQLite reader: %v",err)
 }
 var stopped int
 if err=db.QueryRow(`SELECT COUNT(*) FROM local_runtime_instances
  WHERE status='stopped' AND stopped_at IS NOT NULL`).Scan(&stopped);
  err!=nil||stopped!=2{
  t.Fatalf("managed model processes failed safe shutdown: %d %v",stopped,err)
 }
 var synced int
 if err=db.QueryRow(`SELECT COUNT(*) FROM model_deployments
  WHERE residency_state='stopped'`).Scan(&synced);
  err!=nil||synced!=2{
  t.Fatalf("deployment statuses not synchronized: %d %v",synced,err)
 }
}

func TestGPUEvictionClosesReadCursorBeforeAnyStop(t *testing.T){
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 source:=string(raw)
 start:=strings.Index(source,"func (s *RuntimeSupervisor) ensureCapacity(")
 if start<0{t.Fatal("managed placement admission missing")}
 scope:=source[start:]
 if end:=strings.Index(scope,"\nfunc ");end>0{scope=scope[:end]}
 query:=strings.Index(scope,"rows, err := s.db.QueryContext(")
 close:=strings.Index(scope,"if err := rows.Close(); err != nil {")
 stop:=strings.Index(scope,"s.Stop(ctx, x.DeploymentID)")
 if query<0||close<query||stop<close{
  t.Fatal("GPU eviction may execute while a SQLite residency reader is open")
 }
}
