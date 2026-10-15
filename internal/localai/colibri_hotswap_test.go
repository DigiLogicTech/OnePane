package localai

import (
    "reflect"
 "context"
    "strings"
    "testing"
)

func TestColibriHotSwapEvictsOnlyOneIdleResident(t *testing.T) {
    idle:=[]colibriSwapCandidate{{ID:"sole",Status:RuntimeHealthy,LastUsed:2}}
    out,err:=selectColibriEvictions(idle,map[string]int{})
    if err!=nil||!reflect.DeepEqual(out,[]string{"sole"}){
        t.Fatalf("single safely idle Colibri not selected: %v %v",out,err)
    }
}

func TestColibriHotSwapRefusesAbnormalMultipleIdleResidents(t *testing.T){
    peers:=[]colibriSwapCandidate{
        {ID:"older",Status:RuntimeHealthy,LastUsed:2},
        {ID:"newer",Status:RuntimeHealthy,LastUsed:9},
    }
    out,err:=selectColibriEvictions(peers,map[string]int{})
    if err==nil||!strings.Contains(err.Error(),"multiple Colibri")||len(out)!=0{
        t.Fatalf("multiple resident Colibri would be evicted partially: %v %v",out,err)
    }
}

func TestColibriHotSwapRefusesBusyBeforeAnyEviction(t *testing.T) {
    tests:=[]struct{
        name string
        candidates []colibriSwapCandidate
        active map[string]int
        errorPart string
    }{
        {"busy marker",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"busy",Status:RuntimeBusy}},nil,"busy"},
        {"active lease despite healthy marker",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"leased",Status:RuntimeHealthy}},map[string]int{"leased":1},"active"},
        {"starting",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"loading",Status:RuntimeStarting}},nil,"starting"},
        {"draining",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"drain",Status:RuntimeDraining}},nil,"draining"},
        {"orphaned",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"unknown",Status:RuntimeOrphaned}},nil,"orphaned"},
    }
    for _,tt:=range tests{
        t.Run(tt.name,func(t *testing.T){
            out,err:=selectColibriEvictions(tt.candidates,tt.active)
            if err==nil||!strings.Contains(err.Error(),tt.errorPart){
                t.Fatalf("expected %s error, got evictions %v, err %v",tt.errorPart,out,err)
            }
            if len(out)!=0{t.Fatalf("partial eviction allowed: %v",out)}
        })
    }
}

func TestColibriHotSwapNoConflictingModels(t *testing.T) {
    out,err:=selectColibriEvictions(nil,nil)
    if err!=nil||len(out)!=0 {t.Fatalf("unexpected result: out=%v err=%v",out,err)}
}

func TestColibriSwapStatusRejectsMissingSupervisor(t *testing.T){
 ctx:=context.Background()
 var missing *Service
 if _,err:=missing.ColibriHotSwapStatus(ctx,"model");err==nil{
  t.Fatal("nil local runtime service allowed status inspection")
 }
 svc:=&Service{}
 if _,err:=svc.ColibriHotSwapStatus(ctx,"model");err==nil{
  t.Fatal("unavailable runtime supervisor caused a nil dereference")
 }
}

func TestColibriSwapStatusPreservesFailedVersusStoppedInstance(t *testing.T){
 db:=colibriPinTestDB(t)
 defer db.Close()
 for _,query:=range []string{
  `CREATE TABLE local_runtime_instances(
   deployment_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,
   status TEXT NOT NULL,last_seen_at INTEGER,started_at INTEGER,
   updated_at INTEGER NOT NULL)`,
  `INSERT INTO local_runtime_instances
   VALUES('colibri-a','node-1','failed',3,1,5)`,
  `INSERT INTO local_runtime_instances
   VALUES('colibri-b','node-1','healthy',4,2,6)`,
 }{
  if _,err:=db.Exec(query);err!=nil{t.Fatal(err)}
 }
 service:=&Service{db:db,supervisor:&RuntimeSupervisor{db:db}}
 ctx:=context.Background()
 failed,err:=service.ColibriHotSwapStatus(ctx,"colibri-a")
 if err!=nil{t.Fatal(err)}
 if failed.Status!="failed"||failed.Message==""||
  !reflect.DeepEqual(failed.ActiveDeploymentIDs,[]string{"colibri-b"}){
  t.Fatalf("failed engine was hidden or healthy neighbour misplaced: %+v",failed)
 }
 if _,err=db.Exec(`UPDATE local_runtime_instances SET status='stopped'
   WHERE deployment_id='colibri-a'`);err!=nil{t.Fatal(err)}
 stopped,err:=service.ColibriHotSwapStatus(ctx,"colibri-a")
 if err!=nil||stopped.Status!="stopped"||stopped.Message!=""{
  t.Fatalf("operator-requested stop was classified as failure: %+v %v",stopped,err)
 }
 missing,err:=service.ColibriHotSwapStatus(ctx,"colibri-remote")
 if err!=nil||missing.Status!="stopped"||len(missing.ActiveDeploymentIDs)!=0{
  t.Fatalf("other Node's model was included in local Colibri status: %+v %v",missing,err)
 }
}
