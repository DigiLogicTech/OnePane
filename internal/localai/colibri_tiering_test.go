package localai

import (
    "reflect"
    "strings"
    "testing"
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
