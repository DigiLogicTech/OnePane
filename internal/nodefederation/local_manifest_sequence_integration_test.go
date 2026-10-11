//go:build integration

package nodefederation

import (
 "context"
 "path/filepath"
 "testing"
 "time"

 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type pinnedManifestClock struct{millis int64}
func(c pinnedManifestClock) Now()time.Time{return time.UnixMilli(c.millis).UTC()}
func(c pinnedManifestClock) UnixMilli()int64{return c.millis}

func TestLocalManifestSequenceSurvivesEqualMillisClockRollbackAndRestart(t *testing.T){
 ctx:=context.Background()
 path:=filepath.Join(t.TempDir(),"node-sequence.db")
 db,err:=sqlitestore.Open(path)
 if err!=nil{t.Fatal(err)}
 if err:=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 if _,err:=db.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,
  protocol_json,capabilities_json,last_seen_at,
  revision,created_at,updated_at)
  VALUES('origin','Origin',1,'origin-fp','local','LOCAL_TRUSTED',
  '{}','{}',1000,1,1000,1000)`);err!=nil{t.Fatal(err)}
 local:=NodeView{ID:"origin",Name:"Origin",Local:true,TrustState:"local"}
 read:=func(db *sqlitestore.DB,ms int64)int64{
  t.Helper()
  svc:=NewService(db.SQL(),db,pinnedManifestClock{ms},local,Identity{},"",30*time.Second)
  manifest,err:=svc.BuildManifest(ctx)
  if err!=nil{t.Fatal(err)}
  if manifest.NodeID!="origin"||manifest.Protocol!=ProtocolVersion||
   manifest.Sequence<=0{t.Fatalf("invalid persisted local manifest: %+v",manifest)}
  return manifest.Sequence
 }
 first:=read(db,1000)
 sameMillisecond:=read(db,1000)
 clockRolledBack:=read(db,800)
 if first!=1000||sameMillisecond!=first+1||clockRolledBack!=sameMillisecond+1{
  t.Fatalf("sequence was not monotonic across equal/older timestamps: %d %d %d",
   first,sameMillisecond,clockRolledBack)
 }
 if err:=db.Close();err!=nil{t.Fatal(err)}
 restored,err:=sqlitestore.Open(path)
 if err!=nil{t.Fatal(err)}
 defer restored.Close()
 if err:=restored.Migrate(ctx);err!=nil{t.Fatal(err)}
 afterRestart:=read(restored,700)
 if afterRestart!=clockRolledBack+1{
  t.Fatalf("process restart and clock rollback reused manifest sequence: got %d want %d",
   afterRestart,clockRolledBack+1)
 }
 if _,err:=restored.SQL().ExecContext(ctx,`INSERT INTO harness_nodes(
  id,name,local,identity_fingerprint,trust_state,trust_zone,
  protocol_json,capabilities_json,last_seen_at,
  revision,created_at,updated_at)
  VALUES('foreign','Peer',0,'foreign-fp','paired','PAIRED_MTLS',
  '{}','{}',1000,1,1000,1000)`);err!=nil{t.Fatal(err)}
 foreign:=NewService(restored.SQL(),restored,pinnedManifestClock{900},
  NodeView{ID:"foreign",Local:false},Identity{},"",30*time.Second)
 if _,err:=foreign.BuildManifest(ctx);err==nil{
  t.Fatal("foreign/untrusted Node reserved origin-local manifest sequence")
 }
 var persisted int64
 if err:=restored.SQL().QueryRowContext(ctx,`SELECT sequence
  FROM local_node_manifest_sequences WHERE node_id='origin'`).Scan(&persisted);
  err!=nil||persisted!=afterRestart{
  t.Fatalf("invalid durable manifest high-water mark: %d %v",persisted,err)
 }
}
