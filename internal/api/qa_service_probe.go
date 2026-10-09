package api

import (
 "bytes"
 "context"
 "os/exec"
 "path/filepath"
 "strings"
 "time"
)

// Service manager checks are restricted to the local OnePane daemon, never
// an arbitrary service, Node endpoint or user-provided executable. Do not
// capture stderr, arguments, command paths, usernames, logs or process IDs.
const qaServiceProbeTimeout=2*time.Second
const qaServiceOutputLimit=4096

type qaOSServiceObservation struct {
 Manager string `json:"manager"`
 Collection string `json:"collection"`
 State string `json:"state"`
 ObservedAtMS *int64 `json:"observed_at_ms,omitempty"`
}

type qaServiceOutput struct{data bytes.Buffer}
func (w *qaServiceOutput)Write(p []byte)(int,error){
 original:=len(p)
 remaining:=qaServiceOutputLimit-w.data.Len()
 if remaining>0{
  if len(p)>remaining{p=p[:remaining]}
  _,_=w.data.Write(p)
 }
 return original,nil
}

// cmd runs without a shell, has fixed arguments and a 2-second deadline.
// The output buffer never grows beyond 4 KiB, irrespective of child output.
// Executable paths are absolute and never resolved through the user PATH.
func qaRunServiceCommand(parent context.Context,executable string,args ...string)(string,string){
 if !filepath.IsAbs(executable){return "","unavailable"}
 ctx,cancel:=context.WithTimeout(parent,qaServiceProbeTimeout)
 defer cancel()
 cmd:=exec.CommandContext(ctx,executable,args...)
 var stdout qaServiceOutput
 cmd.Stdout=&stdout
 cmd.Stderr=nil // os/exec discards stderr; never collect service diagnostics
 err:=cmd.Run()
 if ctx.Err()==context.DeadlineExceeded{return "","timeout"}
 if err!=nil{return "","unavailable"}
 return stdout.data.String(),"observed"
}

func qaServiceUnavailable(manager,collection string)qaOSServiceObservation{
 if manager!="systemd"&&manager!="windows_scm"{manager="not_collected"}
 if collection!="timeout"&&collection!="unavailable"&&collection!="not_collected"{
  collection="not_collected"
 }
 return qaOSServiceObservation{Manager:manager,Collection:collection,State:"not_collected"}
}
func qaObservedService(manager,state string)qaOSServiceObservation{
 switch state{
 case "running","stopped","starting","stopping","failed","paused","not_installed":
 default:return qaServiceUnavailable(manager,"unavailable")
 }
 now:=time.Now().UTC().UnixMilli()
 return qaOSServiceObservation{Manager:manager,Collection:"observed",State:state,ObservedAtMS:&now}
}

// Fixed allowlist; never return raw service-manager output such as paths,
// descriptions, failure messages, process IDs or user-controlled text.
func qaLinuxServiceState(raw string)(string,bool){
 properties:=make(map[string]string,3)
 for _,line:=range strings.Split(raw,"\n"){
  k,v,ok:=strings.Cut(strings.TrimSpace(line),"=")
  if !ok{continue}
  if k=="LoadState"||k=="ActiveState"||k=="SubState"{properties[k]=strings.TrimSpace(v)}
 }
 load,loaded:=properties["LoadState"]
 active,found:=properties["ActiveState"]
 if !loaded||!found{return "",false}
 if load=="not-found"{return "not_installed",true}
 if load!="loaded"{return "",false}
 switch active{
 case "active":return "running",true // manager reports active, not app readiness
 case "inactive":return "stopped",true
 case "failed":return "failed",true
 case "activating":return "starting",true
 case "deactivating":return "stopping",true
 default:return "",false
 }
}
func qaWindowsServiceState(raw string)(string,bool){
 for _,line:=range strings.Split(raw,"\n"){
  fields:=strings.Fields(strings.TrimSpace(line))
  // Fixed SCM query format: STATE : 4 RUNNING. No error/description text.
  if len(fields)<4||fields[0]!="STATE"||fields[1]!=":"{continue}
  switch fields[3]{
  case "RUNNING":return "running",true
  case "STOPPED":return "stopped",true
  case "START_PENDING","CONTINUE_PENDING":return "starting",true
  case "STOP_PENDING":return "stopping",true
  case "PAUSED","PAUSE_PENDING":return "paused",true
  default:return "",false
  }
 }
 return "",false
}

// Only the exact canonical Node attached to this server process may be
// checked. A remote Node or a DB alias claiming local=true never gains the
// host's service-manager state. Invocation remains behind Node Admin auth.
func qaAttachLocalServiceEvidence(ctx context.Context,report *qaNodeEvidence,requestedID,serverLocalID string,
 probe func(context.Context)qaOSServiceObservation){
 if report==nil||!report.IsLocal||requestedID==""||requestedID!=serverLocalID||probe==nil{return}
 result:=probe(ctx)
 report.ServiceObservation=&result
 report.ServiceState=result.State
 report.CollectionLimits=append(report.CollectionLimits,
  "local OS service-manager state is not proof that backend, models, agents or HTTP dependencies are healthy")
}
