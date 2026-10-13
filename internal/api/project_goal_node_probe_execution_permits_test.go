package api

import (
 "context"
 "encoding/json"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestHumanNodeProbeExecutionPermitAPICannotGrantToAgentOrAnonymous(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/node-one-shot-api.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,query:=range []string{
  "INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)",
  "INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,1,1)",
  "INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Robot','active',1,1,1)",
  "INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)",
  "INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)",
 }{if _,err=db.SQL().ExecContext(ctx,query);err!=nil{t.Fatal(err)}}
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"One shot API",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage("{\"onepane_operations\":{\"version\":1,\"mode\":\"continuous_operations\"}}"),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Scope",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 goal,err:=projects.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"operator",
  Objective:"No network effects from permissions",
  MonitoringPlanJSON:json.RawMessage("{\"version\":1,\"source_ids\":[\"router\"]}"),
 })
 if err!=nil{t.Fatal(err)}
 h:=NewServer(projects,nil,goalAPIAuth{}).Handler()
 route:="/v1/projects/"+p.ID+"/continuous-goals/"+goal.ID+"/node-probe-execution-permits"
 req:=func(method,who,url,body string)*httptest.ResponseRecorder{
  r:=httptest.NewRequest(method,url,strings.NewReader(body))
  if who!=""{r.Header.Set("Authorization","Bearer "+who)}
  if body!=""{r.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder()
  h.ServeHTTP(w,r)
  return w
 }
 input:=`{"instrument_key_id":"missing","challenge_id":"missing",
   "peer_node_id":"peer","expected_goal_revision":1}`
 for _,test:=range []struct{principal string;want int}{
  {"",401},{"outsider",403},{"robot",403},
 }{
  w:=req("POST",test.principal,route,input)
  if w.Code!=test.want{
   t.Fatalf("untrusted %s received Node execution authority: %d %s",
    test.principal,w.Code,w.Body)
  }
  listed:=req("GET",test.principal,route,"")
  if listed.Code!=test.want{
   t.Fatalf("untrusted %s listed permits: %d %s",
    test.principal,listed.Code,listed.Body)
  }
 }
 listed:=req("GET","operator",route,"")
 if listed.Code!=200||!strings.Contains(listed.Body.String(),"[]"){
  t.Fatalf("authorised human cannot inventory permits: %d %s",listed.Code,listed.Body)
 }
 if got:=req("POST","operator",route,input);got.Code<400{
  t.Fatalf("invented peer/challenge executed unapproved scope: %d",got.Code)
 }
 duplicate:=strings.TrimSuffix(input,"}")+`,"peer_node_id":"another"}`
 if got:=req("POST","operator",route,duplicate);got.Code!=400{
  t.Fatalf("duplicate Node scope member accepted: %d %s",got.Code,got.Body)
 }
 revokedURL:=route+"/unknown/revoke"
 body:=`{"expected_goal_revision":1,"expected_permit_revision":1}`
 if got:=req("POST","robot",revokedURL,body);got.Code!=403{
  t.Fatalf("Agent could revoke scoped network grant: %d",got.Code)
 }
 if got:=req("POST","operator",revokedURL,body);got.Code<400{
  t.Fatalf("human revoked a nonexistent permit as if valid: %d",got.Code)
 }
 // A server-origin execution seal is a sensitive HUMAN action.
 // An unconfigured original OnePane signing identity must fail closed,
 // not generate a new identity or return an unsigned imitation.
 sealURL:=route+"/unknown/seal"
 for _,tc:=range []struct{who string;expect int}{
  {"",401},{"outsider",403},{"robot",403},{"operator",503},
 }{
  got:=req("POST",tc.who,sealURL,"")
  if got.Code!=tc.expect{
   t.Fatalf("principal %q unexpectedly exported live Node authority: %d %s",
    tc.who,got.Code,got.Body)
  }
 }
 gotGoal,err:=projects.ContinuousGoal(ctx,goal.ID)
 if err!=nil{t.Fatal(err)}
 if gotGoal.Health!="unobserved"||gotGoal.Status!="configured"||
  gotGoal.LastVerifiedAt!=0{
  t.Fatalf("invalid authorization requests activated Goal: %+v",gotGoal)
 }
}
