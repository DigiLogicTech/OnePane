package api

import (
 "context"
 "encoding/json"
 "fmt"
 "net/http/httptest"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestTCPRecurringGrantAPIRequiresSeparateHumanConsent(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-recurring-api.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,query:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Human','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent','agent','Agent','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','agent','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,query);err!=nil{t.Fatal(err)}}
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Recurring network",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`)})
 if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Local",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"operator",
  CadenceMinutes:30,Objective:"Review service availability",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["lan"]}`)})
 if err!=nil{t.Fatal(err)}
 expiry:=time.Now().Add(time.Hour).UnixMilli()
 manual,err:=svc.ApproveGoalTCPProbe(ctx,projectworkspace.ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"lan",
  TargetIPv4:"192.168.25.9",TargetPort:443,TimeoutMS:350,
  ExpectedGoalRevision:goal.Revision,ExpiresAt:expiry,
  ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 h:=NewServer(svc,nil,goalAPIAuth{}).Handler()
 route:="/v1/projects/"+project.ID+"/continuous-goals/"+goal.ID+"/tcp-recurring-grants"
 call:=func(method,who,url,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,url,strings.NewReader(body))
  if who!=""{req.Header.Set("Authorization","Bearer "+who)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder();h.ServeHTTP(w,req);return w
 }
 listing:=call("GET","operator",route,"")
 if listing.Code!=200||strings.Contains(listing.Body.String(),manual.ID){
  t.Fatalf("manual source permit secretly authorised recurring network: %d %s",listing.Code,listing.Body)
 }
 body:=fmt.Sprintf(`{"manual_permit_id":"%s",
  "expected_goal_revision":2,"expected_manual_permit_revision":1,
  "expires_at":%d}`,manual.ID,expiry)
 for _,who:=range []string{"","outsider","agent"}{
  got:=call("POST",who,route,body)
  expect:=403;if who==""{expect=401}
  if got.Code!=expect{t.Fatalf("unauthorised recurring consent %q: %d %s",who,got.Code,got.Body)}
 }
 approved:=call("POST","operator",route,body)
 if approved.Code!=201{t.Fatalf("separate recurring grant rejected: %d %s",approved.Code,approved.Body)}
 var grant projectworkspace.TCPRecurringGrant
 if err:=json.Unmarshal(approved.Body.Bytes(),&grant);err!=nil{t.Fatal(err)}
 if grant.EffectClass!="recurring_tcp_connect"||grant.TargetIPv4!="192.168.25.9"||
  grant.CadenceMinutes!=30||grant.ManualPermitID!=manual.ID{
  t.Fatalf("recurring scope not tied to manual grant: %+v",grant)
 }
 if got:=call("POST","operator",route,body);got.Code!=409{
  t.Fatalf("stale recurring consent accepted: %d %s",got.Code,got.Body)
 }
 if got:=call("GET","outsider",route,"");got.Code!=403{
  t.Fatalf("foreign principal learned network targets: %d",got.Code)
 }
 if got:=call("GET","operator",route,"");got.Code!=200||
   !strings.Contains(got.Body.String(),grant.ID){
  t.Fatalf("recurring grant invisible to owner: %d %s",got.Code,got.Body)
 }
 stop:=route+"/"+grant.ID+"/revoke"
 revoke:=`{"expected_goal_revision":3,"expected_grant_revision":1}`
 if got:=call("POST","agent",stop,revoke);got.Code!=403{
  t.Fatalf("Agent removed human recurring grant: %d %s",got.Code,got.Body)
 }
 if got:=call("POST","operator",stop,revoke);got.Code!=200{
  t.Fatalf("human revoke rejected: %d %s",got.Code,got.Body)
 }
 if got:=call("POST","operator",stop,revoke);got.Code!=409{
  t.Fatalf("replayed revocation accepted: %d %s",got.Code,got.Body)
 }
 var tcpCalls int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM project_goal_tcp_probe_attempts").Scan(&tcpCalls);err!=nil{
  t.Fatal(err)
 }
 if tcpCalls!=0{t.Fatalf("grant API started a network probe: %d",tcpCalls)}
}
