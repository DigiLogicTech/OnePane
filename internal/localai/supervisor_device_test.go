package localai

import (
 "strings"
 "testing"
 "os"
 "path/filepath"
)

func TestValidateLlamaPlacementRejectsMismatchedBackend(t *testing.T) {
 plan:=PlacementPlan{Mode:PlacementSingleDevice,Devices:[]PlacementDevice{{
  Kind:"accelerator",Backend:"cuda",RuntimeDevice:"CUDA0",DeviceIndex:0,
 }}}
 err:=validateLlamaPlacementDevices("unused","vulkan",plan)
 if err==nil||!strings.Contains(err.Error(),"cannot address device CUDA0"){
  t.Fatalf("expected explicit backend mismatch, got %v",err)
 }
}

func TestValidateLlamaCPUPlacementNeedsNoDeviceProbe(t *testing.T) {
 plan:=PlacementPlan{Mode:PlacementCPUOnly}
 if err:=validateLlamaPlacementDevices("unused","cpu",plan);err!=nil{t.Fatal(err)}
}

func TestChooseManagedLlamaExecutableKeepsPinAndRepairsExactBackend(t *testing.T){
 root:=t.TempDir()
 write:=func(name string)string{
  p:=filepath.Join(root,"llamacpp",name,"llama-server")
  if err:=os.MkdirAll(filepath.Dir(p),0700);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(p,[]byte("stub"),0700);err!=nil{t.Fatal(err)}
  return p
 }
 old:=write("b11430/cuda")
 registered:=write("b11430/cuda-verified-generation")
 same,err:=chooseManagedLlamaExecutable(old,registered,"llamacpp@cuda","cuda",root)
 if err!=nil||same!=old{t.Fatalf("must preserve available pinned executable: %s %v",same,err)}
 if err:=os.Remove(old);err!=nil{t.Fatal(err)}
 repaired,err:=chooseManagedLlamaExecutable(old,registered,"llamacpp@cuda","cuda",root)
 if err!=nil||repaired!=registered{t.Fatalf("expected verified exact CUDA replacement, got %q: %v",repaired,err)}
 if _,err:=chooseManagedLlamaExecutable(old,registered,"llamacpp@vulkan","cuda",root);err==nil{
  t.Fatal("must not silently substitute Vulkan for CUDA")
 }
 if _,err:=chooseManagedLlamaExecutable(old,filepath.Join(t.TempDir(),"outside"),"llamacpp@cuda","cuda",root);err==nil{
  t.Fatal("must not launch an executable outside trusted runtime root")
 }
 if err:=os.Remove(registered);err!=nil{t.Fatal(err)}
 if _,err:=chooseManagedLlamaExecutable(old,registered,"llamacpp@cuda","cuda",root);err==nil||!strings.Contains(err.Error(),"repair or reinstall"){
  t.Fatalf("expected actionable missing binary error; got %v",err)
 }
}
