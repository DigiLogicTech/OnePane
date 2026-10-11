package inference

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "testing"

 _ "modernc.org/sqlite"
)

func TestTasklessAssistantRequiresPersistedHostLocalOnly(t *testing.T){
 origin:="local-node"
 remote:="paired-node"
 provider:="free-cloud-provider"
 for _,source:=range []string{"onepane-assistant","project-orchestrator"}{
  cmd:=ExecuteCommand{DeploymentID:"deployment",ClassificationMetadata:json.RawMessage(
   `{"source":"`+source+`"}`)}
  base:=ModelDeployment{ID:"deployment",ModelID:"model",NodeID:&origin,
   Revision:1,Status:DeploymentReady}
  if err:=requireTasklessLocalDeployment(cmd,base,origin);err!=nil{t.Fatal(err)}
  for _,tc:=range []struct{name string;mutate func(*ModelDeployment)}{
   {"paired node",func(d *ModelDeployment){d.NodeID=&remote}},
   {"provider-backed even if free",func(d *ModelDeployment){d.ProviderConnectionID=&provider}},
   {"missing Node identity",func(d *ModelDeployment){d.NodeID=nil}},
   {"not ready",func(d *ModelDeployment){d.Status="unavailable"}},
  }{
   t.Run(source+" / "+tc.name,func(t *testing.T){
    selected:=base
    tc.mutate(&selected)
    if err:=requireTasklessLocalDeployment(cmd,selected,origin);!errors.Is(err,ErrInformationFlow){
     t.Fatalf("unsafe taskless transport admitted: %v",err)
    }
   })
  }
  task:="t"
  cmd.TaskID=&task
  if err:=requireTasklessLocalDeployment(cmd,base,origin);!errors.Is(err,ErrInformationFlow){
   t.Fatalf("private chat used Task identity to bypass local-only policy: %v",err)
  }
 }
}

func TestTasklessLocalTransportRechecksDeploymentAfterModelQueue(t *testing.T){
 db,err:=sql.Open("sqlite",t.TempDir()+"/locality.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if _,err=db.Exec(`CREATE TABLE model_deployments(
  id TEXT PRIMARY KEY,model_id TEXT,node_id TEXT,provider_connection_id TEXT,
  runtime_name TEXT,runtime_version TEXT,runtime_config_json TEXT,status TEXT,
  residency_state TEXT,context_max_reported INTEGER,context_max_verified INTEGER,
  deployment_fingerprint TEXT,revision INTEGER,discovered_at INTEGER,updated_at INTEGER
 )`);err!=nil{t.Fatal(err)}
 if _,err=db.Exec(`INSERT INTO model_deployments(
  id,model_id,node_id,provider_connection_id,runtime_name,runtime_version,
  runtime_config_json,status,residency_state,context_max_reported,context_max_verified,
  deployment_fingerprint,revision,discovered_at,updated_at)
  VALUES('local-model','model','local-node',NULL,'local-openai','1','{}','ready',
   NULL,NULL,NULL,'verified-local',1,1,1)`);err!=nil{t.Fatal(err)}
 svc:=&Service{repo:newSQLRepository(db),localNodeID:"local-node"}
 cmd:=ExecuteCommand{DeploymentID:"local-model",
  ClassificationMetadata:json.RawMessage(`{"source":"project-orchestrator"}`)}
 before,err:=svc.repo.Deployment(context.Background(),cmd.DeploymentID)
 if err!=nil{t.Fatal(err)}
 if err:=svc.requireTasklessLocalAtTransport(context.Background(),cmd,before);err!=nil{t.Fatal(err)}
 if _,err=db.Exec(`UPDATE model_deployments
  SET provider_connection_id='free-provider' WHERE id='local-model'`);err!=nil{t.Fatal(err)}
 if err:=svc.requireTasklessLocalAtTransport(context.Background(),cmd,before);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("provider substituted while queued: %v",err)
 }
 if _,err=db.Exec(`UPDATE model_deployments SET provider_connection_id=NULL,
  revision=revision+1 WHERE id='local-model'`);err!=nil{t.Fatal(err)}
 if err:=svc.requireTasklessLocalAtTransport(context.Background(),cmd,before);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("deployment revision changed during queue: %v",err)
 }
 if _,err=db.Exec(`UPDATE model_deployments SET node_id='paired-node' WHERE id='local-model'`);err!=nil{t.Fatal(err)}
 if err:=svc.requireTasklessLocalAtTransport(context.Background(),cmd,before);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("model moved to paired Node without transfer approval: %v",err)
 }
}
