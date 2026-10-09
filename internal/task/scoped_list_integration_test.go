//go:build integration

package task_test

import (
 "context"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// A busy tenancy must not cause an older World Task to disappear behind
// dozens of newer Story or other Project Tasks before Workspace filtering.
func TestScopedTaskInventoryFiltersBeforeLimit(t *testing.T) {
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/scoped_tasks.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','operator','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 projects:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{WorkspaceID:"tenant",Name:"Game",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 other,err:=projects.CreateProject(ctx,projectworkspace.CreateProjectCommand{WorkspaceID:"tenant",Name:"Other",CreatedBy:"operator"})
 if err!=nil{t.Fatal(err)}
 world,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"World",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 story,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:p.ID,Name:"Story",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 foreign,err:=projects.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{ProjectID:other.ID,Name:"Other Work",ActorPrincipalID:"operator"})
 if err!=nil{t.Fatal(err)}
 tasks:=task.NewService(db.SQL(),db,clock.Real{})
 create:=func(projectID,projectWorkspaceID,name string){
  t.Helper()
  if _,e:=tasks.Create(ctx,task.CreateCommand{
   WorkspaceID:"tenant",ProjectID:&projectID,ProjectWorkspaceID:&projectWorkspaceID,
   Objective:name,Completion:[]byte(`{"type":"operator_review"}`),
  });e!=nil{t.Fatal(e)}
 }
 create(p.ID,world.ID,"world-first")
 for i:=0;i<40;i++{create(p.ID,story.ID,"story-task")}
 for i:=0;i<20;i++{create(other.ID,foreign.ID,"other-project-task")}
 got,err:=tasks.ListProjectWorkspace(ctx,"tenant",p.ID,world.ID,15)
 if err!=nil||len(got)!=1||got[0].Objective!="world-first"{
  t.Fatalf("World Task was lost to tenancy-wide limit: rows=%+v err=%v",got,err)
 }
 storyRows,err:=tasks.ListProjectWorkspace(ctx,"tenant",p.ID,story.ID,15)
 if err!=nil||len(storyRows)!=15{t.Fatalf("Story scoped limit failed: count=%d err=%v",len(storyRows),err)}
 for _,row:=range storyRows{
  if row.ProjectID==nil||*row.ProjectID!=p.ID||row.ProjectWorkspaceID==nil||*row.ProjectWorkspaceID!=story.ID{
   t.Fatalf("Task escaped scoped SQL predicate: %+v",row)
  }
 }
 if mismatch,err:=tasks.ListProjectWorkspace(ctx,"tenant",other.ID,world.ID,15);err!=nil||len(mismatch)!=0{
  t.Fatalf("foreign Project/Workspace selector leaked data: rows=%d err=%v",len(mismatch),err)
 }
 if mismatch,err:=tasks.ListProjectWorkspace(ctx,"other-tenant",p.ID,world.ID,15);err!=nil||len(mismatch)!=0{
  t.Fatalf("foreign tenancy selector leaked data: rows=%d err=%v",len(mismatch),err)
 }
 if _,err:=tasks.ListProjectWorkspace(ctx,"tenant",p.ID,"",15);err==nil{
  t.Fatal("accepted missing Workspace selector")
 }
}
