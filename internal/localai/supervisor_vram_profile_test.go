package localai

import (
 "math"
 "os"
 "strings"
 "testing"
)

func TestManagedVRAMHeadroomArithmeticNeverOverflows(t *testing.T){
 const max=int64(math.MaxInt64)
 for _,tc:=range []struct{name string;total int64;reserve int;usable int64;ok bool}{
  {"empty accelerator",0,10,0,true},
  {"full physical capacity",1024,0,1024,true},
  {"10 percent headroom",1000,10,900,true},
  {"50 percent headroom",1000,50,500,true},
  {"retention floor",19,10,17,true},
  {"large 0 percent",max,0,max,true},
  {"large 50 percent",max,50,max/2,true},
  {"large 10 percent",max,10,(max/100)*90+((max%100)*90)/100,true},
  {"negative memory",-1,10,0,false},
  {"negative reserve",100,-1,0,false},
  {"reserve beyond cap",100,51,0,false},
 }{
  t.Run(tc.name,func(t *testing.T){
   got,ok:=usableManagedVRAM(tc.total,tc.reserve)
   if got!=tc.usable||ok!=tc.ok{
    t.Fatalf("total=%d pct=%d got=%d ok=%v want=%d %v",tc.total,tc.reserve,got,ok,tc.usable,tc.ok)
   }
   if ok&&(got<0||got>tc.total){t.Fatal("computed VRAM capacity was outside physical range")}
  })
 }
}

// A malformed live resident plan cannot silently disappear from occupancy.
func TestCapacityAdmissionRefusesUnknownResidentPlacement(t *testing.T){
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 source:=string(raw)
 start:=strings.Index(source,"func (s *RuntimeSupervisor) ensureCapacity(")
 if start<0{t.Fatal("capacity admission unavailable")}
 scope:=source[start:]
 if end:=strings.Index(scope,"\nfunc ");end>0{scope=scope[:end]}
 deny:=strings.Index(scope,`return errors.New("active managed runtime placement is unverified; reconcile or reinstall model plan")`)
 reclaim:=strings.Index(scope,"capacityCouldFitAfterIdleReclaim(")
 stop:=strings.Index(scope,"s.stopIfIdleBeforeLocked(")
 if deny<0||reclaim<=deny||stop<=reclaim{
  t.Fatal("unknown resident GPU allocation may fabricate headroom or permit eviction")
 }
}
