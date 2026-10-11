package api

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type goalAPIAuth struct{}
func(goalAPIAuth)Authenticate(r *http.Request)(Identity,error){
 switch r.Header.Get("Authorization"){
 case "Bearer operator":return Identity{PrincipalID:"operator",PrincipalType:"human"},nil
 case "Bearer outsider":return Identity{PrincipalID:"outsider",PrincipalType:"human"},nil
 default:return Identity{},ErrUnauthenticated
 }
}
func(goalAPIAuth)AuthorizeWorkspace(_ context.Context,i Identity,ws,capability string)error{
 if i.PrincipalID!="operator"||ws!="tenant"||
  (capability!="project.read"&&capability!="project.write"){return ErrForbidden}
 return nil
}

func TestContinuousGoalsAPIRequiresProjectAuthorizationAndNeverActivatesMonitoring(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/goal-api.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Security",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 });if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Monitoring",ActorPrincipalID:"operator",
 });if err!=nil{t.Fatal(err)}
 server:=NewServer(svc,nil,goalAPIAuth{})
 h:=server.Handler()
 call:=func(method,token,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,"/v1/projects/"+p.ID+"/continuous-goals",strings.NewReader(body))
  if token!=""{req.Header.Set("Authorization","Bearer "+token)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  rw:=httptest.NewRecorder()
  h.ServeHTTP(rw,req)
  return rw
 }
 if got:=call("GET","","");got.Code!=401{t.Fatalf("unauthenticated read %d",got.Code)}
 if got:=call("GET","outsider","");got.Code!=403{t.Fatalf("outsider read %d",got.Code)}
 if got:=call("GET","operator","");got.Code!=200||!strings.Contains(got.Body.String(),"[]"){
  t.Fatalf("empty goal list mismatch %d %s",got.Code,got.Body.String())
 }
 body:=`{"project_workspace_id":"`+ws.ID+`","objective":"Protect approved local devices",
 "cadence_minutes":60,"monitoring_plan":{"version":1,"source_ids":["uptime"]}}`
 if got:=call("POST","outsider",body);got.Code!=403{
  t.Fatalf("outsider could configure goal %d %s",got.Code,got.Body.String())
 }
 if got:=call("POST","operator",strings.Replace(body,ws.ID,"foreign",1));
  got.Code<400||got.Code>=500{
  t.Fatalf("foreign Workspace accepted %d %s",got.Code,got.Body.String())
 }
 created:=call("POST","operator",body)
 if created.Code!=201{t.Fatalf("goal config denied %d %s",created.Code,created.Body.String())}
 var g projectworkspace.ContinuousGoal
 if err:=json.Unmarshal(created.Body.Bytes(),&g);err!=nil{t.Fatal(err)}
 if g.Status!="configured"||g.Health!="unobserved"||g.NextDueAt!=0{
  t.Fatalf("config implied active protection: %+v",g)
 }
 listed:=call("GET","operator","")
 if listed.Code!=200||!strings.Contains(listed.Body.String(),g.ID){
  t.Fatalf("configured goal not listed %d %s",listed.Code,listed.Body.String())
 }
 transition:=func(goalID,action,token,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest("POST","/v1/projects/"+p.ID+"/continuous-goals/"+goalID+"/"+action,strings.NewReader(body))
  if token!=""{req.Header.Set("Authorization","Bearer "+token)}
  req.Header.Set("Content-Type","application/json")
  rw:=httptest.NewRecorder();h.ServeHTTP(rw,req);return rw
 }
 pauseBody:=`{"expected_revision":1}`
 if got:=transition(g.ID,"paused","outsider",pauseBody);got.Code!=403{
  t.Fatalf("outsider paused goal: %d %s",got.Code,got.Body.String())
 }
 if got:=transition(g.ID,"active","operator",pauseBody);got.Code!=400{
  t.Fatalf("unverified activation accepted: %d %s",got.Code,got.Body.String())
 }
 paused:=transition(g.ID,"paused","operator",pauseBody)
 if paused.Code!=200{t.Fatalf("pause denied %d %s",paused.Code,paused.Body.String())}
 var pg projectworkspace.ContinuousGoal
 if err:=json.Unmarshal(paused.Body.Bytes(),&pg);err!=nil{t.Fatal(err)}
 if pg.Status!="paused"||pg.Revision!=2||pg.NextDueAt!=0{t.Fatalf("pause state invalid %+v",pg)}
 if got:=transition(g.ID,"retired","operator",pauseBody);got.Code!=409{
  t.Fatalf("stale revision was accepted: %d %s",got.Code,got.Body.String())
 }
 retired:=transition(g.ID,"retired","operator",`{"expected_revision":2}`)
 if retired.Code!=200||!strings.Contains(retired.Body.String(),`"status":"retired"`){
  t.Fatalf("retirement denied %d %s",retired.Code,retired.Body.String())
 }
 if got:=transition(g.ID,"paused","operator",`{"expected_revision":3}`);got.Code!=400{
  t.Fatalf("retired goal could be re-enabled %d %s",got.Code,got.Body.String())
 }
 var count int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&count);err!=nil||count!=0{
  t.Fatalf("goal API unexpectedly launched task: %d %v",count,err)
 }
}
