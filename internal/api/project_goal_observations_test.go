package api

import (
 "context"
 "encoding/json"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestL5EvidenceHTTPIsAuthorisedMetadataOnlyAndDoesNotActivateMonitoring(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/goal-evidence-api.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Human','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Worker','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,stmt);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Network health",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Read-only observations",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=projects.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"operator",
  Objective:"Observe approved host status",CadenceMinutes:60,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["uptime"]}`),
 })
 if err!=nil{t.Fatal(err)}
 actor:="operator"
 obs,err:=observation.NewService(db.SQL(),db,clock.Real{}).Record(ctx,observation.RecordCommand{
  WorkspaceID:"tenant",SubjectRef:"host://secret-device",
  ObservationType:"health.sample",ProbeToolID:"uptime",ProbeToolVersion:"1",
  SourcePrincipalID:&actor,ActorPrincipalID:&actor,
  Value:json.RawMessage(`{"secret_payload":"RAW_VALUE_MUST_NOT_LEAK"}`),
  Label:policy.DataLabel{WorkspaceID:"tenant",
    Confidentiality:policy.ConfidentialityInternal,
    Residency:policy.ResidencyAny,Trust:policy.TrustAuthoritativeData},
 })
 if err!=nil{t.Fatal(err)}
 server:=NewServer(projects,nil,goalAPIAuth{})
 route:="/v1/projects/"+p.ID+"/continuous-goals/"+goal.ID+"/observations"
 call:=func(method,token,path,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,path,strings.NewReader(body))
  if token!=""{req.Header.Set("Authorization","Bearer "+token)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder();server.Handler().ServeHTTP(w,req);return w
 }
 attach:=`{"observation_id":"`+obs.ID+`","expected_revision":1}`
 if got:=call("GET","",route,"");got.Code!=401{t.Fatalf("missing auth %d",got.Code)}
 if got:=call("GET","outsider",route,"");got.Code!=403{t.Fatalf("outsider read %d",got.Code)}
 if got:=call("POST","outsider",route,attach);got.Code!=403{t.Fatalf("outsider write %d",got.Code)}
 if got:=call("POST","robot",route,attach);got.Code!=403{t.Fatalf("agent attestation %d %s",got.Code,got.Body)}
 if got:=call("GET","operator",route,"");got.Code!=200||!strings.Contains(got.Body.String(),"[]"){
  t.Fatalf("initial evidence list mismatch %d %s",got.Code,got.Body)
 }
 coverageRoute:="/v1/projects/"+p.ID+"/continuous-goals/"+goal.ID+"/coverage"
 if got:=call("GET","outsider",coverageRoute,"");got.Code!=403{
  t.Fatalf("unauthorised evidence coverage read %d",got.Code)
 }
 if got:=call("GET","operator",coverageRoute,"");
   got.Code!=200||!strings.Contains(got.Body.String(),`"evidence_state":"missing"`){
  t.Fatalf("missing coverage mismatch %d %s",got.Code,got.Body.String())
 }
 created:=call("POST","operator",route,attach)
 if created.Code!=201{t.Fatalf("evidence attestation failed %d %s",created.Code,created.Body)}
 var receipt projectworkspace.GoalObservationReceipt
 if err:=json.Unmarshal(created.Body.Bytes(),&receipt);err!=nil{t.Fatal(err)}
 if receipt.ObservationID!=obs.ID||receipt.IntegrityHash!=obs.IntegrityHash{
  t.Fatalf("incorrect receipt %+v",receipt)
 }
 coverage:=call("GET","operator",coverageRoute,"")
 if coverage.Code!=200||!strings.Contains(coverage.Body.String(),`"evidence_state":"recent_attached"`)||
  !strings.Contains(coverage.Body.String(),`"monitoring_active":false`){
  t.Fatalf("coverage incorrectly claimed monitoring %d %s",coverage.Code,coverage.Body.String())
 }
 if strings.Contains(coverage.Body.String(),"RAW_VALUE_MUST_NOT_LEAK")||
   strings.Contains(coverage.Body.String(),"host://secret-device"){
  t.Fatal("coverage API leaked raw observation")
 }
 listed:=call("GET","operator",route,"")
 if listed.Code!=200||!strings.Contains(listed.Body.String(),receipt.ID){
  t.Fatalf("evidence not listed %d %s",listed.Code,listed.Body)
 }
 for _,body:=range []string{created.Body.String(),listed.Body.String()}{
  if strings.Contains(body,"RAW_VALUE_MUST_NOT_LEAK")||
    strings.Contains(body,"host://secret-device")||
    strings.Contains(body,"secret_payload"){
   t.Fatal("API leaked raw observation values or subject")
  }
 }
 if got:=call("POST","operator",route,attach);got.Code!=409{
  t.Fatalf("stale evidence CAS unexpectedly passed %d %s",got.Code,got.Body)
 }
 wrongGoal:=strings.Replace(route,goal.ID,"nonexistent",1)
 if got:=call("GET","operator",wrongGoal,"");got.Code!=404{
  t.Fatalf("unknown goal read %d %s",got.Code,got.Body)
 }
 current,err:=projects.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if current.Status!="configured"||current.Health!="unobserved"||
  current.NextDueAt!=0||current.LastVerifiedAt!=0{
  t.Fatalf("evidence receipt activated or verified protection: %+v",current)
 }
}
