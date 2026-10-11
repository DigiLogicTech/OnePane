//go:build integration

package operation

import (
 "context"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestRestartBlocksAllInterruptedExternalEffectPhasesWithoutReplay(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/recovery.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('agent','agent','Agent','active',1,?,?)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 initial:=map[string]State{
  "effect-executing":StateExecuting,
  "effect-observing":StateObserving,
  "effect-verified":StateVerified,
  "effect-compensating":StateCompensating,
  "already-unknown":StateUnknownOutcome,
  "still-prepared":StatePrepared,
  "already-committed":StateCommitted,
 }
 for id,st:=range initial{
  if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO operations(
   id,workspace_id,principal_id,idempotency_key,state,capability_id,
   resource_ref,tool_id,tool_version,adapter_id,adapter_version,
   desired_state_json,precondition_json,reconciliation_json,
   policy_revision,input_hash,revision,created_at,updated_at)
   VALUES(?, 'tenant','agent',?,?,'test.effect','resource://test',
    'test.external','1','adapter','1','{}','{}','{}',1,'hash',1,?,?)`,
    id,"idem-"+id,string(st),now,now);err!=nil{t.Fatalf("%s: %v",id,err)}
 }
 // Bootstrap's recovery pass must not need live Tool adapters or invoke
 // side effects. It only transitions persisted Operation state and audit.
 coordinator:=NewCoordinator(db.SQL(),db,nil,nil,nil,clock.Real{})
 swept,err:=coordinator.RecoverInterrupted(ctx,nil,nil,nil)
 if err!=nil{t.Fatal(err)}
 if len(swept.Interrupted)!=4||len(swept.Blocked)!=5{
  t.Fatalf("unexpected interrupted/blocked sweep: %+v",swept)
 }
 for id,prior:=range initial{
  var state string
  var revision int64
  if err:=db.SQL().QueryRowContext(ctx,
   `SELECT state,revision FROM operations WHERE id=?`,id).
   Scan(&state,&revision);err!=nil{t.Fatal(err)}
  want:=StateBlockedUnknownOutcome
  if prior==StatePrepared||prior==StateCommitted{want=prior}
  if State(state)!=want{
   t.Fatalf("%s previously %s recovered as %s; want %s",id,prior,state,want)
  }
  if (prior==StatePrepared||prior==StateCommitted)&&revision!=1{
   t.Fatalf("untouched state mutated on recovery: %s rev %d",id,revision)
  }
  if prior!=StatePrepared&&prior!=StateCommitted&&prior!=StateUnknownOutcome&&revision!=3{
   t.Fatalf("interrupted operation missing two audited transitions: %s rev %d",id,revision)
  }
  if prior==StateUnknownOutcome&&revision!=2{
   t.Fatalf("unknown outcome not blocked exactly once: %d",revision)
  }
 }
 again,err:=coordinator.RecoverInterrupted(ctx,nil,nil,nil)
 if err!=nil||len(again.Interrupted)!=0||len(again.Blocked)!=0{
  t.Fatalf("boot recovery replay was not idempotent: %+v %v",again,err)
 }
}
