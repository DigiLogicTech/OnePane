package localai

import (
    "reflect"
    "strings"
    "testing"
)

func TestColibriHotSwapEvictsOnlyIdleModels(t *testing.T) {
    idle:=[]colibriSwapCandidate{
        {ID:"older",Status:RuntimeHealthy,LastUsed:2},
        {ID:"newer",Status:RuntimeHealthy,LastUsed:9},
    }
    out,err:=selectColibriEvictions(idle,map[string]int{})
    if err!=nil{t.Fatal(err)}
    if !reflect.DeepEqual(out,[]string{"older","newer"}){
        t.Fatalf("unexpected idle eviction order: %v",out)
    }
}

func TestColibriHotSwapRefusesBusyBeforeAnyEviction(t *testing.T) {
    tests:=[]struct{
        name string
        candidates []colibriSwapCandidate
        active map[string]int
        errorPart string
    }{
        {"busy marker",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"busy",Status:RuntimeBusy}},nil,"busy"},
        {"active lease despite healthy marker",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"leased",Status:RuntimeHealthy}},map[string]int{"leased":1},"active"},
        {"starting",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"loading",Status:RuntimeStarting}},nil,"starting"},
        {"draining",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"drain",Status:RuntimeDraining}},nil,"draining"},
        {"orphaned",[]colibriSwapCandidate{{ID:"idle",Status:RuntimeHealthy},{ID:"unknown",Status:RuntimeOrphaned}},nil,"orphaned"},
    }
    for _,tt:=range tests{
        t.Run(tt.name,func(t *testing.T){
            out,err:=selectColibriEvictions(tt.candidates,tt.active)
            if err==nil||!strings.Contains(err.Error(),tt.errorPart){
                t.Fatalf("expected %s error, got evictions %v, err %v",tt.errorPart,out,err)
            }
            if len(out)!=0{t.Fatalf("partial eviction allowed: %v",out)}
        })
    }
}

func TestColibriHotSwapNoConflictingModels(t *testing.T) {
    out,err:=selectColibriEvictions(nil,nil)
    if err!=nil||len(out)!=0 {t.Fatalf("unexpected result: out=%v err=%v",out,err)}
}
