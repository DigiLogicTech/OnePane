package localai

import (
 "strings"
 "testing"
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
