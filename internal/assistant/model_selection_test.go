package assistant

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type assistantModelScheduler struct {
 options []scheduler.Candidate
 last scheduler.RouteRequest
}
func(m *assistantModelScheduler) Candidates(_ context.Context,_,_,_ string)([]scheduler.Candidate,error){
 return m.options,nil
}
func(m *assistantModelScheduler) Route(_ context.Context,req scheduler.RouteRequest)(scheduler.Decision,error){
 m.last=req
 return scheduler.Decision{},scheduler.ErrNoEligibleCandidate
}

func TestAssistantThreadModelChoicePersistsAndPinsRouting(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/onepane.db");if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 seed:=[]string{
  `INSERT INTO workspaces(id,name,status,created_at,updated_at) VALUES('ws','Workspace','active',1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Administrator','active',1,1,1)`,
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,protocol_json,capabilities_json,created_at,updated_at) VALUES('node','Local',1,'fp','local','{}','{}',1,1)`,
  `INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES('model','test/phi','["text"]','{}','user_trusted',1,1)`,
  `INSERT INTO model_deployments(id,model_id,node_id,runtime_config_json,status,deployment_fingerprint,discovered_at,updated_at) VALUES('deployment','model','node','{}','ready','fp',1,1)`,
 }
 for _,query:=range seed{if _,err:=db.SQL().ExecContext(ctx,query);err!=nil{t.Fatal(err)}}
 fake:=&assistantModelScheduler{options:[]scheduler.Candidate{
  {ID:"deployment",Kind:scheduler.CandidateModel,Schedulable:true,Status:"ready"},
 }}
 svc:=NewService(db.SQL(),clock.Real{},fake,nil,nil,nil)
 thread,err:=svc.CreateThread(ctx,"ws","OnePane Assistant","admin");if err!=nil{t.Fatal(err)}
 if thread.PreferredModelDeploymentID!=nil{t.Fatal("new Assistant should use Auto")}
 chosen:="deployment"
 updated,err:=svc.SetModel(ctx,thread.ID,&chosen);if err!=nil{t.Fatal(err)}
 if updated.PreferredModelDeploymentID==nil||*updated.PreferredModelDeploymentID!=chosen{t.Fatalf("pin not stored: %+v",updated)}
 rows,err:=svc.Threads(ctx,"ws",10);if err!=nil{t.Fatal(err)}
 if len(rows)!=1||rows[0].PreferredModelDeploymentID==nil||*rows[0].PreferredModelDeploymentID!=chosen{t.Fatal("pin was not restored from thread inventory")}
 _,_,err=svc.reasonGlobal(ctx,"ws","admin","hello",updated.PreferredModelDeploymentID)
 if err==nil||!strings.Contains(err.Error(),"no substitution"){t.Fatalf("expected a hard failure on pinned model: %v",err)}
 if len(fake.last.IncludeCandidateIDs)!=1||fake.last.IncludeCandidateIDs[0]!=chosen{t.Fatalf("pinned route did not constrain deployment: %+v",fake.last)}
 if fake.last.ContextTokens!=2048{t.Fatalf("expected bounded manual context, got %d",fake.last.ContextTokens)}
 fake.options[0].Schedulable=false
 if _,err=svc.SetModel(ctx,thread.ID,&chosen);!errors.Is(err,ErrInvalid){t.Fatalf("inactive deployment accepted: %v",err)}
 reset,err:=svc.SetModel(ctx,thread.ID,nil);if err!=nil{t.Fatal(err)}
 if reset.PreferredModelDeploymentID!=nil{t.Fatalf("Auto did not remove pin: %+v",reset)}
}
