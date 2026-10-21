package localai

import (
 "context"
 "os"
 "strings"
 "testing"
)

func TestRecoveredModelHealthCASRequiresExactOwningGeneration(t *testing.T) {
 for _,tc:=range []struct{name,mutation string}{
  {"stopped during probe",`UPDATE local_runtime_instances SET status='stopped' WHERE id='inst'`},
  {"orphaned during probe",`UPDATE local_runtime_instances SET status='orphaned' WHERE id='inst'`},
  {"process PID changed",`UPDATE local_runtime_instances SET pid=9877 WHERE id='inst'`},
  {"launch revision changed",`UPDATE local_runtime_instances SET revision=revision+1 WHERE id='inst'`},
  {"command fingerprint changed",`UPDATE local_runtime_instances SET process_fingerprint='different' WHERE id='inst'`},
 } {
  t.Run(tc.name,func(t *testing.T){
   svc,db:=stopLeaseFixture(t,"starting")
   if _,err:=db.Exec(`UPDATE local_runtime_instances SET pid=4321 WHERE id='inst'`);err!=nil{t.Fatal(err)}
   inst,err:=svc.Instance(context.Background(),"model")
   if err!=nil{t.Fatal(err)}
   if _,err:=db.Exec(tc.mutation);err!=nil{t.Fatal(err)}
   if err:=svc.confirmRecoveredHealthy(context.Background(),inst,map[string]any{"status":"ok"});err==nil{
    t.Fatal("stale recovered health promoted a different process/state")
   }
   var health string
   if err:=db.QueryRow(`SELECT health_json FROM local_runtime_instances WHERE id='inst'`).Scan(&health);err!=nil{t.Fatal(err)}
   if health!="{}"{t.Fatalf("stale health changed persisted readiness: %s",health)}
  })
 }
}

func TestRecoveredModelHealthCASAcceptsOnlyOriginalActiveProcess(t *testing.T) {
 for _,status:=range []string{"starting","healthy","busy"} {
  t.Run(status,func(t *testing.T){
   svc,db:=stopLeaseFixture(t,status)
   if _,err:=db.Exec(`UPDATE local_runtime_instances SET pid=4321 WHERE id='inst'`);err!=nil{t.Fatal(err)}
   inst,err:=svc.Instance(context.Background(),"model")
   if err!=nil{t.Fatal(err)}
   if err:=svc.confirmRecoveredHealthy(context.Background(),inst,map[string]any{"status":"ready"});err!=nil{t.Fatal(err)}
   var current string
   var health string
   var revision int64
   if err:=db.QueryRow(`SELECT status,health_json,revision FROM local_runtime_instances WHERE id='inst'`).Scan(&current,&health,&revision);err!=nil{t.Fatal(err)}
   if current!="healthy"||!strings.Contains(health,"ready")||revision!=inst.Revision+1{
    t.Fatalf("verified original model not recovered correctly: status=%s rev=%d health=%s",current,revision,health)
   }
  })
 }
 svc,_:=stopLeaseFixture(t,"failed")
 if err:=svc.confirmRecoveredHealthy(context.Background(),RuntimeInstance{ID:"inst",PID:4321,Revision:1,Status:RuntimeFailed,ProcessFingerprint:"identity"},map[string]any{"status":"ok"});err==nil{
  t.Fatal("failed process accepted as recovered resident")
 }
}

func TestRecoveryIdentityIsReverifiedAfterSocketAndBeforeHealthyCAS(t *testing.T){
 src,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 body:=string(src)
 from:=strings.Index(body,"func (s *RuntimeSupervisor) Recover(")
 to:=strings.Index(body,"func argsContainPair(")
 if from<0||to<=from{t.Fatal("recovery boundaries missing")}
 recovery:=body[from:to]
 pre:=strings.Index(recovery,"s.VerifyIdentity(ctx, inst)")
 probe:=strings.Index(recovery,"s.Health(ctx, inst)")
 post:=strings.LastIndex(recovery,"s.VerifyIdentity(ctx, inst)")
 commit:=strings.Index(recovery,"s.confirmRecoveredHealthy(ctx, inst, health)")
 if pre<0||probe<=pre||post<=probe||commit<=post{
  t.Fatal("recovered model can become healthy without rechecking PID ownership after HTTP")
 }
 if !strings.Contains(recovery,"AND pid=? AND process_fingerprint=? AND status=?"){
  t.Fatal("failed recovery process could orphan a replaced model generation")
 }
}
