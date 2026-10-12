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

func TestL5TCPSourceRegistrationHTTPRequiresHumanAndExactAddress(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/tcp-probe-http.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Human','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('robot','agent','Agent','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 service:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=service.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Local observation",CreatedBy:"operator",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 })
 if err!=nil{t.Fatal(err)}
 ws,err:=service.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Approved networks",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 g,err:=service.ConfigureContinuousGoal(ctx,projectworkspace.ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,
  ActorPrincipalID:"operator",Objective:"Observe one LAN endpoint",
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 })
 if err!=nil{t.Fatal(err)}
 h:=NewServer(service,nil,goalAPIAuth{}).Handler()
 route:="/v1/projects/"+p.ID+"/continuous-goals/"+g.ID+"/tcp-probe-permits"
 call:=func(method,who,url,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,url,strings.NewReader(body))
  if who!=""{req.Header.Set("Authorization","Bearer "+who)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder();h.ServeHTTP(w,req)
  return w
 }
 input:=fmt.Sprintf(`{"source_id":"router","target_ipv4":"192.168.1.1",
  "target_port":443,"timeout_ms":500,"expected_goal_revision":1,"expires_at":%d}`,
  time.Now().Add(time.Hour).UnixMilli())
 for _,who:=range []string{"","outsider","robot"}{
  got:=call("POST",who,route,input)
  want:=403
  if who==""{want=401}
  if got.Code!=want{t.Fatalf("untrusted approval %q got %d: %s",who,got.Code,got.Body)}
 }
 bad:=strings.Replace(input,"192.168.1.1","169.254.169.254",1)
 if got:=call("POST","operator",route,bad);got.Code!=400{
  t.Fatalf("metadata IP accepted: %d %s",got.Code,got.Body)
 }
 granted:=call("POST","operator",route,input)
 if granted.Code!=201{t.Fatalf("TCP target grant rejected: %d %s",granted.Code,granted.Body)}
 var permit projectworkspace.GoalTCPProbePermit
 if err:=json.Unmarshal(granted.Body.Bytes(),&permit);err!=nil{t.Fatal(err)}
 if permit.TargetIPv4!="192.168.1.1"||permit.TargetPort!=443||
  permit.EffectClass!="network_tcp_connect"||permit.Status!="approved"{
  t.Fatalf("incorrect network scope: %+v",permit)
 }
 if got:=call("POST","operator",route,input);got.Code!=409{
  t.Fatalf("stale grant CAS accepted: %d %s",got.Code,got.Body)
 }
 if got:=call("GET","outsider",route,"");got.Code!=403{
  t.Fatalf("foreign IP metadata read: %d",got.Code)
 }
 if got:=call("GET","operator",route,"");got.Code!=200||
  !strings.Contains(got.Body.String(),permit.ID){
  t.Fatalf("approved source missing from list: %d %s",got.Code,got.Body)
 }
 stop:=route+"/"+permit.ID+"/revoke"
 revoke:=`{"expected_goal_revision":2,"expected_permit_revision":1}`
 if got:=call("POST","robot",stop,revoke);got.Code!=403{
  t.Fatalf("Agent revoked source: %d %s",got.Code,got.Body)
 }
 if got:=call("POST","operator",stop,revoke);got.Code!=200{
  t.Fatalf("source revocation denied: %d %s",got.Code,got.Body)
 }
 if got:=call("POST","operator",stop,revoke);got.Code!=409{
  t.Fatalf("replayed revocation accepted: %d %s",got.Code,got.Body)
 }
 current,err:=service.ContinuousGoal(ctx,g.ID)
 if err!=nil{t.Fatal(err)}
 if current.Status!="configured"||current.Health!="unobserved"||
  current.LastCheckedAt!=0||current.LastVerifiedAt!=0||current.NextDueAt!=0{
  t.Fatalf("registration pretended monitoring active: %+v",current)
 }
 var tasks,observations int
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM tasks").Scan(&tasks);err!=nil{t.Fatal(err)}
 if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM observations").Scan(&observations);err!=nil{t.Fatal(err)}
 if tasks!=0||observations!=0{
  t.Fatalf("registration triggered network side-effects: tasks=%d observations=%d",tasks,observations)
 }
}
