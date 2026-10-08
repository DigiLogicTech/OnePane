package localai

import (
    "strings"
    "testing"
)

func TestExperimentalPlacementHonoursRequestedHardware(t *testing.T) {
    const gib = int64(1 << 30)
    p := HardwareProfile{
        CPU: CPU{Name: "QA CPU"},
        Memory: Memory{AvailableBytes: 2*gib},
        GPUs: []GPU{{Name: "QA GPU", Vendor:"NVIDIA", DeviceIndex:0, VRAMBytes:4*gib, Backends:[]string{"cuda"}}},
    }
    scenarios:=[]struct{
        name string; pref PlacementMode; want PlacementMode; backend string
    }{
        {"auto uses hybrid under memory pressure", "", PlacementCPUOffload,"cuda"},
        {"explicit GPU remains GPU", PlacementSingleDevice,PlacementSingleDevice,"cuda"},
        {"explicit hybrid remains hybrid", PlacementCPUOffload,PlacementCPUOffload,"cuda"},
        {"explicit CPU remains CPU", PlacementCPUOnly,PlacementCPUOnly,"cpu"},
    }
    for _,tc:=range scenarios{
        t.Run(tc.name,func(t *testing.T){
            plan,_,_,err:=experimentalPlacement(p,12*gib,tc.pref)
            if err!=nil{t.Fatal(err)}
            if plan.Mode!=tc.want||plan.Backend!=tc.backend||!plan.Experimental{
                t.Fatalf("unexpected override plan: %+v",plan)
            }
        })
    }
    noGPU:=p
    noGPU.GPUs=nil
    if _,_,_,err:=experimentalPlacement(noGPU,12*gib,PlacementSingleDevice);err==nil||!strings.Contains(err.Error(),"GPU"){
        t.Fatalf("GPU-only override must reject missing compatible hardware: %v",err)
    }
    if plan,_,_,err:=experimentalPlacement(noGPU,12*gib,"");err!=nil||plan.Mode!=PlacementCPUOnly{
        t.Fatalf("Auto may try CPU when no GPU exists: %+v %v",plan,err)
    }
}

func TestColibriProvisionalContext(t *testing.T) {
    tests:=[]struct{data string; declared,initial int64}{
        {`{"max_position_embeddings":32768}`,32768,32768},
        {`{"text_config":{"max_position_embeddings":262144}}`,262144,131072},
        {`{"max_position_embeddings":100000000000000001}`,0,8192},
        {`{"architectures":["Unknown"]}`,0,8192},
    }
    for _,tc:=range tests{
        declared:=colibriDeclaredContext([]byte(tc.data))
        if declared!=tc.declared{t.Errorf("declared %s: got %d, want %d",tc.data,declared,tc.declared)}
        if got:=initialColibriContext(declared);got!=tc.initial{t.Errorf("initial %s: got %d, want %d",tc.data,got,tc.initial)}
    }
}
