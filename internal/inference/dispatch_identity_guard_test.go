package inference

import (
 "context"
 "database/sql"
 "errors"
 "testing"

 _ "modernc.org/sqlite"
)
func identityGuardFixture(t *testing.T, remote,provider bool)(*Service,ModelDeployment,Model,*ProviderConnection){
 t.Helper()
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",t.TempDir()+"/identity.db")
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){db.Close()})
 for _,q:=range []string{
  `CREATE TABLE models(id TEXT PRIMARY KEY,provider_name TEXT,model_ref TEXT,architecture TEXT,
   revision_ref TEXT,weights_hash TEXT,quantization TEXT,modalities_json TEXT,static_metadata_json TEXT,
   trust_state TEXT,created_at INTEGER,updated_at INTEGER)`,
  `CREATE TABLE model_deployments(id TEXT PRIMARY KEY,model_id TEXT,node_id TEXT,
   provider_connection_id TEXT,runtime_name TEXT,runtime_version TEXT,runtime_config_json TEXT,
   status TEXT,residency_state TEXT,context_max_reported INTEGER,context_max_verified INTEGER,
   deployment_fingerprint TEXT,revision INTEGER,discovered_at INTEGER,updated_at INTEGER)`,
  `CREATE TABLE harness_nodes(id TEXT PRIMARY KEY,trust_state TEXT)`,
  `CREATE TABLE provider_connections(id TEXT PRIMARY KEY,workspace_id TEXT,provider TEXT,
   display_name TEXT,auth_type TEXT,secret_ref TEXT,status TEXT,connection_json TEXT,
   retry_after INTEGER,revision INTEGER,created_at INTEGER,updated_at INTEGER)`,
  `INSERT INTO models VALUES('m',NULL,'qwen-local',NULL,NULL,NULL,NULL,'{}','{}','trusted',1,1)`,
  `INSERT INTO harness_nodes VALUES('remote','paired')`,
  `INSERT INTO provider_connections VALUES('provider',NULL,'cloud','Cloud','api_key','old-secret',
   'connected','{}',NULL,1,1,1)`,
 }{if _,err=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 nodeID:="local"
 if remote{nodeID="remote"}
 var providerID any
 if provider{providerID="provider"}
 if _,err=db.ExecContext(ctx,`INSERT INTO model_deployments VALUES(
  'd','m',?,?,'openai','1','{}','ready',NULL,NULL,NULL,'fingerprint',1,1,1)`,
  nodeID,providerID);err!=nil{t.Fatal(err)}
 s:=&Service{repo:newSQLRepository(db),localNodeID:"local"}
 d,err:=s.repo.Deployment(ctx,"d");if err!=nil{t.Fatal(err)}
 m,err:=s.repo.Model(ctx,"m");if err!=nil{t.Fatal(err)}
 var p *ProviderConnection
 if provider{
  v,e:=s.repo.Provider(ctx,"provider");if e!=nil{t.Fatal(e)}
  p=&v
 }
 if err:=s.requireCurrentDispatchIdentity(ctx,d,m,p);err!=nil{
  t.Fatalf("fixture with unchanged route should be allowed: %v",err)
 }
 return s,d,m,p
}
func TestInferenceLastDispatchBlocksModelAndDeploymentSwaps(t *testing.T){
 cases:=[]struct{name,query string}{
  {"new provider association",`UPDATE model_deployments SET provider_connection_id='provider' WHERE id='d'`},
  {"new Node",`UPDATE model_deployments SET node_id='remote' WHERE id='d'`},
  {"new model weights",`UPDATE models SET weights_hash='replacement' WHERE id='m'`},
  {"model revoked",`UPDATE models SET trust_state='revoked' WHERE id='m'`},
  {"new runtime endpoint",`UPDATE model_deployments SET runtime_config_json='{"base_url":"https://bad.example"}' WHERE id='d'`},
  {"runtime version",`UPDATE model_deployments SET runtime_version='2' WHERE id='d'`},
  {"fingerprint changed",`UPDATE model_deployments SET deployment_fingerprint='other' WHERE id='d'`},
  {"revision changed",`UPDATE model_deployments SET revision=2 WHERE id='d'`},
  {"deployment disabled",`UPDATE model_deployments SET status='disabled' WHERE id='d'`},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   svc,d,m,p:=identityGuardFixture(t,false,false)
   db:=svc.repo.(*sqlRepository).db
   if _,err:=db.ExecContext(context.Background(),tc.query);err!=nil{t.Fatal(err)}
   if err:=svc.requireCurrentDispatchIdentity(context.Background(),d,m,p);!errors.Is(err,ErrInformationFlow){
    t.Fatalf("inference route changed without a hard denial: %v",err)
   }
  })
 }
}
func TestInferenceLastDispatchBlocksProviderAndPairedNodeTrustRevocation(t *testing.T){
 for _,tc:=range []struct{name,statement string}{
  {"provider credential changed",`UPDATE provider_connections SET secret_ref='other-secret' WHERE id='provider'`},
  {"provider endpoint changed",`UPDATE provider_connections SET connection_json='{"endpoint":"https://different.example"}' WHERE id='provider'`},
  {"provider revoked",`UPDATE provider_connections SET status='revoked' WHERE id='provider'`},
  {"provider revision changed",`UPDATE provider_connections SET revision=2 WHERE id='provider'`},
 }{
  t.Run(tc.name,func(t *testing.T){
   svc,d,m,p:=identityGuardFixture(t,false,true)
   if _,err:=svc.repo.(*sqlRepository).db.ExecContext(context.Background(),tc.statement);err!=nil{t.Fatal(err)}
   if err:=svc.requireCurrentDispatchIdentity(context.Background(),d,m,p);!errors.Is(err,ErrInformationFlow){
    t.Fatalf("revoked provider still admitted: %v",err)
   }
  })
 }
 svc,d,m,p:=identityGuardFixture(t,true,false)
 if _,err:=svc.repo.(*sqlRepository).db.ExecContext(context.Background(),
  `UPDATE harness_nodes SET trust_state='revoked' WHERE id='remote'`);err!=nil{t.Fatal(err)}
 if err:=svc.requireCurrentDispatchIdentity(context.Background(),d,m,p);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("unpaired Node still admitted: %v",err)
 }
}
