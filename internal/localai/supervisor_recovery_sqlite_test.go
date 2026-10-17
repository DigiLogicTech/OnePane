package localai

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
 "testing"
 "time"

 _ "modernc.org/sqlite"
)

// A one-connection pool exercises the SQLite reader-lock lifetime directly.
func TestRecoverySnapshotClosesSingleSQLiteConnectionBeforeMutation(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 ctx:=context.Background()
 if _,err=db.ExecContext(ctx,`CREATE TABLE local_runtime_instances(
  id TEXT PRIMARY KEY,pid INTEGER,revision INTEGER,status TEXT)`);err!=nil{t.Fatal(err)}
 if _,err=db.ExecContext(ctx,`INSERT INTO local_runtime_instances VALUES
  ('model-b',42,3,'healthy'),('model-a',NULL,1,'starting'),
  ('old',5,2,'stopped')`);err!=nil{t.Fatal(err)}
 xs,err:=recoverableRuntimeSnapshot(ctx,db)
 if err!=nil||len(xs)!=2||xs[0].id!="model-a"||xs[0].pid!=0||
  xs[1].id!="model-b"||xs[1].pid!=42||xs[1].revision!=3{
  t.Fatalf("unexpected bounded recovery snapshot: %+v %v",xs,err)
 }
 mutationCtx,cancel:=context.WithTimeout(ctx,500*time.Millisecond)
 defer cancel()
 if _,err=db.ExecContext(mutationCtx,`UPDATE local_runtime_instances
   SET status='orphaned' WHERE id='model-b' AND revision=3`);err!=nil{
  t.Fatalf("recovery reader still holds single SQLite connection: %v",err)
 }
 after,err:=recoverableRuntimeSnapshot(ctx,db)
 if err!=nil||len(after)!=1||after[0].id!="model-a"{
  t.Fatalf("recovery retained old active rows: %+v %v",after,err)
 }
}

func TestRecoverySnapshotBoundsHugeOrCancelledRegistry(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 if _,err=db.Exec(`CREATE TABLE local_runtime_instances(
  id TEXT PRIMARY KEY,pid INTEGER,revision INTEGER,status TEXT)`);err!=nil{t.Fatal(err)}
 tx,err:=db.Begin()
 if err!=nil{t.Fatal(err)}
 stmt,err:=tx.Prepare(`INSERT INTO local_runtime_instances VALUES(?,?,1,'healthy')`)
 if err!=nil{t.Fatal(err)}
 for i:=0;i<513;i++{
  if _,err=stmt.Exec(fmt.Sprintf("model-%04d",i),i+2);err!=nil{t.Fatal(err)}
 }
 if err=stmt.Close();err!=nil{t.Fatal(err)}
 if err=tx.Commit();err!=nil{t.Fatal(err)}
 xs,err:=recoverableRuntimeSnapshot(context.Background(),db)
 if err==nil||len(xs)!=0{t.Fatalf("more than 512 model processes silently truncated: %d %v",len(xs),err)}
 if _,err=db.Exec(`UPDATE local_runtime_instances SET status='stopped'`);err!=nil{
  t.Fatalf("oversized recovery left SQLite reader pinned: %v",err)
 }
 cancelled,cancel:=context.WithCancel(context.Background())
 cancel()
 if _,err=recoverableRuntimeSnapshot(cancelled,db);!errors.Is(err,context.Canceled){
  t.Fatalf("cancelled recovery was not fail-closed: %v",err)
 }
}
