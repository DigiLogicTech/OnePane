//go:build integration

package projectworkspace

import(
 "context"
 "encoding/json"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

const humanNodeGrant=`{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["node-owned"]}}}`

func TestAgentCannotSelfApproveTrustedNodeTransfersThroughPolicyEditors(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/grant-edit.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('human','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('agent','agent','Worker','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','human','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','agent','active',1,1)`,
 }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 if _,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Denied",CreatedBy:"agent",
  ProjectPolicyJSON:json.RawMessage(humanNodeGrant)});err==nil{
  t.Fatal("Agent created Project with new Node export permission")
 }
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Approved",CreatedBy:"human"})
 if err!=nil{t.Fatal(err)}
 if _,err:=svc.UpdateProjectPolicy(ctx,UpdateProjectPolicyCommand{
  ProjectID:p.ID,ExpectedRevision:p.Revision,
  ProjectPolicyJSON:json.RawMessage(humanNodeGrant),ActorPrincipalID:"agent"});err==nil{
  t.Fatal("Agent expanded Project transfer scope")
 }
 p,err=svc.Project(ctx,p.ID);if err!=nil{t.Fatal(err)}
 if p.Revision!=1{t.Fatal("denied Project mutation consumed revision")}
 p,err=svc.UpdateProjectPolicy(ctx,UpdateProjectPolicyCommand{
  ProjectID:p.ID,ExpectedRevision:p.Revision,
  ProjectPolicyJSON:json.RawMessage(humanNodeGrant),ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 if _,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Denied Workspace",ActorPrincipalID:"agent",
  AISettingsJSON:json.RawMessage(humanNodeGrant)});err==nil{
  t.Fatal("Agent created Workspace with new Node export permission")
 }
 w,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Approved Workspace",ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 if _,err:=svc.UpdateWorkspaceView(ctx,UpdateWorkspaceViewCommand{
  WorkspaceID:w.ID,Name:w.Name,ExpectedRevision:w.Revision,
  AISettingsJSON:json.RawMessage(humanNodeGrant),ActorPrincipalID:"agent"});err==nil{
  t.Fatal("Agent expanded Workspace transfer scope")
 }
 current,err:=svc.WorkspaceView(ctx,w.ID);if err!=nil{t.Fatal(err)}
 if current.Revision!=w.Revision{t.Fatal("denied Workspace mutation persisted")}
 current,err=svc.UpdateWorkspaceView(ctx,UpdateWorkspaceViewCommand{
  WorkspaceID:w.ID,Name:w.Name,ExpectedRevision:current.Revision,
  AISettingsJSON:json.RawMessage(humanNodeGrant),ActorPrincipalID:"human"})
 if err!=nil{t.Fatal(err)}
 // Restriction is safe; reverse expansion still needs the operator.
 restricted,err:=svc.UpdateWorkspaceView(ctx,UpdateWorkspaceViewCommand{
  WorkspaceID:w.ID,Name:w.Name,ExpectedRevision:current.Revision,
  AISettingsJSON:json.RawMessage(`{}`),ActorPrincipalID:"agent"})
 if err!=nil{t.Fatalf("Agent should be able to narrow access: %v",err)}
 if _,err:=svc.UpdateWorkspaceView(ctx,UpdateWorkspaceViewCommand{
  WorkspaceID:w.ID,Name:w.Name,ExpectedRevision:restricted.Revision,
  AISettingsJSON:json.RawMessage(humanNodeGrant),ActorPrincipalID:"agent"});err==nil{
  t.Fatal("Agent restored revoked trusted Node authority")
 }
}

func TestAgentCannotEscalateWorkspaceFeaturePolicyViaProjectEdits(t *testing.T){
 ctx:=context.Background()
 db,err:=sqliteStore.Open(t.TempDir()+"/feature-escalation.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('worker','agent','Worker','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','worker','active',1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 svc:=NewService(db.SQL(),db,clock.Real{})
 privilege:=json.RawMessage(`{"feature_policy":{"ProcessExecute":"enabled","NetworkAccess":"enabled","SecretsAccess":"enabled"}}`)
 if _,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Escalated",CreatedBy:"worker",
  ProjectPolicyJSON:privilege});err==nil{
  t.Fatal("Agent self-granted Project executable/network/secret privileges")
 }
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Restricted",CreatedBy:"operator",
 });if err!=nil{t.Fatal(err)}
 if _,err:=svc.UpdateProjectPolicy(ctx,UpdateProjectPolicyCommand{
  ProjectID:p.ID,ExpectedRevision:p.Revision,
  ActorPrincipalID:"worker",ProjectPolicyJSON:privilege});err==nil{
  t.Fatal("Agent elevated Project executable/network/secret privileges")
 }
 if _,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Bad Worker",ActorPrincipalID:"worker",
  AISettingsJSON:privilege});err==nil{
  t.Fatal("Agent created elevated Workspace")
 }
 w,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Allowed",ActorPrincipalID:"worker",
 });if err!=nil{t.Fatal(err)}
 if _,err:=svc.UpdateWorkspaceView(ctx,UpdateWorkspaceViewCommand{
  WorkspaceID:w.ID,ExpectedRevision:w.Revision,Name:w.Name,
  ActorPrincipalID:"worker",AISettingsJSON:privilege});err==nil{
  t.Fatal("Agent elevated Workspace executable/network/secret privileges")
 }
 w,err=svc.WorkspaceView(ctx,w.ID);if err!=nil{t.Fatal(err)}
 if w.Revision!=1{t.Fatal("denied policy change consumed revision")}
 upgraded,err:=svc.UpdateProjectPolicy(ctx,UpdateProjectPolicyCommand{
  ProjectID:p.ID,ExpectedRevision:p.Revision,
  ActorPrincipalID:"operator",ProjectPolicyJSON:privilege})
 if err!=nil{t.Fatalf("human explicitly approved Project capability: %v",err)}
 if upgraded.Revision!=p.Revision+1{t.Fatal("approved revision not persisted")}
 // The Agent may narrow its own Workspace permissions without human
 // intervention; a subsequent attempt to reopen them must be refused.
 narrowed:=json.RawMessage(`{"feature_policy":{"FileRead":"disabled","FileWrite":"disabled"}}`)
 w,err=svc.UpdateWorkspaceView(ctx,UpdateWorkspaceViewCommand{
  WorkspaceID:w.ID,ExpectedRevision:w.Revision,Name:w.Name,
  ActorPrincipalID:"worker",AISettingsJSON:narrowed})
 if err!=nil{t.Fatalf("Agent restrictive edit rejected: %v",err)}
 if _,err:=svc.UpdateWorkspaceView(ctx,UpdateWorkspaceViewCommand{
  WorkspaceID:w.ID,ExpectedRevision:w.Revision,Name:w.Name,
  ActorPrincipalID:"worker",AISettingsJSON:json.RawMessage(`{}`)});err==nil{
  t.Fatal("Agent restored read/write permissions after revocation")
 }
}
