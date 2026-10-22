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

func TestDeclaredTCPTargetIsHumanOnlyAndNeverRetroactiveDeviceAttestation(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/goal-target-binding.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('robot','agent','Robot','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','robot','active',1,1)`,
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Exact human declared TCP target",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 view,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Target",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:project.ID,ProjectWorkspaceID:view.ID,ActorPrincipalID:"operator",
  Objective:"Operator assigned but not physical attested",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 permit,err:=svc.ApproveGoalTCPProbe(ctx,projectworkspace.ApproveGoalTCPProbeCommand{
  ProjectID:project.ID,GoalID:goal.ID,SourceID:"router",
  TargetIPv4:"192.168.42.21",TargetPort:443,TimeoutMS:700,
  ActorPrincipalID:"operator",ExpiresAt:now+30*60000,
  ExpectedGoalRevision:goal.Revision,
 })
 if err!=nil{t.Fatal(err)}
 base:="/v1/projects/"+project.ID+"/continuous-goals/"+goal.ID
 route:=base+"/tcp-declared-targets"
 handler:=NewServer(svc,nil,goalAPIAuth{}).Handler()
 call:=func(method,who,path,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,path,strings.NewReader(body))
  if who!=""{req.Header.Set("Authorization","Bearer "+who)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder()
  handler.ServeHTTP(w,req)
  return w
 }
 sample:=func(id string,window int64,observedAt int64){
  t.Helper()
  _,err=db.SQL().ExecContext(ctx,`INSERT INTO project_goal_tcp_probe_attempts(
   id,project_id,goal_id,permit_id,project_workspace_id,source_id,
   target_ipv4,target_port,timeout_ms,cadence_window,goal_revision,
   permit_revision,requested_by,status,claimed_at,completed_at)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'not_connected',?,?)`,
   id,project.ID,goal.ID,permit.ID,view.ID,"router",
   permit.TargetIPv4,permit.TargetPort,permit.TimeoutMS,
   window,goal.Revision,permit.Revision,"operator",observedAt,observedAt)
  if err!=nil{t.Fatal(err)}
 }
 observedBeforeBinding:=clock.Real{}.UnixMilli()
 if observedBeforeBinding<permit.ApprovedAt{observedBeforeBinding=permit.ApprovedAt}
 sample("before-binding",1,observedBeforeBinding)
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 body:=fmt.Sprintf(`{"permit_id":"%s","display_label":"Router uplink",
  "expected_goal_revision":%d,"expected_permit_revision":%d,
  "expires_at":%d}`,permit.ID,goal.Revision,permit.Revision,now+15*60000)
 for _,tc:=range []struct{who string;want int}{
  {"",401},{"outsider",403},{"robot",403},
 }{
  got:=call("POST",tc.who,route,body)
  if got.Code!=tc.want{
   t.Fatalf("nonhuman %s declared target: %d %s",tc.who,got.Code,got.Body)
  }
 }
 if got:=call("POST","operator",route,
  strings.Replace(body,"Router uplink","Bad | unsafe URL",1));got.Code<400{
  t.Fatalf("unsafe target name accepted: %d",got.Code)
 }
 if got:=call("POST","operator",route,
  strings.TrimSuffix(body,"}")+`,"permit_id":"foreign"}`);got.Code!=400{
  t.Fatalf("ambiguous duplicate permit was accepted: %d",got.Code)
 }
 created:=call("POST","operator",route,body)
 if created.Code!=201{t.Fatalf("human target approval refused: %d %s",
  created.Code,created.Body)}
 var target projectworkspace.GoalTCPDeclaredTarget
 if err=json.Unmarshal(created.Body.Bytes(),&target);err!=nil{t.Fatal(err)}
 if target.IdentityClass!="operator_declared_tcp_endpoint"||
  target.SourceID!="router"||target.PermitID!=permit.ID||
  target.Status!="approved"{
  t.Fatalf("operator-assigned target misidentified as physical attested: %+v",target)
 }
 for _,leak:=range []string{"192.168.42.21",
  "\"target_ipv4\"","\"target_port\""}{
  if strings.Contains(created.Body.String(),leak){
   t.Fatalf("declared target API exposed private probe endpoint: %s",leak)
  }
 }
 if got:=call("POST","operator",route,body);got.Code<400{
  t.Fatalf("stale Goal version approved second target: %d",got.Code)
 }
 listed:=call("GET","operator",route,"")
 if listed.Code!=200||!strings.Contains(listed.Body.String(),target.ID)||
  !strings.Contains(listed.Body.String(),"\"effective_identity_state\":\"active\""){
  t.Fatalf("human cannot view current declared target: %d %s",listed.Code,listed.Body)
 }
 if got:=call("GET","outsider",route,"");got.Code!=403{
  t.Fatalf("foreign tenant listed target: %d",got.Code)
 }
 if got:=call("GET","robot",route,"");got.Code!=403{
  t.Fatalf("agent listed target: %d",got.Code)
 }
 before,err:=svc.GoalSignalTriage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if len(before.Sources)!=1||before.Sources[0].TCPDeclaredTargetID!=""{
  t.Fatalf("prebinding receipt inherited a later operator identity: %+v",before)
 }
 time.Sleep(2*time.Millisecond)
 observedAfterBinding:=clock.Real{}.UnixMilli()
 if observedAfterBinding<target.ApprovedAt{
  observedAfterBinding=target.ApprovedAt
 }
 sample("after-binding",2,observedAfterBinding)
 matched,err:=svc.GoalSignalTriage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if len(matched.Sources)!=1||
  matched.Sources[0].TCPDeclaredTargetID!=target.ID||
  matched.Sources[0].TCPIdentityClass!="operator_declared_tcp_endpoint"||
  matched.DeviceIdentityVerified||matched.MonitoringActive||
  matched.RemediationAuthorized||
  matched.CrossClassTargetCorrelation!="not_established"{
  t.Fatalf("operator-labelled evidence incorrectly attested device: %+v",matched)
 }
 // A plan no longer allowing this TCP source makes its operator label
 // unusable even though original human approval remains in audit.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_continuous_goals
  SET monitoring_plan_json=?,revision=revision+1 WHERE id=?`,
  `{"version":1,"source_ids":["different-source"]}`,goal.ID);err!=nil{t.Fatal(err)}
 listed=call("GET","operator",route,"")
 if listed.Code!=200||!strings.Contains(listed.Body.String(),
  "\"effective_identity_state\":\"permit_inactive\""){
  t.Fatalf("removed Goal source still shown as current identity: %d %s",
   listed.Code,listed.Body)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_continuous_goals
  SET monitoring_plan_json=?,revision=revision+1 WHERE id=?`,
  `{"version":1,"source_ids":["router"]}`,goal.ID);err!=nil{t.Fatal(err)}
 // Even retaining the original source name cannot silently transfer an
 // immutable target label to a modified monitoring plan.
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_continuous_goals
  SET monitoring_plan_json=?,revision=revision+1 WHERE id=?`,
  `{"version":1,"source_ids":["router","secondary"]}`,goal.ID);err!=nil{t.Fatal(err)}
 listed=call("GET","operator",route,"")
 if listed.Code!=200||!strings.Contains(listed.Body.String(),
  "\"effective_identity_state\":\"permit_inactive\""){
  t.Fatalf("changed plan reused old target authority despite source match: %d %s",
   listed.Code,listed.Body)
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_continuous_goals
  SET monitoring_plan_json=?,revision=revision+1 WHERE id=?`,
  `{"version":1,"source_ids":["router"]}`,goal.ID);err!=nil{t.Fatal(err)}

 // After editing and RESTORING original Goal JSON, a previous human
 // permit generation cannot be used for new current target evidence.
 stale,err:=svc.GoalSignalTriage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if stale.RecentTCPNegativeSources!=0||
  stale.Sources[0].TCPAttemptID!=""||
  stale.Sources[0].TCPDeclaredTargetID!=""{
  t.Fatalf("old TCP generation trusted after plan restore: %+v",stale)
 }

 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 revokeBody:=fmt.Sprintf(`{"expected_goal_revision":%d,
  "expected_target_revision":1}`,goal.Revision)
 revoke:=route+"/"+target.ID+"/revoke"
 if got:=call("POST","robot",revoke,revokeBody);got.Code!=403{
  t.Fatalf("agent revoked target: %d",got.Code)
 }
 revoked:=call("POST","operator",revoke,revokeBody)
 if revoked.Code!=200{t.Fatalf("human cannot revoke target: %d %s",
  revoked.Code,revoked.Body)}
 listed=call("GET","operator",route,"")
 if listed.Code!=200||!strings.Contains(listed.Body.String(),
  "\"effective_identity_state\":\"revoked\""){
  t.Fatalf("revoked human target still displayed as current: %d %s",
   listed.Code,listed.Body)
 }
 after,err:=svc.GoalSignalTriage(ctx,project.ID,goal.ID)
 if err!=nil{t.Fatal(err)}
 if after.Sources[0].TCPDeclaredTargetID!=""||
  after.RecentTCPNegativeSources!=0{
  t.Fatalf("revoked identity continued to attach or erased TCP evidence: %+v",after)
 }
 if _,err=db.SQL().ExecContext(ctx,`DELETE FROM
  project_goal_tcp_declared_target_bindings WHERE id=?`,target.ID);err==nil{
  t.Fatal("human target approval audit was deletable")
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE
  project_goal_tcp_declared_target_bindings SET target_ipv4='192.168.42.22'
  WHERE id=?`,target.ID);err==nil{
  t.Fatal("historical target IP was mutable")
 }
 goal,err=svc.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if goal.Health!="unobserved"||goal.Status!="configured"||goal.LastVerifiedAt!=0{
  t.Fatalf("target annotation activated security monitor: %+v",goal)
 }
}
