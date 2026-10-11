package api

import (
 "context"
 "database/sql"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
 _ "modernc.org/sqlite"
)

func TestWorkspaceTaskDependenciesDiscloseOnlySameWorkspaceStates(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"dependencies.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT,state TEXT)`,
  `CREATE TABLE task_dependencies(task_id TEXT,depends_on_task_id TEXT,dependency_type TEXT)`,
  `INSERT INTO tasks VALUES
    ('parent','tenant','project','world','waiting_dependency'),
    ('world-complete','tenant','project','world','complete'),
    ('world-failed','tenant','project','world','failed'),
    ('world-blocked','tenant','project','world','blocked'),
    ('story-complete','tenant','project','story','complete'),
    ('different-project','tenant','private','world','failed'),
    ('different-tenant','private-tenant','project','world','failed'),
    ('foreign-parent','private-tenant','project','world','waiting_dependency')`,
  `INSERT INTO task_dependencies VALUES
    ('parent','world-complete','hard'),
    ('parent','world-failed','hard'),
    ('parent','world-blocked','hard'),
    ('parent','story-complete','hard'),
    ('parent','different-project','hard'),
    ('parent','different-tenant','hard'),
    ('parent','world-complete','soft'),
    ('foreign-parent','different-tenant','hard')`,
 }{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 project,world,otherTenant:="project","world","private-tenant"
 visible:=[]task.Task{
  {ID:"parent",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world},
  {ID:"foreign-parent",WorkspaceID:otherTenant,ProjectID:&project,ProjectWorkspaceID:&world},
 }
 check:=func()(map[string]taskDependencyEvidence,error){
  return loadWorkspaceTaskDependencies(ctx,db,"tenant",project,world,visible)
 }
 result,err:=check()
 if err!=nil{t.Fatal(err)}
 if len(result)!=1{t.Fatalf("cross-tenant or Project dependencies leaked: %+v",result)}
 d:=result["parent"]
 if d.Total!=6||d.Completed!=1||d.Failed!=1||d.Blocked!=1||
  d.Restricted!=3||d.Remaining!=5 {
  t.Fatalf("incorrect hard prerequisite summary: %+v",d)
 }
 // A different Workspace's private state must not alter exposed counts.
 if _,err:=db.ExecContext(ctx,`UPDATE tasks SET state='failed' WHERE id='story-complete'`);err!=nil{t.Fatal(err)}
 changed,err:=check()
 if err!=nil{t.Fatal(err)}
 if changed["parent"]!=d{t.Fatalf("foreign child state leaked through projection: before=%+v after=%+v",d,changed["parent"])}
 // A forged visible parent with World labels is independently rejected by SQL.
 forged:=[]task.Task{{ID:"foreign-parent",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world}}
 denied,err:=loadWorkspaceTaskDependencies(ctx,db,"tenant",project,world,forged)
 if err!=nil||len(denied)!=0{t.Fatalf("forged parent escaped backend scope: %+v %v",denied,err)}
 none,err:=loadWorkspaceTaskDependencies(ctx,db,"tenant",project,"",visible)
 if err!=nil||len(none)!=0{t.Fatalf("missing Workspace scope must not expose dependencies: %+v %v",none,err)}
}
