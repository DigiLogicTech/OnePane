package localai

import (
    "reflect"
    "strings"
    "testing"
    "context"
    "time"

    "github.com/DigiLogicTech/OnePane/internal/clock"
)

func TestColibriTierDefaultsAndValidation(t *testing.T) {
    got:=defaultColibriTier()
    if got.Mode!="automatic"||got.Backend!="auto"||got.VulkanExperts!=96 {
        t.Fatalf("unexpected default tier profile: %+v",got)
    }
    tests:=[]struct{name string;profile ColibriTierSettings;wantError bool}{
        {"automatic",ColibriTierSettings{Mode:"automatic",Backend:"auto"},false},
        {"balanced-vulkan",ColibriTierSettings{Mode:"balanced",Backend:"vulkan"},false},
        {"manual-ram",ColibriTierSettings{Mode:"manual",Backend:"vulkan",RAMGB:8,ExpertCap:2,GPUIndex:0,VulkanExperts:64},false},
        {"unknown-mode",ColibriTierSettings{Mode:"aggressive",Backend:"auto"},true},
        {"unknown-backend",ColibriTierSettings{Mode:"manual",Backend:"rocm"},true},
        {"negative-ram",ColibriTierSettings{Mode:"manual",Backend:"cpu",RAMGB:-1},true},
        {"too-many-experts",ColibriTierSettings{Mode:"manual",Backend:"vulkan",VulkanExperts:2500},true},
        {"unsafe-gpu-index",ColibriTierSettings{Mode:"manual",Backend:"cuda",GPUIndex:16},true},
    }
    for _,tt:=range tests {
        t.Run(tt.name,func(t *testing.T){
            got,err:=validateColibriTier(tt.profile)
            if (err!=nil)!=tt.wantError {t.Fatalf("validate(%+v)=%+v, %v",tt.profile,got,err)}
            if tt.name=="balanced-vulkan"&&got.VulkanExperts!=96 {t.Fatalf("small-card expert cap missing: %+v",got)}
        })
    }
}

func TestColibriTierEnvIsBackendScoped(t *testing.T) {
    want:=[]string{"RAM_GB=8","REPIN=128","COLI_CUDA=0","COLI_VULKAN=1","COLI_VK_DEV=0","COLI_VK_EXPERTS=64"}
    got:=colibriTierEnv(ColibriTierSettings{Mode:"manual",Backend:"vulkan",RAMGB:8,RepinTokens:128,VulkanExperts:64})
    if !reflect.DeepEqual(got,want){t.Fatalf("env: got %q, want %q",got,want)}
    cpu:=strings.Join(colibriTierEnv(ColibriTierSettings{Mode:"manual",Backend:"cpu"})," ")
    if !strings.Contains(cpu,"COLI_CUDA=0")||!strings.Contains(cpu,"COLI_VULKAN=0"){t.Fatalf("cpu backend does not disable GPU: %s",cpu)}
    balanced:=strings.Join(colibriTierEnv(ColibriTierSettings{Mode:"balanced",Backend:"auto"})," ")
    if balanced!="COLI_POLICY=balanced REPIN=64" {t.Fatalf("balanced policy: %s",balanced)}
    if args:=colibriTierCapArgs(ColibriTierSettings{Mode:"automatic",ExpertCap:3});len(args)!=0 {
        t.Fatalf("automatic must not force an expert cap: %v",args)
    }
    if args:=colibriTierCapArgs(ColibriTierSettings{Mode:"manual",ExpertCap:3});!reflect.DeepEqual(args,[]string{"--cap","3"}) {
        t.Fatalf("manual expert cap: %v",args)
    }
}

func TestColibriTierConfigBackwardsCompatible(t *testing.T) {
    settings:=tierSettingsFromConfig(nil)
    if settings.Mode!="automatic"||settings.Backend!="auto" {t.Fatalf("legacy profile: %+v",settings)}
}


