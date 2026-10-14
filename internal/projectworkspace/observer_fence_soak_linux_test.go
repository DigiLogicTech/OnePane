//go:build integration && linux

package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "runtime"
 "syscall"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// TestObserverFenceBoundedRestartWALDurabilitySoak stresses OnePane's real
// disk-backed SQLite file on a hosted Linux runner. It NEVER performs a
// network probe, deletes events or purges user history. Physical Node /
// long-duration endurance acceptance remains independent of this test.
func TestObserverFenceBoundedRestartWALDurabilitySoak(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),4*time.Minute)
 defer cancel()
 path:=filepath.Join(t.TempDir(),"observation-retention.db")
 db,err:=sqliteStore.Open(path);if err!=nil{t.Fatal(err)}
 closed:=false
 defer func(){if !closed{_ =db.Close()}}()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,1,1)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('owner','human','Owner','active',1,1,1)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','owner','active',1,1)`,
 }{
  if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Temporary soak Project",CreatedBy:"owner",
  ProjectPolicyJSON:json.RawMessage(`{"onepane_operations":{"version":1,"mode":"continuous_operations"}}`),
 });if err!=nil{t.Fatal(err)}
 ws,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Isolated",ActorPrincipalID:"owner",
 });if err!=nil{t.Fatal(err)}
 goal,err:=svc.ConfigureContinuousGoal(ctx,ConfigureContinuousGoalCommand{
  ProjectID:p.ID,ProjectWorkspaceID:ws.ID,ActorPrincipalID:"owner",
  Objective:"No sockets, host effects or independent device claims",
  CadenceMinutes:30,
  MonitoringPlanJSON:json.RawMessage(`{"version":1,"source_ids":["router"]}`),
 });if err!=nil{t.Fatal(err)}
 const reopenCount=4
 const togglesPerOpen=160
 expectedRevision:=int64(0)
 start:=time.Now()
 maximumHeap:=uint64(0)
 peakPinnedWALBytes:=int64(0)
 for generation:=0;generation<reopenCount;generation++{
  // Hold the first read transaction at an older snapshot while another
  // connection commits 160 independent append-only audit events. WAL
  // readers must neither block the writer nor see uncommitted/new rows.
  var snapshot *sql.Tx
  if generation==0{
   snapshot,err=db.SQL().BeginTx(ctx,&sql.TxOptions{ReadOnly:true})
   if err!=nil{t.Fatal(err)}
   var firstCount int64
   err=snapshot.QueryRowContext(ctx,`SELECT COUNT(*) FROM events
    WHERE aggregate_type='project_goal_tcp_observation_fence'
      AND aggregate_id=?`,goal.ID).Scan(&firstCount)
   if err!=nil||firstCount!=0{t.Fatalf("initial WAL snapshot: %d %v",firstCount,err)}
  }
  for i:=0;i<togglesPerOpen;i++{
   status:="paused";if expectedRevision%2==1{status="running"}
   state,e:=svc.SetGoalTCPObservationFence(ctx,SetGoalTCPObservationFenceCommand{
    ProjectID:p.ID,GoalID:goal.ID,ActorPrincipalID:"owner",
    TargetStatus:status,ExpectedGoalRevision:goal.Revision,
    ExpectedFenceRevision:expectedRevision,
   })
   if e!=nil{t.Fatalf("generation %d toggle %d: %v",generation,i,e)}
   expectedRevision++
   if state.Revision!=expectedRevision||state.Status!=status||
    state.ObservationAuthorized{
    t.Fatalf("lost CAS or invented consent: %+v",state)
   }
   if i%40==0{
    var mem runtime.MemStats
    runtime.ReadMemStats(&mem)
    if mem.HeapAlloc>maximumHeap{maximumHeap=mem.HeapAlloc}
   }
   if snapshot!=nil&&i%32==0{
    var count int64
    e=snapshot.QueryRowContext(ctx,`SELECT COUNT(*) FROM events
     WHERE aggregate_type='project_goal_tcp_observation_fence'
       AND aggregate_id=?`,goal.ID).Scan(&count)
    if e!=nil||count!=0{
     t.Fatalf("pinned read snapshot saw later writes: %d %v",count,e)
    }
    st,e:=os.Stat(path+"-wal")
    if e!=nil&&!os.IsNotExist(e){t.Fatal(e)}
    if e==nil&&st.Size()>peakPinnedWALBytes{peakPinnedWALBytes=st.Size()}
   }
  }
  if snapshot!=nil{
   if err:=snapshot.Rollback();err!=nil{t.Fatal(err)}
   if peakPinnedWALBytes==0||peakPinnedWALBytes>32<<20{
    t.Fatalf("pinned-reader WAL bounded-growth check failed: %d",peakPinnedWALBytes)
   }
  }
  if err:=db.Close();err!=nil{t.Fatal(err)}
  closed=true
  reopened,e:=sqliteStore.Open(path);if e!=nil{t.Fatal(e)}
  db=reopened;closed=false
  if e=db.Migrate(ctx);e!=nil{t.Fatal(e)}
  svc=NewService(db.SQL(),db,clock.Real{})
  persisted,e:=svc.GetGoalTCPObservationFence(ctx,p.ID,goal.ID)
  if e!=nil||persisted.Revision!=expectedRevision||
   persisted.Status!="running"||persisted.ObservationAuthorized{
   t.Fatalf("restart %d lost durable control: %+v %v",generation,persisted,e)
  }
  history,e:=svc.GoalRetentionInventory(ctx,p.ID,goal.ID)
  if e!=nil||history.ObserverFenceEvents!=expectedRevision||
   history.CurrentFenceRevision!=expectedRevision||
   history.TCPAttempts!=0||history.TCPPermits!=0||
   history.TCPRecurringGrants!=0||history.AutomaticPruningEnabled{
   t.Fatalf("restart %d lost audit/authority boundary: %+v %v",generation,history,e)
  }
 }
 var auditRows int64
 err=db.SQL().QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_type='project_goal_tcp_observation_fence'
   AND aggregate_id=?`,goal.ID).Scan(&auditRows)
 if err!=nil||auditRows!=expectedRevision{
  t.Fatalf("append-only history lost: %d != %d: %v",auditRows,expectedRevision,err)
 }
 var result string
 if err:=db.SQL().QueryRowContext(ctx,"PRAGMA quick_check").Scan(&result);
  err!=nil||result!="ok"{
  t.Fatalf("SQLite quick_check failed: %s %v",result,err)
 }
 var journalMode string
 if err:=db.SQL().QueryRowContext(ctx,"PRAGMA journal_mode").Scan(&journalMode);
  err!=nil||journalMode!="wal"{
  t.Fatalf("durable WAL mode changed: %s %v",journalMode,err)
 }
 totalBytes:=int64(0)
 fileBytes:=map[string]int64{}
 for _,suffix:=range []string{"","-wal","-shm"}{
  st,e:=os.Stat(path+suffix)
  if os.IsNotExist(e){fileBytes[suffix]=0;continue}
  if e!=nil{t.Fatal(e)}
  fileBytes[suffix]=st.Size()
  totalBytes+=st.Size()
 }
 if fileBytes[""]==0||totalBytes>64<<20{
  t.Fatalf("unexpected SQLite storage growth after %d writes: %v",expectedRevision,fileBytes)
 }
 // This threshold is a hosted-runner cap, not an assertion about RSS on
 // old user hardware. Report both the Go peak sample and Linux max RSS.
 if maximumHeap>256<<20{
  t.Fatalf("Go heap sample exceeded 256MiB: %d bytes",maximumHeap)
 }
 var usage syscall.Rusage
 if err:=syscall.Getrusage(syscall.RUSAGE_SELF,&usage);err!=nil{t.Fatal(err)}
 maxRSSBytes:=usage.Maxrss*1024 // Linux reports KiB, NOT raw bytes.
 t.Logf("soak_stats=%s",func()string{
  v,_:=json.Marshal(map[string]any{
   "schema":"onepane.l5.observer_soak.v1","restarts":reopenCount,
   "toggles":expectedRevision,"event_rows":auditRows,
   "db_bytes":fileBytes[""],"wal_bytes":fileBytes["-wal"],
   "shm_bytes":fileBytes["-shm"],"max_heap_sample_bytes":maximumHeap,
   "max_rss_bytes":maxRSSBytes,"peak_pinned_wal_bytes":peakPinnedWALBytes,
   "elapsed_ms":time.Since(start).Milliseconds(),
   "physical_node_accepted":false,"network_attempts":0,
  })
  return string(v)
 }())
 if maxRSSBytes>1024<<20{
  t.Fatal(fmt.Sprintf("hosted process RSS exceeded 1GiB: %d",maxRSSBytes))
 }
}
