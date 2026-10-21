package localai

import (
 "context"
 "os"
 "strings"
 "testing"
)

func TestCancelledUnlaunchedModelReconcilesExactGeneration(t *testing.T){
 svc,db:=stopLeaseFixture(t,"starting")
 inst,err:=svc.Instance(context.Background(),"model")
 if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if err:=svc.failUnlaunchedManagedRuntime(ctx,inst);err!=nil{
  t.Fatalf("cancelled caller left unlaunched model starting: %v",err)
 }
 var status,reason string
 var rev int64
 if err:=db.QueryRow(`SELECT status,failure_reason,revision FROM local_runtime_instances WHERE id='inst'`).Scan(&status,&reason,&rev);err!=nil{t.Fatal(err)}
 if status!="failed"||reason==""||rev!=inst.Revision+1{
  t.Fatalf("model startup without process did not fail safely: %s %q %d",status,reason,rev)
 }
}

func TestUnlaunchedModelFailureCannotOverwriteNewPIDOrRevision(t *testing.T){
 for _,mutation:=range []string{
  `UPDATE local_runtime_instances SET pid=555 WHERE id='inst'`,
  `UPDATE local_runtime_instances SET revision=revision+1 WHERE id='inst'`,
  `UPDATE local_runtime_instances SET process_fingerprint='replacement' WHERE id='inst'`,
  `UPDATE local_runtime_instances SET status='orphaned' WHERE id='inst'`,
 }{
  t.Run(mutation,func(t *testing.T){
   svc,db:=stopLeaseFixture(t,"starting")
   inst,err:=svc.Instance(context.Background(),"model")
   if err!=nil{t.Fatal(err)}
   if _,err:=db.Exec(mutation);err!=nil{t.Fatal(err)}
   if err:=svc.failUnlaunchedManagedRuntime(context.Background(),inst);err==nil{
    t.Fatal("original unlaunched start overwrote a newer runtime")
   }
   var status string
   if err:=db.QueryRow(`SELECT status FROM local_runtime_instances WHERE id='inst'`).Scan(&status);err!=nil{t.Fatal(err)}
   if status=="failed"{t.Fatal("changed generation falsely marked failed")}
  })
 }
}

// The model must remain resident across the end of a successful chat. Only
// cancel-before-Start fences a launch; do not pass a request's cancellation
// into exec.CommandContext for a long-lived managed engine.
func TestManagedLauncherCancellationPreflightPrecedesDetachedProcessStart(t *testing.T){
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 source:=string(raw)
 begin:=strings.Index(source,"func (s *RuntimeSupervisor) startLocked(")
 end:=strings.Index(source[begin:],"func (s *RuntimeSupervisor) Acquire(")
 if begin<0||end<0{t.Fatal("supervisor launch body missing")}
 body:=source[begin:begin+end]
 fence:=strings.LastIndex(body,"if cancelErr:=ctx.Err();cancelErr!=nil")
 spawn:=strings.Index(body,"s.processes.Start(context.Background()")
 if fence<0||spawn<fence{t.Fatal("model started before cancellation preflight")}
}
