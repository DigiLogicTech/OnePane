// Package nodepreflight reports physical Node prerequisites without starting,
// pulling, removing or inspecting any running user containers. Prerequisite
// success is NOT physical acceptance or permission to enable a Tool.
package nodepreflight

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "os"
 "os/exec"
 "regexp"
 "runtime"
 "strings"
 "time"
)

const (
 RootlessImageEnv = "ONEPANE_ROOTLESS_SMOKE_IMAGE"
 GodotImageEnv = "ONEPANE_GODOT_SMOKE_IMAGE"
 PythonImageEnv = "ONEPANE_LARGE_ARTIFACT_SMOKE_IMAGE"
)
var ApprovedVariables = []string{RootlessImageEnv,GodotImageEnv,PythonImageEnv}
var immutableImage = regexp.MustCompile(`^[A-Za-z0-9._:/-]+@sha256:[a-fA-F0-9]{64}$`)

type Inspector interface{
 RootlessPodman(context.Context)(bool,error)
 ImagePresent(context.Context,string)(bool,error)
}
type ImageCheck struct{
 Variable string `json:"variable"`
 State string `json:"state"`
 DigestFingerprint string `json:"digest_fingerprint,omitempty"`
}
type Report struct {
 Schema int `json:"schema"`
 NodeOS string `json:"node_os"`
 Podman string `json:"podman"`
 ImageChecks []ImageCheck `json:"image_checks"`
 ReadyForPhysicalTest bool `json:"ready_for_physical_test"`
 PhysicalAcceptanceVerified bool `json:"physical_acceptance_verified"`
}

var ErrInvalidVariable=errors.New("unapproved image environment variable")
func allowedVariable(v string)bool{
 for _,allowed:=range ApprovedVariables{if v==allowed{return true}}
 return false
}
func requestedVariables(required string)([]string,error){
 if required==""{return append([]string(nil),ApprovedVariables...),nil}
 if !allowedVariable(required){return nil,ErrInvalidVariable}
 return []string{required},nil
}
func fingerprint(ref string)string{
 hash:=sha256.Sum256([]byte(ref))
 return hex.EncodeToString(hash[:6])
}
// Check never prints input OCI image names, credentials, engine diagnostics,
// user or host identity. A failed check never calls run/pull/exec/network.
func Check(ctx context.Context,inspector Inspector,getenv func(string)string,osName,required string)(Report,error){
 variables,err:=requestedVariables(required)
 if err!=nil{return Report{},err}
 report:=Report{Schema:1,NodeOS:osName,Podman:"unchecked",
  ImageChecks:make([]ImageCheck,0,len(variables)),
  PhysicalAcceptanceVerified:false}
 if inspector==nil||getenv==nil{return Report{},errors.New("Node inspector unavailable")}
 if osName!="linux"{
  report.Podman="unsupported_platform"
 }else{
  // Bound command duration even if a broken rootless engine wedges.
  probeCtx,cancel:=context.WithTimeout(ctx,8*time.Second)
  ok,probeErr:=inspector.RootlessPodman(probeCtx)
  cancel()
  switch{
  case probeErr!=nil:report.Podman="unavailable"
  case !ok:report.Podman="not_rootless"
  default:report.Podman="rootless"
  }
 }
 report.ReadyForPhysicalTest=report.Podman=="rootless"
 for _,variable:=range variables{
  ref:=getenv(variable)
  check:=ImageCheck{Variable:variable,State:"approval_missing"}
  switch{
  case ref=="":
  case strings.TrimSpace(ref)!=ref||!immutableImage.MatchString(ref):
   check.State="invalid_immutable_reference"
  case report.Podman!="rootless":
   check.State="node_unavailable"
  default:
   check.DigestFingerprint=fingerprint(ref)
   imageCtx,cancel:=context.WithTimeout(ctx,8*time.Second)
   exists,e:=inspector.ImagePresent(imageCtx,ref)
   cancel()
   if e!=nil{check.State="image_check_failed"
   }else if !exists{check.State="image_not_present"
   }else{check.State="locally_present"}
  }
  if check.State!="locally_present"{report.ReadyForPhysicalTest=false}
  report.ImageChecks=append(report.ImageChecks,check)
 }
 return report,nil
}
// LocalPodman only uses read-only info and image existence operations. The
// presence of a rootless Docker binary cannot silently substitute for Podman.
type LocalPodman struct{}
func(LocalPodman) RootlessPodman(ctx context.Context)(bool,error){
 if runtime.GOOS!="linux"||os.Geteuid()==0{
  return false,errors.New("Linux unprivileged process required")
 }
 executable,err:=exec.LookPath("podman")
 if err!=nil{return false,err}
 out,err:=exec.CommandContext(ctx,executable,"info","--format","{{.Host.Security.Rootless}}").Output()
 if err!=nil{return false,err}
 return strings.TrimSpace(string(out))=="true",nil
}
func(LocalPodman) ImagePresent(ctx context.Context,ref string)(bool,error){
 if !immutableImage.MatchString(ref){return false,errors.New("immutable digest required")}
 executable,err:=exec.LookPath("podman")
 if err!=nil{return false,err}
 err=exec.CommandContext(ctx,executable,"image","exists",ref).Run()
 if err==nil{return true,nil}
 var status *exec.ExitError
 if errors.As(err,&status)&&status.ExitCode()==1{return false,nil}
 return false,err
}
