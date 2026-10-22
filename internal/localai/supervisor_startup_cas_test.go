package localai

import (
 "context"
 "os"
 "strings"
 "testing"
)

func startingCASFixture(t *testing.T)(*RuntimeSupervisor,RuntimeInstance){
 t.Helper()
 svc,db:=stopLeaseFixture(t,"starting")
 if _,err:=db.Exec(`UPDATE local_runtime_instances SET pid=23456 WHERE id='inst'`);err!=nil{t.Fatal(err)}
 inst,err:=svc.Instance(context.Background(),"model")
 if err!=nil{t.Fatal(err)}
 return svc,inst
}

func TestManagedStartupHealthCASRejectsChangedRevisionAndPID(t *testing.T){
 ctx:=context.Background()
 for _,tc:=range []struct{name,mutation string}{
  {"stopped",`UPDATE local_runtime_instances SET status='stopped' WHERE id='inst'`},
  {"orphaned",`UPDATE local_runtime_instances SET status='orphaned' WHERE id='inst'`},
  {"new revision",`UPDATE local_runtime_instances SET revision=revision+1 WHERE id='inst'`},
  {"new PID",`UPDATE local_runtime_instances SET pid=4521 WHERE id='inst'`},
  {"new fingerprint",`UPDATE local_runtime_instances SET process_fingerprint='new-engine' WHERE id='inst'`},
 }{
  t.Run(tc.name,func(t *testing.T){
   svc,inst:=startingCASFixture(t)
   if _,err:=svc.db.Exec(tc.mutation);err!=nil{t.Fatal(err)}
   if err:=svc.confirmStartingHealthy(ctx,inst,map[string]any{"status":"ok"});err==nil{
    t.Fatal("stale health observation overwrote newer ownership")
   }
   var status,health string
   if err:=svc.db.QueryRow(`SELECT status,health_json FROM local_runtime_instances WHERE id='inst'`).Scan(&status,&health);err!=nil{t.Fatal(err)}
   if status=="healthy"||health!="{}"{t.Fatalf("stale ready probe changed state: %s %q",status,health)}
  })
 }
}

func TestManagedStartupFailureCASRejectsNewOwner(t *testing.T){
 svc,inst:=startingCASFixture(t)
 if _,err:=svc.db.Exec(`UPDATE local_runtime_instances SET status='orphaned',revision=revision+1, failure_reason='owner unknown' WHERE id='inst'`);err!=nil{t.Fatal(err)}
 if err:=svc.failStartingInstance(context.Background(),inst,"timeout");err==nil{
  t.Fatal("stale startup timeout overwrote orphaned state")
 }
 var status,reason string
 if err:=svc.db.QueryRow(`SELECT status,failure_reason FROM local_runtime_instances WHERE id='inst'`).Scan(&status,&reason);err!=nil{t.Fatal(err)}
 if status!="orphaned"||reason!="owner unknown"{t.Fatalf("replaced process ownership was rewritten: %s %s",status,reason)}
}

func TestManagedStartupHealthAndFailureCASHaveSingleWinner(t *testing.T){
 ctx:=context.Background()
 svc,inst:=startingCASFixture(t)
 if err:=svc.confirmStartingHealthy(ctx,inst,map[string]any{"status":"ready","slots_idle":1});err!=nil{t.Fatal(err)}
 if err:=svc.failStartingInstance(ctx,inst,"late timed-out health");err==nil{
  t.Fatal("old timeout demoted healthy model")
 }
 current,err:=svc.Instance(ctx,"model")
 if err!=nil{t.Fatal(err)}
 if current.Status!=RuntimeHealthy||current.Revision!=inst.Revision+1||!strings.Contains(string(current.HealthJSON),"ready"){
  t.Fatalf("managed runtime admission not persisted correctly: %+v",current)
 }
 svc2,original:=startingCASFixture(t)
 if err:=svc2.failStartingInstance(ctx,original,"original failed");err!=nil{t.Fatal(err)}
 if err:=svc2.confirmStartingHealthy(ctx,original,map[string]any{"status":"ok"});err==nil{
  t.Fatal("late ready health resurrected failed runtime")
 }
 current,err=svc2.Instance(ctx,"model")
 if err!=nil{t.Fatal(err)}
 if current.Status!=RuntimeFailed{t.Fatalf("failed managed startup unexpectedly revived: %s",current.Status)}
}

func TestStartupHealthRechecksProcessIdentityAfterHTTPBeforePersist(t *testing.T){
 source,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 body:=string(source)
 from:=strings.Index(body,"func (s *RuntimeSupervisor) waitHealthy(")
 to:=strings.Index(body,"func (s *RuntimeSupervisor) Health(")
 if from<0||to<=from{t.Fatal("managed health and startup boundaries missing")}
 section:=body[from:to]
 before:=strings.Index(section,"s.VerifyIdentity(ctx,inst)")
 probe:=strings.Index(section,"s.Health(ctx, inst)")
 after:=strings.LastIndex(section,"s.VerifyIdentity(ctx,inst)")
 commit:=strings.Index(section,"s.confirmStartingHealthy(ctx,inst,health)")
 if before<0||probe<=before||after<=probe||commit<=after{
  t.Fatal("a startup health response can be persisted without post-HTTP process identity revalidation")
 }
}
