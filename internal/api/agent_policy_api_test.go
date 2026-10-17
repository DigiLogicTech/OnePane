package api

import (
 "context"
 "encoding/json"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/agentprofile"
 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestVersionedAgentPolicyAPIEnforcesScopeAndHumanDenialRemoval(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/agent-policy-api.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,sqlStmt:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('robot','agent','Worker','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','robot','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,sqlStmt);err!=nil{t.Fatal(err)}}
 profiles:=agentprofile.NewService(db.SQL(),db,clock.Real{})
 profile,err:=profiles.Create(ctx,agentprofile.CreateCommand{
  WorkspaceID:"tenant",Name:"Restricted Operator",InstructionsMD:"Respect sandbox limits.",
  CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 server:=NewServer(nil,nil,goalAPIAuth{})
 server.SetAgentProfiles(profiles)
 url:="/v1/agent-profiles/"+profile.ID+"/policy"
 call:=func(method,token,target,body string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,target,strings.NewReader(body))
  if token!=""{req.Header.Set("Authorization","Bearer "+token)}
  if body!=""{req.Header.Set("Content-Type","application/json")}
  w:=httptest.NewRecorder()
  server.Handler().ServeHTTP(w,req)
  return w
 }
 if got:=call("GET","",url+"?workspace_id=tenant","");got.Code!=401{
  t.Fatalf("anonymous Agent policy read %d",got.Code)
 }
 if got:=call("GET","outsider",url+"?workspace_id=tenant","");got.Code!=403{
  t.Fatalf("cross-tenant profile read %d",got.Code)
 }
 if got:=call("GET","operator",url+"?workspace_id=tenant","");got.Code!=200{
  t.Fatalf("current Agent policy head unavailable %d %s",got.Code,got.Body)
 }
 restricted:=`{"workspace_id":"tenant","expected_revision":0,"policy":{"feature_policy":{"FileWrite":"disabled"}}}`
 created:=call("PUT","robot",url,restricted)
 if created.Code!=200{t.Fatalf("Agent restriction rejected %d %s",created.Code,created.Body)}
 var policy agentprofile.PolicyRevision
 if err:=json.Unmarshal(created.Body.Bytes(),&policy);err!=nil{t.Fatal(err)}
 if policy.Revision!=1{t.Fatalf("wrong Agent revision %+v",policy)}
 remove:=`{"workspace_id":"tenant","expected_revision":1,"policy":{"feature_policy":{"FileWrite":"inherited"}}}`
 if got:=call("PUT","robot",url,remove);got.Code==200{
  t.Fatal("agent removed its own prior denial")
 }
 if got:=call("PUT","outsider",url,remove);got.Code!=403{
  t.Fatalf("outsider changed Agent profile policy %d",got.Code)
 }
 if got:=call("PUT","operator",url,remove);got.Code!=200{
  t.Fatalf("human review denied %d %s",got.Code,got.Body)
 }
 if got:=call("PUT","operator",url,remove);got.Code==200{
  t.Fatal("stale CAS unexpectedly changed Agent policy")
 }
 if got:=call("GET","operator",url+"?workspace_id=foreign","");got.Code==200{
  t.Fatal("foreign tenant could inspect profile revision")
 }
 current:=call("GET","operator",url+"?workspace_id=tenant","")
 if current.Code!=200||!strings.Contains(current.Body.String(),`"revision":2`){
  t.Fatalf("current Agent policy head missing %d %s",current.Code,current.Body)
 }
}