func TestColibriPlacementOutputBoundedDuringWrites(t *testing.T){
 capture:=&boundedColibriPlanOutput{}
 message:=[]byte(`{"budget":"normal"}`)
 if n,err:=capture.Write(message);err!=nil||n!=len(message){
  t.Fatalf("normal captured bytes=%d error=%v",n,err)
 }
 data,overflow:=capture.Result()
 if overflow||string(data)!=string(message){t.Fatalf("normal output corrupted")}
 huge:=[]byte(strings.Repeat("x",maxColibriPlanOutputBytes*4))
 n,err:=capture.Write(huge)
 if err!=nil||n!=len(huge){t.Fatalf("oversized stdout should drain: %d %v",n,err)}
 data,overflow=capture.Result()
 if !overflow||len(data)!=maxColibriPlanOutputBytes{
  t.Fatalf("external stdout escaped bound: len=%d overflow=%v",len(data),overflow)
 }
 n,err=capture.Write(huge)
 if err!=nil||n!=len(huge){t.Fatalf("after limit output not drained: %d %v",n,err)}
 data,overflow=capture.Result()
 if !overflow||len(data)!=maxColibriPlanOutputBytes{
  t.Fatalf("capture grew after overflow: %d %v",len(data),overflow)
 }
}

func TestColibriTierAndPinWaitForRuntimeResidencyLock(t *testing.T){
 db:=colibriPinTestDB(t)
 defer db.Close()
 if _,err:=db.Exec(`CREATE TABLE local_runtime_instances(
  deployment_id TEXT PRIMARY KEY,status TEXT)`);err!=nil{t.Fatal(err)}
 sup:=&RuntimeSupervisor{db:db,clock:clock.Real{}}
 svc:=&Service{db:db,clock:clock.Real{},supervisor:sup}
 ctx:=context.Background()
 sup.residencyMu.Lock()
 entered:=make(chan struct{})
 completed:=make(chan error,1)
 go func(){
  close(entered)
  _,err:=svc.SetColibriTier(ctx,ColibriTierCommand{
   DeploymentID:"colibri-a",
   Settings:ColibriTierSettings{Mode:"manual",Backend:"cpu",RAMGB:4},
  })
  completed<-err
 }()
 <-entered
 select{
 case err:=<-completed:
  sup.residencyMu.Unlock()
  t.Fatalf("Colibri config changed while residency lock held: %v",err)
 case <-time.After(60*time.Millisecond):
 }
 var before string
 if err:=db.QueryRow(`SELECT json_extract(runtime_config_json,'$.colibri_tier.ram_gb')
   FROM model_deployments WHERE id='colibri-a'`).Scan(&before);err!=nil{t.Fatal(err)}
 if before!="8"{t.Fatalf("tier modified while hot-swap owns lock: %s",before)}
 sup.residencyMu.Unlock()
 select{
 case err:=<-completed:if err!=nil{t.Fatalf("Colibri tier update failed: %v",err)}
 case <-time.After(3*time.Second):t.Fatal("Colibri tier update stayed blocked")
 }
 var after string
 if err:=db.QueryRow(`SELECT json_extract(runtime_config_json,'$.colibri_tier.ram_gb')
   FROM model_deployments WHERE id='colibri-a'`).Scan(&after);err!=nil{t.Fatal(err)}
 if after!="4"{t.Fatalf("Colibri tier update did not persist: %s",after)}
 // Pin update shares the same lock and preserves the new tier profile.
 sup.residencyMu.Lock()
 pinDone:=make(chan error,1)
 pinStarted:=make(chan struct{})
 go func(){
  close(pinStarted)
  _,err:=svc.SetColibriPin(ctx,"colibri-a",true)
  pinDone<-err
 }()
 <-pinStarted
 select{
 case err:=<-pinDone:
  sup.residencyMu.Unlock()
  t.Fatalf("pin modified while hot-swap owns lock: %v",err)
 case <-time.After(60*time.Millisecond):
 }
 sup.residencyMu.Unlock()
 select{
 case err:=<-pinDone:if err!=nil{t.Fatalf("pin update failed: %v",err)}
 case <-time.After(3*time.Second):t.Fatal("pin update stayed blocked")
 }
 var pinned int
 if err:=db.QueryRow(`SELECT json_extract(runtime_config_json,'$.colibri_pinned')
  FROM model_deployments WHERE id='colibri-a'`).Scan(&pinned);err!=nil{t.Fatal(err)}
 if pinned!=1{t.Fatal("Colibri pin lost during tier update")}
}
