package localai

import(
 "context"
 "database/sql"
 "testing"
 "time"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 _ "modernc.org/sqlite"
)

func newReaperIsolationDB(t *testing.T)*sql.DB{
 t.Helper()
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 db.SetMaxOpenConns(1)
 for _,q:=range []string{
  `CREATE TABLE model_deployments(id TEXT PRIMARY KEY,runtime_config_json TEXT)`,
  `CREATE TABLE local_runtime_instances(
   id TEXT PRIMARY KEY, node_id TEXT,deployment_id TEXT,
   runtime_id TEXT,managed_model_id TEXT,bind_address TEXT,port INTEGER,pid INTEGER,
   status TEXT,process_fingerprint TEXT,launch_json TEXT,health_json TEXT,
   started_at INTEGER,last_seen_at INTEGER,stopped_at INTEGER,failure_reason TEXT,
   revision INTEGER,created_at INTEGER,updated_at INTEGER)`,
  `INSERT INTO model_deployments VALUES
   ('ordinary','{"colibri_pinned":false}'),('pinned','{"colibri_pinned":true}')`,
  `INSERT INTO local_runtime_instances VALUES
   ('inst-one','node','ordinary','runtime','model','127.0.0.1',11001,42,
    'healthy','fp','{}','{}',1,1,NULL,NULL,1,1,1),
   ('inst-pin','node','pinned','runtime','model','127.0.0.1',11002,43,
    'healthy','fp','{}','{}',1,1,NULL,NULL,1,1,1)`,
 }{
  if _,err=db.Exec(q);err!=nil{t.Fatal(err)}
 }
 return db
}

func TestIdleReaperClosesSQLiteCursorAndPreservesActiveModels(t *testing.T){
 db:=newReaperIsolationDB(t)
 defer db.Close()
 s:=&RuntimeSupervisor{db:db,clock:clock.Real{},activeRequests:map[string]int{"ordinary":1}}
 ctx:=context.Background()
 cutoff:=time.Now().UnixMilli()-1000
 ids,err:=s.idleReaperCandidates(ctx,cutoff,10)
 if err!=nil||len(ids)!=1||ids[0]!="ordinary"{
  t.Fatalf("reaper selected wrong idle model: %v %v",ids,err)
 }
 editCtx,cancel:=context.WithTimeout(ctx,time.Second)
 defer cancel()
 if _,err=db.ExecContext(editCtx,`UPDATE local_runtime_instances
  SET updated_at=updated_at+1 WHERE id='inst-one'`);err!=nil{
  t.Fatalf("candidate reader retained SQLite connection: %v",err)
 }
 stopped,err:=s.ReapIdle(ctx,time.Second,10)
 if err!=nil||stopped!=0{
  t.Fatalf("reaper evicted a model with active inference: count=%d err=%v",stopped,err)
 }
 result,err:=s.stopIfIdleBefore(ctx,"ordinary",cutoff)
 if err!=nil||result{
  t.Fatalf("busy inference escaped final stop guard: %v %v",result,err)
 }
 delete(s.activeRequests,"ordinary")
 result,err=s.stopIfIdleBefore(ctx,"pinned",cutoff)
 if err!=nil||result{
  t.Fatalf("pinned model could be stopped by automatic idle cleanup: %v %v",result,err)
 }
 if _,err=db.ExecContext(ctx,`UPDATE local_runtime_instances SET
  updated_at=? WHERE id='inst-one'`,cutoff+10);err!=nil{t.Fatal(err)}
 result,err=s.stopIfIdleBefore(ctx,"ordinary",cutoff)
 if err!=nil||result{
  t.Fatalf("recently-used model was stopped after stale candidate selection: %v %v",result,err)
 }
 var state string
 if err=db.QueryRowContext(ctx,`SELECT status FROM local_runtime_instances
  WHERE id='inst-one'`).Scan(&state);err!=nil||state!="healthy"{
  t.Fatalf("safe veto still mutated process residency: %s %v",state,err)
 }
}
