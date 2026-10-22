//go:build integration

package projectworkspace

import (
    "context"
    "encoding/json"
    "testing"

    "github.com/DigiLogicTech/OnePane/internal/clock"
    sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestProjectOperatingModeDefaultsAndExplicitPromotionAreDurable(t *testing.T){
    ctx:=context.Background()
    db,err:=sqliteStore.Open(t.TempDir()+"/operating.db")
    if err!=nil{t.Fatal(err)}
    defer db.Close()
    if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
    for _,q:=range []string{
        `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
         VALUES('tenant','Tenant','active',1,1,1)`,
        `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
         VALUES('owner','human','Owner','active',1,1,1)`,
        `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
         VALUES('tenant','owner','active',1,1)`,
    }{if _,err=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
    svc:=NewService(db.SQL(),db,clock.Real{})
    old,err:=svc.CreateProject(ctx,CreateProjectCommand{
        WorkspaceID:"tenant",Name:"Existing-style",CreatedBy:"owner"})
    if err!=nil{t.Fatal(err)}
    if old.OperatingMode!=OperatingAutonomousProject{
        t.Fatalf("legacy Project should display L4 without mutation: %+v",old)
    }
    if string(old.ProjectPolicyJSON)!="{}"{t.Fatalf("legacy policy was mutated: %s",old.ProjectPolicyJSON)}
    explicit:=json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`)
    updated,err:=svc.UpdateProjectPolicy(ctx,UpdateProjectPolicyCommand{
        ProjectID:old.ID,ExpectedRevision:old.Revision,
        ActorPrincipalID:"owner",ProjectPolicyJSON:explicit,
    })
    if err!=nil{t.Fatal(err)}
    if updated.OperatingMode!=OperatingContinuousOperations||
        updated.Revision!=old.Revision+1{
        t.Fatalf("L5 mode not persisted or revised: %+v",updated)
    }
    again,err:=svc.Project(ctx,old.ID)
    if err!=nil||again.OperatingMode!=OperatingContinuousOperations{
        t.Fatalf("L5 not durable through reload: %+v %v",again,err)
    }
    for _,raw:=range []string{
        `{"onepane_operations":{"version":1,"mode":"superuser"}}`,
        `{"onepane_operations":{"version":2,"mode":"continuous_operations"}}`,
        `{"onepane_operations":true}`,
        `{"onepane_operations":null}`,
    }{
        if _,err:=svc.UpdateProjectPolicy(ctx,UpdateProjectPolicyCommand{
            ProjectID:old.ID,ExpectedRevision:updated.Revision,
            ActorPrincipalID:"owner",ProjectPolicyJSON:json.RawMessage(raw)});err==nil{
            t.Fatalf("invalid Project operating mode accepted: %s",raw)
        }
    }
    unchanged,err:=svc.Project(ctx,old.ID)
    if err!=nil||unchanged.Revision!=updated.Revision||unchanged.OperatingMode!=OperatingContinuousOperations{
        t.Fatalf("invalid policy changed Project mode: %+v %v",unchanged,err)
    }
    // Merely selecting Level 5 cannot create an autonomous Task/Agent,
    // routine, operation or monitoring lease by implication.
    for _,table:=range []string{"tasks","agent_worker_runs","operations"}{
        var n int
        if err:=db.SQL().QueryRowContext(ctx,"SELECT COUNT(*) FROM "+table).Scan(&n);err!=nil{
            t.Fatal(err)
        }
        if n!=0{t.Fatalf("mode change auto-executed work in %s: %d",table,n)}
    }
}
