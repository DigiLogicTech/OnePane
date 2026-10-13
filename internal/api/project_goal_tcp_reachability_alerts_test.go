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

func TestTCPReachabilityAlertAPIIsScopedAndHumanAcknowledgedOnly(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-notice-api.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Robot','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"TCP drift",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 w,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"LAN",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:w.ID,ActorPrincipalID:"operator",
  Objective:"Manual TCP observation",CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["svc"]}`)})
 if err!=nil{t.Fatal(err)}
 permit,err:=svc.ApproveGoalTCPProbe(ctx,projectworkspace.ApproveGoalTCPProbeCommand{
  ProjectID:p.ID,GoalID:goal.ID,SourceID:"svc",
  TargetIPv4:"192.168.10.11",TargetPort:443,TimeoutMS:500,
  ExpiresAt:time.Now().Add(time.Hour).UnixMilli(),
  ExpectedGoalRevision:goal.Revision,ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 baseline,err:=svc.SetGoalTCPBaseline(ctx,projectworkspace.SetGoalTCPBaselineCommand{
  ProjectID:p.ID,GoalID:goal.ID,PermitID:permit.ID,
  ExpectedGoalRevision:goal.Revision,ExpectedBaselineRevision:0,
  ExpectedTCP:"not_connected",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 now:=time.Now().UnixMilli()
 // Inject already-audited fixture data; HTTP tests must never touch a
 // live LAN service. Domain integration independently verifies generation.
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_tcp_probe_attempts(
  id,project_id,goal_id,permit_id,project_workspace_id,source_id,
  target_ipv4,target_port,timeout_ms,cadence_window,goal_revision,
  permit_revision,requested_by,status,claimed_at,completed_at,
  recurring_grant_id,baseline_id_at_claim,baseline_revision_at_claim)
  VALUES('test-tcp',?,?,?,?,?,'192.168.10.11',443,500,?,
   ?,1,'operator','connected',?,?,NULL,?,1)`,
  p.ID,goal.ID,permit.ID,w.ID,"svc",now/(30*60000),
  goal.Revision,now,now,baseline.ID)
 if err!=nil{t.Fatal(err)}
 _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_tcp_reachability_alerts(
  id,project_id,goal_id,project_workspace_id,source_id,baseline_id,
  baseline_revision,attempt_id,expected_tcp,observed_tcp,classification,
  status,revision,detected_by,detected_at)
  VALUES('test-notice',?,?,?,?,?,1,'test-tcp',
   'not_connected','connected','unexpected_reachable','open',1,'operator',?)`,
  p.ID,goal.ID,w.ID,"svc",baseline.ID,now)
 if err!=nil{t.Fatal(err)}
 h:=NewServer(svc,nil,goalAPIAuth{}).Handler()
 route:="/v1/projects/"+p.ID+"/continuous-goals/"+goal.ID+"/tcp-alerts"
 call:=func(method,who,url,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,url,strings.NewReader(body))
  if who!=""{req.Header.Set("Authorization","Bearer "+who)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder();h.ServeHTTP(w,req)
  return w
 }
 for _,who:=range []string{"","outsider"}{
  got:=call("GET",who,route,"")
  expected:=403;if who==""{expected=401}
  if got.Code!=expected{t.Fatalf("unauthorised TCP alert list %q: %d",who,got.Code)}
 }
 listed:=call("GET","operator",route,"")
 if listed.Code!=200||!strings.Contains(listed.Body.String(),"test-notice")||
   !strings.Contains(listed.Body.String(),`"device_health":"unverified"`){
  t.Fatalf("drift notice not properly scoped: %d %s",listed.Code,listed.Body)
 }
 ackRoute:=route+"/test-notice/acknowledge"
 for _,who:=range []string{"","outsider","robot"}{
  got:=call("POST",who,ackRoute,`{"expected_revision":1}`)
  expected:=403;if who==""{expected=401}
  if got.Code!=expected{
   t.Fatalf("unauthorised TCP warning acknowledgement %q: %d %s",who,got.Code,got.Body)
  }
 }
 acknowledged:=call("POST","operator",ackRoute,`{"expected_revision":1}`)
 if acknowledged.Code!=200||
   !strings.Contains(acknowledged.Body.String(),`"status":"acknowledged"`){
  t.Fatalf("human acknowledgement denied: %d %s",acknowledged.Code,acknowledged.Body)
 }
 if got:=call("POST","operator",ackRoute,`{"expected_revision":1}`);
  got.Code!=409{t.Fatalf("alert acknowledgement CAS replay accepted: %d",got.Code)}
 // An incident review is deliberately human-owned and does not inherit
 // the alert's acknowledgement as an execution or health grant.
 caseRoute:="/v1/projects/"+p.ID+"/continuous-goals/"+goal.ID+"/incident-reviews"
 if got:=call("GET","robot",caseRoute,"");got.Code!=403{
  t.Fatalf("Agent listed human incident cases: %d",got.Code)
 }
 if got:=call("POST","robot",caseRoute,
  `{"tcp_alert_id":"test-notice","expected_alert_revision":2}`);got.Code!=403{
  t.Fatalf("Agent opened incident case: %d",got.Code)
 }
 opened:=call("POST","operator",caseRoute,
  `{"tcp_alert_id":"test-notice","expected_alert_revision":2}`)
 if opened.Code!=201||!strings.Contains(opened.Body.String(),
  `"status":"pending_review"`)||
  !strings.Contains(opened.Body.String(),`"device_health":"unverified"`){
  t.Fatalf("human could not open scoped case: %d %s",opened.Code,opened.Body)
 }
 if got:=call("POST","operator",caseRoute,
  `{"tcp_alert_id":"test-notice","expected_alert_revision":2}`);got.Code<400{
  t.Fatalf("duplicate case accepted: %d %s",got.Code,got.Body)
 }
 var reviewID string
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT id FROM project_goal_incident_reviews WHERE tcp_alert_id='test-notice'").
  Scan(&reviewID);err!=nil{t.Fatal(err)}
 reviewURL:=caseRoute+"/"+reviewID+"/review"
 if got:=call("POST","robot",reviewURL,
  `{"decision":"needs_investigation","expected_revision":1}`);got.Code!=403{
  t.Fatalf("Agent reviewed case: %d",got.Code)
 }
 reviewed:=call("POST","operator",reviewURL,
  `{"decision":"needs_investigation","expected_revision":1}`)
 if reviewed.Code!=200||!strings.Contains(reviewed.Body.String(),
  `"status":"reviewed"`)||
  !strings.Contains(reviewed.Body.String(),`"remediation_authorized":false`){
  t.Fatalf("human review failed: %d %s",reviewed.Code,reviewed.Body)
 }
 if got:=call("POST","operator",reviewURL,
  `{"decision":"needs_investigation","expected_revision":1}`);got.Code!=409{
  t.Fatalf("stale incident review CAS replay succeeded: %d",got.Code)
 }
 if listed:=call("GET","operator",caseRoute,"");listed.Code!=200||
  !strings.Contains(listed.Body.String(),reviewID){
  t.Fatalf("review history inaccessible: %d %s",listed.Code,listed.Body)
 }

 var tasks int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);err!=nil||tasks!=0{
  t.Fatal(fmt.Sprintf("TCP notice unexpectedly initiated Tasks: %d %v",tasks,err))
 }
}
