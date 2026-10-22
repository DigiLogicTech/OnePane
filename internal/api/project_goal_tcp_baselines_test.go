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

func TestTCPBaselineAPIRequiresHumanAndNeverClaimsVerifiedDeviceHealth(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-baseline-http.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,stmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Worker','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,stmt);err!=nil{t.Fatal(err)}}
 service:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=service.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Baseline",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=service.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Local scope",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 goal,err:=service.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:ws.ID,
  ActorPrincipalID:"operator",Objective:"Compare approved TCP evidence only",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["dns"]}`),
 })
 if err!=nil{t.Fatal(err)}
 permit,err:=service.ApproveGoalTCPProbe(ctx,projectworkspace.ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"dns",
  TargetIPv4:"10.50.1.9",TargetPort:443,TimeoutMS:500,
  ExpiresAt:time.Now().Add(time.Hour).UnixMilli(),
  ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 h:=NewServer(service,nil,goalAPIAuth{}).Handler()
 base:="/v1/projects/"+project.ID+"/continuous-goals/"+goal.ID
 route:=base+"/tcp-baselines"
 call:=func(method,who,url,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,url,strings.NewReader(body))
  if who!=""{req.Header.Set("Authorization","Bearer "+who)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder();h.ServeHTTP(w,req);return w
 }
 assessmentURL:=base+"/tcp-assessment/dns"
 if got:=call("GET","operator",assessmentURL,"");got.Code!=200||
   !strings.Contains(got.Body.String(),`"assessment":"unconfigured_baseline"`){
  t.Fatalf("unconfigured read incorrect: %d %s",got.Code,got.Body)
 }
 body:=fmt.Sprintf(`{"permit_id":"%s","expected_tcp":"connected",
  "expected_goal_revision":2,"expected_baseline_revision":0}`,permit.ID)
 for _,who:=range []string{"","outsider","robot"}{
  w:=call("POST",who,route,body)
  want:=403;if who==""{want=401}
  if w.Code!=want{t.Fatalf("unauthorised baseline %q got %d: %s",who,w.Code,w.Body)}
 }
 if got:=call("POST","operator",route,strings.Replace(body,"connected","healthy",1));
   got.Code!=400{t.Fatalf("API admitted false health baseline: %d %s",got.Code,got.Body)}
 approved:=call("POST","operator",route,body)
 if approved.Code!=201{t.Fatalf("TCP baseline denied: %d %s",approved.Code,approved.Body)}
 var baseline projectworkspace.GoalTCPBaselineRevision
 if err:=json.Unmarshal(approved.Body.Bytes(),&baseline);err!=nil{t.Fatal(err)}
 if baseline.ExpectedTCP!="connected"||baseline.Revision!=1{
  t.Fatalf("unexpected approved baseline: %+v",baseline)
 }
 if got:=call("POST","operator",route,body);got.Code!=409{
  t.Fatalf("stale baseline revision accepted: %d %s",got.Code,got.Body)
 }
 if got:=call("GET","outsider",assessmentURL,"");got.Code!=403{
  t.Fatalf("foreign baseline disclosure: %d",got.Code)
 }
 missing:=call("GET","operator",assessmentURL,"")
 if missing.Code!=200||!strings.Contains(missing.Body.String(),`"assessment":"missing_evidence"`)||
   !strings.Contains(missing.Body.String(),`"device_health":"unverified"`)||
   !strings.Contains(missing.Body.String(),`"monitoring_active":false`){
  t.Fatalf("missing sensor evidence reported healthy: %d %s",missing.Code,missing.Body)
 }
 listed:=call("GET","operator",route,"")
 if listed.Code!=200||!strings.Contains(listed.Body.String(),baseline.ID){
  t.Fatalf("TCP baseline audit list unavailable: %d %s",listed.Code,listed.Body)
 }
 current,err:=service.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if current.Health!="unobserved"||current.LastVerifiedAt!=0||current.NextDueAt!=0{
  t.Fatalf("baseline mutated security health: %+v",current)
 }
 var taskCount,observedCount int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&taskCount);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM observations").Scan(&observedCount);err!=nil{t.Fatal(err)}
 if taskCount!=0||observedCount!=0{t.Fatalf("baseline started monitoring: tasks=%d observations=%d",taskCount,observedCount)}
}
