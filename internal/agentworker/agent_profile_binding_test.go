package agentworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "strings"
 "testing"
 
 "github.com/DigiLogicTech/OnePane/internal/task"
 _ "modernc.org/sqlite"
)

func TestCustomAgentProfileRevisionBindingAndRevocation(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",t.TempDir()+"/agent-profile-revocation.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE agent_profiles(
    id TEXT PRIMARY KEY,workspace_id TEXT,status TEXT NOT NULL,revision INTEGER NOT NULL
  )`,
  `INSERT INTO agent_profiles VALUES('ap-custom','ws1','active',2)`,
  `INSERT INTO agent_profiles VALUES('ap-foreign','ws2','active',4)`,
  `INSERT INTO agent_profiles VALUES('ap-archived','ws1','archived',3)`,
 }{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 for _,defaultID:=range []string{"","onepane-default","agent.md"}{
  b,err:=resolveTaskAgentProfileBinding(ctx,db,"ws1",defaultID)
  if err!=nil||b.ID!=""{t.Fatalf("legacy default unexpectedly became an Agent grant: %+v %v",b,err)}
 }
 for _,p:=range []string{"ap-foreign","ap-archived","ap-missing"}{
  if b,err:=resolveTaskAgentProfileBinding(ctx,db,"ws1",p);err==nil||b.ID!=""{
   t.Fatalf("invalid custom profile admitted: %s %+v %v",p,b,err)
  }
 }
 b,err:=resolveTaskAgentProfileBinding(ctx,db,"ws1","ap-custom")
 if err!=nil||b.ID!="ap-custom"||b.Revision!=2{t.Fatalf("unexpected binding: %+v %v",b,err)}
 if err:=enforceTaskAgentProfileBinding(ctx,db,"ws1",b);err!=nil{t.Fatal(err)}
 if _,err=db.ExecContext(ctx,"UPDATE agent_profiles SET revision=revision+1 WHERE id='ap-custom'");err!=nil{t.Fatal(err)}
 if err:=enforceTaskAgentProfileBinding(ctx,db,"ws1",b);err==nil{
  t.Fatal("changed Agent prompt revision passed live pre-dispatch fence")
 }
 updated,err:=resolveTaskAgentProfileBinding(ctx,db,"ws1","ap-custom")
 if err!=nil||updated.Revision!=3{t.Fatalf("updated revision not resolvable %+v %v",updated,err)}
 if _,err=db.ExecContext(ctx,"UPDATE agent_profiles SET status='archived',revision=revision+1 WHERE id='ap-custom'");err!=nil{t.Fatal(err)}
 if err:=enforceTaskAgentProfileBinding(ctx,db,"ws1",updated);err==nil{
  t.Fatal("archived Agent profile permitted previously admitted Worker dispatch")
 }
}

func TestCompileContextRejectsUnknownCustomAgentInsteadOfDefaultFallback(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",t.TempDir()+"/agent-profile-missing.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if _,err=db.ExecContext(ctx,`CREATE TABLE agent_profiles (
  id TEXT PRIMARY KEY,name TEXT,instructions_md TEXT,default_role TEXT,
  revision INTEGER,status TEXT,workspace_id TEXT
 )`);err!=nil{t.Fatal(err)}
 svc:=&Service{db:db}
 completion,_:=json.Marshal(map[string]any{
  "onepane_routing":map[string]any{"agent_profile":"unknown-pinned-custom"},
 })
 _,err=svc.compileContext(ctx,Run{WorkspaceID:"tenant"},task.Task{
  ID:"t1",WorkspaceID:"tenant",Objective:"use explicit Agent only",Completion:completion,
 })
 if err==nil||!strings.Contains(err.Error(),"custom Agent profile"){
  t.Fatalf("missing profile silently fell back to default Agent: %v",err)
 }
}
