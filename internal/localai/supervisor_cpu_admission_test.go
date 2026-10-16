package localai

import "testing"

func TestCPUModelFootprintReservesRAMBeforeAdmission(t *testing.T){
 gib:=int64(1)<<30
 mib:=int64(1)<<20
 for _,tc:=range []struct{name string;need,total int64;fit bool}{
  {"small host usable",400*mib,512*mib,true},
  {"small host leaves os reserve",500*mib,512*mib,false},
  {"8 GiB fits",7*gib,8*gib,true},
  {"8 GiB overcommit",8*gib,8*gib,false},
  {"16 GiB still protects OS",15*gib,16*gib,true},
  {"16 GiB exceeds budget",16*gib,16*gib,false},
  {"zero estimate",0,8*gib,false},
  {"negative estimate",-1,8*gib,false},
  {"unknown host",3*gib,0,false},
  {"negative host",3*gib,-1,false},
 }{
  t.Run(tc.name,func(t *testing.T){
   if got:=cpuModelFitsPhysicalRAM(tc.need,tc.total);got!=tc.fit{
    t.Fatalf("CPU plan admission: need=%d total=%d got=%v want=%v",
     tc.need,tc.total,got,tc.fit)
   }
  })
 }
}

func TestCPUConcurrentModelFootprintsDoNotOvercommitPhysicalRAM(t *testing.T){
 gib:=int64(1)<<30
 for _,tc:=range []struct{name string;need,total,resident int64;fit bool}{
  {"single model",4*gib,8*gib,0,true},
  {"second model fits",2*gib,8*gib,5*gib,true},
  {"two models exceed budget",3*gib,8*gib,5*gib,false},
  {"reserve matters",1*gib,8*gib,7*gib,false},
  {"already over budget",1*gib,8*gib,8*gib,false},
  {"invalid negative residency",1*gib,8*gib,-1,false},
  {"overflow protected",1<<62,1<<62,1<<62,false},
 }{
  t.Run(tc.name,func(t *testing.T){
   if got:=cpuModelFitsConcurrentRAM(tc.need,tc.total,tc.resident);got!=tc.fit{
    t.Fatalf("need=%d total=%d resident=%d fit=%v want=%v",tc.need,tc.total,tc.resident,got,tc.fit)
   }
  })
 }
}
