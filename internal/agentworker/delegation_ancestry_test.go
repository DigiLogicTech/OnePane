package agentworker

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
 _ "modernc.org/sqlite"
)

func TestDelegationAncestryCapsRecursiveChainAndChecksEveryWorkspace(t *testing.T) {
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"delegation_ancestry.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if _,err:=db.ExecContext(ctx,`CREATE TABLE tasks(
 id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,project_id TEXT,project_workspace_id TEXT,parent_task_id TEXT
 )`);err!=nil{t.Fatal(err)}
 project,world:="p","world"
 tasks:=make([]task.Task,0,maxAutonomousDelegationDepth+1)
 for i:=0;i<=maxAutonomousDelegationDepth;i++{
  id:=fmt.Sprintf("t%d",i)
  var parent *string
  if i>0{parent=&tasks[i-1].ID}
  v:=task.Task{
   ID:id,WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world,ParentTaskID:parent,
  }
  if _,err:=db.ExecContext(ctx,`INSERT INTO tasks(id,workspace_id,project_id,project_workspace_id,parent_task_id) VALUES(?,?,?,?,?)`,
    v.ID,v.WorkspaceID,v.ProjectID,v.ProjectWorkspaceID,v.ParentTaskID);err!=nil{t.Fatal(err)}
  tasks=append(tasks,v)
 }
 check:=func(item task.Task) error{
  tx,err:=db.BeginTx(ctx,nil)
  if err!=nil{t.Fatal(err)}
  defer tx.Rollback()
  return verifyDelegationAncestry(ctx,tx,item)
 }
 if err:=check(tasks[0]);err!=nil{t.Fatalf("root should delegate: %v",err)}
 if err:=check(tasks[maxAutonomousDelegationDepth-1]);err!=nil{
  t.Fatalf("last allowed generation must delegate: %v",err)
 }
 if err:=check(tasks[maxAutonomousDelegationDepth]);!errors.Is(err,ErrDelegationDepthLimit){
  t.Fatalf("child at max depth must not delegate again: %v",err)
 }
 if _,err:=db.ExecContext(ctx,`UPDATE tasks SET project_workspace_id='story' WHERE id='t3'`);err!=nil{t.Fatal(err)}
 if err:=check(tasks[5]);!errors.Is(err,ErrDelegationAncestry){
  t.Fatalf("cross-Workspace ancestor must block even with in-scope direct parent: %v",err)
 }
 if _,err:=db.ExecContext(ctx,`UPDATE tasks SET project_workspace_id='world' WHERE id='t3'`);err!=nil{t.Fatal(err)}
 if _,err:=db.ExecContext(ctx,`UPDATE tasks SET parent_task_id='t0' WHERE id='t0'`);err!=nil{t.Fatal(err)}
 mutated:=tasks[0];mutated.ParentTaskID=&mutated.ID
 if err:=check(mutated);!errors.Is(err,ErrDelegationAncestry){
  t.Fatalf("cyclic ancestry must be denied: %v",err)
 }
 if _,err:=db.ExecContext(ctx,`UPDATE tasks SET parent_task_id=NULL WHERE id='t0'`);err!=nil{t.Fatal(err)}
 stale:=tasks[1];stale.ParentTaskID=&tasks[2].ID
 if err:=check(stale);!errors.Is(err,ErrDelegationAncestry){
  t.Fatalf("stale supplied parent ID must not bypass stored ancestry: %v",err)
 }
 foreign:=tasks[1];other:="story";foreign.ProjectWorkspaceID=&other
 if err:=check(foreign);!errors.Is(err,ErrDelegationAncestry){
  t.Fatalf("caller-supplied foreign Workspace boundary must fail: %v",err)
 }
}

func TestDelegationAncestryRejectsNilTransactionAndMissingTask(t *testing.T){
 ctx:=context.Background()
 if err:=verifyDelegationAncestry(ctx,nil,task.Task{ID:"x",WorkspaceID:"tenant"});!errors.Is(err,ErrDelegationAncestry){
  t.Fatalf("nil transaction accepted: %v",err)
 }
}
