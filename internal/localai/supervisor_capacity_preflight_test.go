package localai

import "testing"

func TestCapacityPreflightProtectsExistingModelsOnImpossiblePlacement(t *testing.T){
 idle:=func(id string,bytes int64)residentRuntime{
  return residentRuntime{Placement:PlacementPlan{Devices:[]PlacementDevice{{
   Kind:"accelerator",DeviceID:id,AllocatedBytes:bytes,
  }}}}
 }
 for _,tc:=range []struct{
  name string
  used,target,capacity map[string]int64
  idle []residentRuntime
  override bool
  allow bool
 }{
  {"all memory busy",map[string]int64{"gpu":8},map[string]int64{"gpu":4},
   map[string]int64{"gpu":10},nil,false,false},
  {"healthy idle releases enough",map[string]int64{"gpu":8},map[string]int64{"gpu":4},
   map[string]int64{"gpu":10},[]residentRuntime{idle("gpu",4)},false,true},
  {"eviction still cannot fit busy peer",map[string]int64{"gpu":9},map[string]int64{"gpu":5},
   map[string]int64{"gpu":10},[]residentRuntime{idle("gpu",2)},false,false},
  {"target itself too large",map[string]int64{"gpu":4},map[string]int64{"gpu":12},
   map[string]int64{"gpu":10},[]residentRuntime{idle("gpu",4)},false,false},
  {"multi gpu busy second device",map[string]int64{"gpu0":8,"gpu1":9},
   map[string]int64{"gpu0":4,"gpu1":5},
   map[string]int64{"gpu0":10,"gpu1":10},
   []residentRuntime{idle("gpu0",4),idle("gpu1",2)},false,false},
  {"multi gpu both recover",map[string]int64{"gpu0":8,"gpu1":9},
   map[string]int64{"gpu0":4,"gpu1":5},
   map[string]int64{"gpu0":10,"gpu1":10},
   []residentRuntime{idle("gpu0",4),idle("gpu1",4)},false,true},
  {"explicit override requires no other resident",map[string]int64{"gpu":8},
   map[string]int64{"gpu":20},map[string]int64{"gpu":10},
   []residentRuntime{idle("gpu",3)},true,false},
  {"explicit override after full release",map[string]int64{"gpu":8},
   map[string]int64{"gpu":20},map[string]int64{"gpu":10},
   []residentRuntime{idle("gpu",8)},true,true},
 }{
  t.Run(tc.name,func(t *testing.T){
   got:=capacityCouldFitAfterIdleReclaim(tc.used,tc.target,tc.capacity,tc.idle,tc.override)
   if got!=tc.allow{t.Fatalf("capacity plan=%v want=%v",got,tc.allow)}
   // Dry-run admission must never consume or mutate source accounting.
   if tc.used["gpu"]<0||tc.used["gpu0"]<0{t.Fatal("negative original residency")}
  })
 }
}
