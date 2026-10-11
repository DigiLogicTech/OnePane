package localai

import (
 "context"
 "database/sql"
 "encoding/json"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/migrations"
 _ "modernc.org/sqlite"
)

func colibriPinTestDB(t *testing.T)*sql.DB{
 t.Helper()
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 db.SetMaxOpenConns(1)
 for _,q:=range []string{
  `CREATE TABLE model_deployments(id TEXT PRIMARY KEY,node_id TEXT,runtime_name TEXT,runtime_config_json TEXT,updated_at INTEGER,revision INTEGER)`,
  `CREATE TABLE managed_local_models(deployment_id TEXT,status TEXT)`,
  `INSERT INTO model_deployments VALUES
    ('colibri-a','node-1','colibri','{"managed":true,"runtime_backend":"colibri","colibri_tier":{"mode":"manual","ram_gb":8}}',1,1),
    ('colibri-b','node-1','colibri','{"managed":true,"runtime_backend":"colibri"}',1,1),
    ('colibri-remote','node-2','colibri','{"runtime_backend":"colibri"}',1,1),
    ('llama','node-1','llamacpp','{"runtime_backend":"cuda"}',1,1),
    ('fake','node-1','colibri','{"runtime_backend":"llamacpp"}',1,1),
    ('removed','node-1','colibri','{"runtime_backend":"colibri"}',1,1)`,
  `INSERT INTO managed_local_models VALUES ('colibri-a','ready'),('colibri-b','ready'),('colibri-remote','ready'),('llama','ready'),('fake','ready'),('removed','removed')`,
 }{
  if _,err=db.Exec(q);err!=nil{t.Fatalf("setup: %v query=%s",err,q)}
 }
 migration,err:=migrations.FS.ReadFile("0043_colibri_model_pin.sql")
 if err!=nil{t.Fatal(err)}
 if _,err=db.Exec(string(migration));err!=nil{t.Fatal(err)}
 return db
}

func TestColibriPinPersistentNativeOnlyAndNodeExclusive(t *testing.T){
 db:=colibriPinTestDB(t);defer db.Close()
 svc:=&Service{db:db,clock:clock.Real{}}
 ctx:=context.Background()
 initial,err:=svc.ColibriPin(ctx,"colibri-a")
 if err!=nil||initial.Pinned||!initial.Eligible{t.Fatalf("initial state=%+v err=%v",initial,err)}
 pinned,err:=svc.SetColibriPin(ctx,"colibri-a",true)
 if err!=nil||!pinned.Pinned{t.Fatalf("pin failed: %+v %v",pinned,err)}
 if _,err:=svc.SetColibriPin(ctx,"colibri-b",true);err==nil||!strings.Contains(err.Error(),"another Colibri model"){
  t.Fatalf("node pin conflict should fail closed: %v",err)
 }
 for _,id:=range []string{"llama","fake","removed"}{
  if _,err:=svc.SetColibriPin(ctx,id,true);err==nil{t.Fatalf("ineligible deployment pinned: %s",id)}
 }
 remote,err:=svc.SetColibriPin(ctx,"colibri-remote",true)
 if err!=nil||!remote.Pinned{t.Fatalf("independent Node should allow pin: %+v %v",remote,err)}
 var raw string
 if err:=db.QueryRow(`SELECT runtime_config_json FROM model_deployments WHERE id='colibri-a'`).Scan(&raw);err!=nil{t.Fatal(err)}
 var cfg map[string]any
 if err:=json.Unmarshal([]byte(raw),&cfg);err!=nil{t.Fatal(err)}
 if cfg["colibri_pinned"]!=true||cfg["managed"]!=true{
  t.Fatalf("other runtime config fields lost: %+v",cfg)
 }
 tier,ok:=cfg["colibri_tier"].(map[string]any)
 if !ok||tier["ram_gb"]!=float64(8){t.Fatalf("existing tier settings changed: %+v",cfg)}
 // A new service instance must observe the saved flag without a local
 // process/session preference, including after a simulated service restart.
 restarted:=&Service{db:db,clock:clock.Real{}}
 state,err:=restarted.ColibriPin(ctx,"colibri-a")
 if err!=nil||!state.Pinned{t.Fatalf("pin was not durable: %+v %v",state,err)}
 undone,err:=restarted.SetColibriPin(ctx,"colibri-a",false)
 if err!=nil||undone.Pinned{t.Fatalf("unpin failed: %+v %v",undone,err)}
 replacement,err:=restarted.SetColibriPin(ctx,"colibri-b",true)
 if err!=nil||!replacement.Pinned{t.Fatalf("unpin did not release Node slot: %+v %v",replacement,err)}
}

func TestPinnedColibriIgnoredByIdleReaper(t *testing.T){
 db:=colibriPinTestDB(t);defer db.Close()
 svc:=&Service{db:db,clock:clock.Real{}}
 _,err:=svc.SetColibriPin(context.Background(),"colibri-a",true)
 if err!=nil{t.Fatal(err)}
 _,err=db.Exec(`CREATE TABLE local_runtime_instances(deployment_id TEXT,status TEXT,last_seen_at INTEGER,started_at INTEGER,updated_at INTEGER)`)
 if err!=nil{t.Fatal(err)}
 _,err=db.Exec(`INSERT INTO local_runtime_instances VALUES('colibri-a','healthy',1,1,1)`)
 if err!=nil{t.Fatal(err)}
 supervisor:=&RuntimeSupervisor{db:db,clock:clock.Real{}}
 stopped,err:=supervisor.ReapIdle(context.Background(),time.Second,4)
 if err!=nil||stopped!=0{t.Fatalf("idle reaper should not stop a pinned model: count=%d err=%v",stopped,err)}
}

func TestPinnedColibriRequiresExplicitUnpinBeforeSwap(t *testing.T){
 _,err:=selectColibriEvictions([]colibriSwapCandidate{{
  ID:"pinned",Pinned:true,Status:RuntimeHealthy,
 }},map[string]int{})
 if err==nil||!strings.Contains(err.Error(),"pinned"){t.Fatalf("pinned model evicted: %v",err)}
}
