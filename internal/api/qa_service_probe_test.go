package api

import (
 "context"
 "strings"
 "testing"
)

func TestQAServiceStateParsersAreStrictAndRedacted(t *testing.T){
 for _,tc:=range []struct{
  raw string
  state string
  ok bool
 }{
  {"LoadState=loaded\nActiveState=active\nSubState=running\nExecStart=/secret/BearerToken\n","running",true},
  {"LoadState=loaded\nActiveState=failed\nSubState=failed\n","failed",true},
  {"LoadState=loaded\nActiveState=activating\n","starting",true},
  {"LoadState=loaded\nActiveState=inactive\n","stopped",true},
  {"LoadState=not-found\nActiveState=inactive\n","not_installed",true},
  {"LoadState=masked\nActiveState=active\n","",false},
  {"LoadState=loaded\nActiveState=unknown-error-BearerToken\n","",false},
  {"LoadState=loaded\nSecret=x\n","",false},
  {"ActiveState=active\n","",false},
 }{
  got,ok:=qaLinuxServiceState(tc.raw)
  if got!=tc.state||ok!=tc.ok{t.Errorf("systemd expected %q/%v, got %q/%v",tc.state,tc.ok,got,ok)}
  if strings.Contains(got,"BearerToken"){t.Fatal("raw systemctl output was exported")}
 }
 for _,tc:=range []struct{raw,state string;ok bool}{
  {"SERVICE_NAME: OnePane\n        TYPE : 10 WIN32_OWN_PROCESS\n        STATE              : 4  RUNNING\n","running",true},
  {"STATE : 1 STOPPED\n","stopped",true},
  {"STATE : 2 START_PENDING\n","starting",true},
  {"STATE : 3 STOP_PENDING\n","stopping",true},
  {"STATE : 7 PAUSED\n","paused",true},
  {"STATE : 9 BearerPrivateValue\n","",false},
  {"BINARY_PATH_NAME : C:\\SECRET\\onepane.exe\n","",false},
  {"ACCESS_KEY : 4 RUNNING\n","",false},
 }{
  got,ok:=qaWindowsServiceState(tc.raw)
  if got!=tc.state||ok!=tc.ok{t.Errorf("SCM expected %q/%v, got %q/%v",tc.state,tc.ok,got,ok)}
  if strings.Contains(got,"BearerPrivateValue"){t.Fatal("raw SCM output was exported")}
 }
}

func TestQALocalServiceCheckMustMatchRegisteredCanonicalHostNode(t *testing.T){
 calls:=0
 probe:=func(context.Context)qaOSServiceObservation{
  calls++
  return qaObservedService("systemd","running")
 }
 for _,test:=range []struct{
  name string
  nodeID string
  localID string
  registeredLocal bool
  wantCalls int
 }{
  {"valid local Node","local","local",true,1},
  {"remote Node","remote","local",false,0},
  {"forged local DB flag","remote","local",true,0},
  {"unconfigured backend Node","local","",true,0},
  {"local ID but registered remote","local","local",false,0},
 }{
  t.Run(test.name,func(t *testing.T){
   calls=0
   report:=qaNodeEvidence{SchemaVersion:2,IsLocal:test.registeredLocal,ServiceState:"not_collected"}
   qaAttachLocalServiceEvidence(context.Background(),&report,test.nodeID,test.localID,probe)
   if calls!=test.wantCalls{t.Fatalf("unexpected OS command execution: %d",calls)}
   if calls==0{
    if report.ServiceObservation!=nil||report.ServiceState!="not_collected"{
     t.Fatalf("remote Node received host service observation: %+v",report)
    }
   }else if report.ServiceState!="running"||report.ServiceObservation==nil||
    report.ServiceObservation.Collection!="observed"||report.ServiceObservation.Manager!="systemd"||
    report.ServiceObservation.ObservedAtMS==nil{
    t.Fatalf("bad local observation: %+v",report)
   }
  })
 }
}

func TestQAServiceProbeCannotPromoteUnavailableToHealthy(t *testing.T){
 for _,status:=range []string{"unavailable","timeout","not_collected"}{
  report:=qaNodeEvidence{IsLocal:true,ServiceState:"not_collected"}
  qaAttachLocalServiceEvidence(context.Background(),&report,"local","local",
   func(context.Context)qaOSServiceObservation{return qaServiceUnavailable("systemd",status)})
  if report.ServiceState!="not_collected"||report.ServiceObservation==nil||
   report.ServiceObservation.Collection!=status||report.ServiceObservation.ObservedAtMS!=nil{
   t.Fatalf("unavailable OS probe invented health: %+v",report)
  }
 }
 if qaServiceUnavailable("user-provider","running").State!="not_collected"{
  t.Fatal("untrusted service manager accepted")
 }
 if qaObservedService("systemd","Bearer secret").State!="not_collected"{
  t.Fatal("free-text state cannot be converted to a healthy result")
 }
 out:=&qaServiceOutput{}
 secret:=strings.Repeat("NEVER_EXPORT_THIS",1000)
 n,err:=out.Write([]byte(secret))
 if err!=nil||n!=len(secret)||out.data.Len()!=qaServiceOutputLimit{
  t.Fatalf("service stdout size cap failed: %d %v",out.data.Len(),err)
 }
}
