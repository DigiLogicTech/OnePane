package sqlite

import (
 "context"
 "strings"
 "testing"
)

func TestScopedWorkspaceTaskIndexMigrationIsAdditiveAndQueryable(t *testing.T){
 ctx:=context.Background()
 db,err:=Open(t.TempDir()+"/task_index.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 // Reopening/migrating must leave the additive index intact.
 if err:=db.Migrate(ctx);err!=nil{t.Fatalf("repeat migration: %v",err)}
 var migrations int
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT COUNT(*) FROM schema_migrations WHERE version=38").
  Scan(&migrations);err!=nil||migrations!=1{
  t.Fatalf("missing or repeated migration 0038: %v %v",migrations,err)
 }
 var indexSQL string
 if err:=db.SQL().QueryRowContext(ctx,
  "SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_tasks_project_workspace_active_updated'").
  Scan(&indexSQL);err!=nil{t.Fatal(err)}
 if !strings.Contains(indexSQL,"archived_at IS NULL"){
  t.Fatalf("Task index must exclude archived rows: %s",indexSQL)
 }
 rows,err:=db.SQL().QueryContext(ctx,`EXPLAIN QUERY PLAN SELECT
  id,workspace_id,project_id,project_workspace_id,artifact_session_id,plan_id,parent_task_id,
  objective,state,scheduling_class,priority,completion_json,result_json,
  revision,ready_at,cancel_requested_at,archived_at,created_at,updated_at
  FROM tasks WHERE workspace_id=? AND project_id=? AND project_workspace_id=?
   AND archived_at IS NULL ORDER BY updated_at DESC,id DESC LIMIT ?`,
  "tenant","game","world",15)
 if err!=nil{t.Fatal(err)}
 defer rows.Close()
 matched:=false
 for rows.Next(){
  var id,parent,unused int
  var detail string
  if err=rows.Scan(&id,&parent,&unused,&detail);err!=nil{t.Fatal(err)}
  if strings.Contains(detail,"idx_tasks_project_workspace_active_updated"){
   matched=true
  }
 }
 if err=rows.Err();err!=nil{t.Fatal(err)}
 if !matched{
  t.Fatal("SQLite query planner did not select the new scoped Task index")
 }
}
