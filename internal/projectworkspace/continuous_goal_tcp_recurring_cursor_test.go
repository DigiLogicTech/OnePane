package projectworkspace

import (
 "context"
 "database/sql"
 "errors"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 _ "modernc.org/sqlite"
)

func TestRecurringTCPWorkerCursorCASRefusesStalePollerProgress(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 for _,q:=range []string{
  `CREATE TABLE goal_tcp_recurring_worker_cursors(
    worker_id TEXT PRIMARY KEY, after_grant_id TEXT,
    revision INTEGER, updated_at INTEGER)`,
  `INSERT INTO goal_tcp_recurring_worker_cursors VALUES
    ('exact_tcp_connect','',1,0)`,
 }{
  if _,err=db.Exec(q);err!=nil{t.Fatal(err)}
 }
 s:=&Service{db:db,clock:clock.Real{}}
 ctx:=context.Background()
 if err:=s.advanceTCPRecurringCursor(ctx,1,"grant-a");err!=nil{t.Fatal(err)}
 if err:=s.advanceTCPRecurringCursor(ctx,1,"stale-grant");!errors.Is(err,ErrRevisionConflict){
  t.Fatalf("stale worker falsely acknowledged cursor progress: %v",err)
 }
 var pointer string
 var revision int64
 if err:=db.QueryRowContext(ctx,`SELECT after_grant_id,revision
   FROM goal_tcp_recurring_worker_cursors WHERE worker_id='exact_tcp_connect'`).
   Scan(&pointer,&revision);err!=nil{t.Fatal(err)}
 if pointer!="grant-a"||revision!=2{
  t.Fatalf("stale worker overwrote durable traversal pointer: %q revision=%d",pointer,revision)
 }
 if err:=s.advanceTCPRecurringCursor(ctx,revision,"grant-b");err!=nil{t.Fatal(err)}
 cancelCtx,cancel:=context.WithCancel(ctx)
 cancel()
 if err:=s.advanceTCPRecurringCursor(cancelCtx,3,"ignored");!errors.Is(err,context.Canceled){
  t.Fatalf("cancelled cursor update was not fail closed: %v",err)
 }
 editCtx,end:=context.WithTimeout(ctx,time.Second)
 defer end()
 if err:=db.QueryRowContext(editCtx,`SELECT revision FROM goal_tcp_recurring_worker_cursors
  WHERE worker_id='exact_tcp_connect'`).Scan(&revision);err!=nil||revision!=3{
  t.Fatalf("cursor reader pinned SQLite or cancellation changed revision: %d %v",revision,err)
 }
}
