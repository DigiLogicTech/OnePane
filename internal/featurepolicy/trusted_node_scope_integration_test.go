//go:build integration

package featurepolicy

import (
    "context"
    "encoding/json"
    "reflect"
    "testing"
    "time"

    "github.com/DigiLogicTech/OnePane/internal/clock"
    "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
    sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
    "github.com/DigiLogicTech/OnePane/internal/task"
)

const pairPolicy = `{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["node-a","node-b"]}}}`
const narrowedPolicy = `{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["node-b","node-c"]}}}`

func TestTrustedNodeGrantRequiresPersistedProjectAndWorkspaceIntersection(t *testing.T){
    ctx:=context.Background()
    db,err:=sqliteStore.Open(t.TempDir()+"/trust.db");if err!=nil{t.Fatal(err)}
    defer db.Close()
    if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
    now:=time.Now().UTC().UnixMilli()
    for _,q:=range []string{
        `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
            VALUES('tenant','Tenant','active',1,1,1)`,
        `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
            VALUES('agent','human','Operator','active',1,1,1)`,
        `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
            VALUES('tenant','agent','active',1,1)`,
    }{
        if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}
    }
    _=now
    projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
    p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{
        WorkspaceID:"tenant",Name:"Project",CreatedBy:"agent",
        ProjectPolicyJSON:json.RawMessage(pairPolicy),
    });if err!=nil{t.Fatal(err)}
    w,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
        ProjectID:p.ID,Name:"Tasks",ActorPrincipalID:"agent",
        AISettingsJSON:json.RawMessage(narrowedPolicy),
    });if err!=nil{t.Fatal(err)}
    tasks:=task.NewService(db.SQL(),db,clock.Real{})
    row,err:=tasks.Create(ctx,task.CreateCommand{WorkspaceID:"tenant",ProjectID:&p.ID,
        ProjectWorkspaceID:&w.ID,Objective:"run on approved Node"})
    if err!=nil{t.Fatal(err)}
    scopes:=StoredScopes{DB:db.SQL()}
    ids,err:=scopes.TrustedNodesForTask(ctx,"tenant",row.ID)
    if err!=nil||!reflect.DeepEqual(ids,[]string{"node-b"}){
        t.Fatalf("intersection=%v err=%v",ids,err)
    }
    for _,tc:=range []struct{id string;allow bool}{
        {"node-a",false},{"node-b",true},{"node-c",false},{"*",false},
    }{
        ok,err:=scopes.AuthorizeNodeTransfer(ctx,"tenant",row.ID,tc.id)
        if err!=nil||ok!=tc.allow{t.Fatalf("%s: allow=%v error=%v",tc.id,ok,err)}
    }
    if other,err:=scopes.TrustedNodesForTask(ctx,"other-tenant",row.ID);
        err!=nil||len(other)!=0{t.Fatalf("cross-tenant grant=%v err=%v",other,err)}
    // Duplicate keys in either persisted half of the human transfer
    // grant may be parsed differently by another consumer: fail closed.
    ambiguous:=`{"onepane_routing":{},"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["node-b"]}}}`
    if _,err:=db.SQL().ExecContext(ctx,
        "UPDATE projects SET project_policy_json=? WHERE id=?",ambiguous,p.ID);err!=nil{t.Fatal(err)}
    if got,err:=scopes.TrustedNodesForTask(ctx,"tenant",row.ID);
        err==nil||len(got)>0{t.Fatalf("ambiguous Project granted remote access: %v %v",got,err)}
    if _,err:=db.SQL().ExecContext(ctx,
        "UPDATE projects SET project_policy_json=? WHERE id=?",pairPolicy,p.ID);err!=nil{t.Fatal(err)}
    if _,err:=db.SQL().ExecContext(ctx,
        "UPDATE project_workspaces SET ai_settings_json=? WHERE id=?",ambiguous,w.ID);err!=nil{t.Fatal(err)}
    if got,err:=scopes.TrustedNodesForTask(ctx,"tenant",row.ID);
        err==nil||len(got)>0{t.Fatalf("ambiguous Workspace granted remote access: %v %v",got,err)}
    if _,err:=db.SQL().ExecContext(ctx,
        "UPDATE project_workspaces SET ai_settings_json=? WHERE id=?",narrowedPolicy,w.ID);err!=nil{t.Fatal(err)}
    if _,err:=db.SQL().ExecContext(ctx,
        "UPDATE project_workspaces SET ai_settings_json='{}' WHERE id=?",w.ID);err!=nil{t.Fatal(err)}
    ids,err=scopes.TrustedNodesForTask(ctx,"tenant",row.ID)
    if err!=nil||len(ids)!=0{t.Fatalf("revoked workspace grant=%v err=%v",ids,err)}
    if _,err:=db.SQL().ExecContext(ctx,
        "UPDATE project_workspaces SET ai_settings_json=? WHERE id=?",narrowedPolicy,w.ID);err!=nil{t.Fatal(err)}
    if _,err:=db.SQL().ExecContext(ctx,
        "UPDATE projects SET project_policy_json='{}' WHERE id=?",p.ID);err!=nil{t.Fatal(err)}
    ids,err=scopes.TrustedNodesForTask(ctx,"tenant",row.ID)
    if err!=nil||len(ids)!=0{t.Fatalf("revoked Project grant=%v err=%v",ids,err)}
    if _,err:=db.SQL().ExecContext(ctx,
        "UPDATE projects SET project_policy_json=? WHERE id=?",pairPolicy,p.ID);err!=nil{t.Fatal(err)}
    if _,err:=db.SQL().ExecContext(ctx,
        "UPDATE project_workspaces SET ai_settings_json=? WHERE id=?",
        `{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["*"]}}}`,w.ID);err!=nil{t.Fatal(err)}
    ids,err=scopes.TrustedNodesForTask(ctx,"tenant",row.ID)
    if err==nil||len(ids)!=0{t.Fatalf("malformed saved Workspace expanded grant=%v err=%v",ids,err)}
    if _,err:=db.SQL().ExecContext(ctx,
        "UPDATE project_workspaces SET ai_settings_json=? WHERE id=?",narrowedPolicy,w.ID);err!=nil{t.Fatal(err)}
    if _,err:=db.SQL().ExecContext(ctx,"UPDATE project_workspaces SET status='archived' WHERE id=?",w.ID);err!=nil{t.Fatal(err)}
    ids,err=scopes.TrustedNodesForTask(ctx,"tenant",row.ID)
    if err!=nil||len(ids)!=0{t.Fatalf("inactive Workspace transfer=%v err=%v",ids,err)}
}
