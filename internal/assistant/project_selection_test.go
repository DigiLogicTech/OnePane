package assistant

import(
 "context"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestUnifiedAssistantNeverSelectsProjectFromUntrustedChatText(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/assistant-scope.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('tenant','User workspace','active',1,1,1)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
    VALUES('foreign','Other workspace','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
    VALUES('foreign','operator','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 ps:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 target,err:=ps.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Home network security",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 other,err:=ps.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:"foreign",Name:"Private project",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 svc:=NewService(db.SQL(),clock.Real{},nil,nil,nil,nil)
 thread:=Thread{WorkspaceID:"tenant"}
 mention:="I just want to ask questions about Home network security; please don't touch any files."
 selected,err:=svc.resolveProject(ctx,thread,mention,nil)
 if err!=nil||selected!=nil{
  t.Fatalf("Project name in chat switched execution context: %v %v",selected,err)
 }
 explicit:=target.ID
 selected,err=svc.resolveProject(ctx,thread,"Execute approved Project plan",&explicit)
 if err!=nil||selected==nil||*selected!=target.ID{
  t.Fatalf("explicit Project not available: %v %v",selected,err)
 }
 thread.ActiveProjectID=&explicit
 selected,err=svc.resolveProject(ctx,thread,"Please continue",nil)
 if err!=nil||selected==nil||*selected!=target.ID{
  t.Fatalf("selected thread scope was discarded: %v %v",selected,err)
 }
 foreign:=other.ID
 if selected,err=svc.resolveProject(ctx,thread,"", &foreign);err==nil||selected!=nil{
  t.Fatalf("cross-tenant Project allowed: %v %v",selected,err)
 }
 if _,err:=db.SQL().ExecContext(ctx,
   "UPDATE projects SET status='archived' WHERE id=?",target.ID);err!=nil{t.Fatal(err)}
 if selected,err=svc.resolveProject(ctx,thread,"Home network security",nil);err==nil||selected!=nil{
  t.Fatalf("archived selected Project retained implicit execution: %v %v",selected,err)
 }
}
