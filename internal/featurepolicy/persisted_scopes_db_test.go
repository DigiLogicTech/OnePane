package featurepolicy

import (
 "context"
 "database/sql"
 "path/filepath"
 "testing"

 _ "modernc.org/sqlite"
)

func setupStoredPolicyDB(t *testing.T)*sql.DB{
 t.Helper()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"scopes.db"))
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){db.Close()})
 for _,stmt:=range []string{
  `CREATE TABLE workspaces (id TEXT PRIMARY KEY,status TEXT NOT NULL)`,
  `CREATE TABLE projects (id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,status TEXT NOT NULL,project_policy_json TEXT NOT NULL)`,
  `CREATE TABLE project_workspaces (id TEXT PRIMARY KEY,project_id TEXT NOT NULL,status TEXT NOT NULL,ai_settings_json TEXT NOT NULL)`,
  `CREATE TABLE tasks (id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,project_id TEXT,project_workspace_id TEXT,completion_json TEXT)`,
  `INSERT INTO workspaces VALUES('tenant','active'),('other','active')`,
  `INSERT INTO projects VALUES('project','tenant','active','{"feature_policy":{"CloudRouting":"enabled","Files":"enabled"}}')`,
  `INSERT INTO project_workspaces VALUES('world','project','active','{"feature_policy":{"CloudRouting":"disabled","Files":"disabled"}}')`,
  `INSERT INTO project_workspaces VALUES('story','project','active','{}')`,
  `INSERT INTO tasks VALUES('world-task','tenant','project','world','{"onepane_routing":{"feature_policy":{"CloudRouting":"enabled","Files":"enabled"}}}')`,
  `INSERT INTO tasks VALUES('story-task','tenant','project','story','{}')`,
  `INSERT INTO tasks VALUES('legacy-task','tenant',NULL,NULL,'{}')`,
 }{
  if _,err:=db.Exec(stmt);err!=nil{t.Fatalf("fixture SQL %v: %v",stmt,err)}
 }
 return db
}

func TestStoredScopesCannotEscalateWorkspaceOrCrossTenantPolicy(t *testing.T){
 ctx:=context.Background()
 db:=setupStoredPolicyDB(t)
 reader:=StoredScopes{DB:db}
 for _,action:=range []string{"inference.cloud","files.read","files.write"}{
  allowed,err:=reader.AuthorizeTask(ctx,"tenant","world-task",action)
  if err!=nil||allowed{t.Fatalf("Task escalated Workspace %q: allowed=%v err=%v",action,allowed,err)}
 }
 for _,action:=range []string{"inference.cloud","files.read"}{
  allowed,err:=reader.AuthorizeTask(ctx,"tenant","story-task",action)
  if err!=nil||!allowed{t.Fatalf("Project allowed %q but resolver denied: %v",action,err)}
 }
 for _,bad:=range []struct{tenant,task string}{
  {"other","world-task"},{"tenant","no-such-task"},
 }{
  allowed,err:=reader.AuthorizeTask(ctx,bad.tenant,bad.task,"files.read")
  if err==nil||allowed{t.Fatalf("cross-tenant or absent Task granted: %+v",bad)}
 }
}

func TestStoredScopesRejectsInactiveAndForgedCanonicalWorkspace(t *testing.T){
 ctx:=context.Background()
 db:=setupStoredPolicyDB(t)
 reader:=StoredScopes{DB:db}
 if _,err:=db.Exec("UPDATE project_workspaces SET status='archived' WHERE id='world'");err!=nil{t.Fatal(err)}
 if allowed,err:=reader.AuthorizeTask(ctx,"tenant","world-task","files.read");err==nil||allowed{
  t.Fatalf("archived canonical Workspace admitted: allowed=%v err=%v",allowed,err)
 }
 if _,err:=db.Exec("UPDATE project_workspaces SET status='active' WHERE id='world'");err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("UPDATE tasks SET project_workspace_id='foreign' WHERE id='world-task'");err!=nil{t.Fatal(err)}
 if allowed,err:=reader.AuthorizeTask(ctx,"tenant","world-task","files.read");err==nil||allowed{
  t.Fatalf("forged canonical Workspace admitted: allowed=%v err=%v",allowed,err)
 }
 if _,err:=db.Exec("UPDATE tasks SET project_workspace_id='world' WHERE id='world-task'");err!=nil{t.Fatal(err)}
 if _,err:=db.Exec("UPDATE projects SET status='archived' WHERE id='project'");err!=nil{t.Fatal(err)}
 if allowed,err:=reader.AuthorizeTask(ctx,"tenant","world-task","files.read");err==nil||allowed{
  t.Fatalf("archived Project admitted: allowed=%v err=%v",allowed,err)
 }
}

func TestStoredScopesPreservesUnconfiguredLegacyTask(t *testing.T){
 reader:=StoredScopes{DB:setupStoredPolicyDB(t)}
 allowed,err:=reader.AuthorizeTask(context.Background(),"tenant","legacy-task","inference.cloud")
 if err!=nil||!allowed{t.Fatalf("existing legacy authority boundary should remain responsible: %v",err)}
}
