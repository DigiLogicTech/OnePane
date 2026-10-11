package sandboxrunner

import (
 "path/filepath"
 "testing"
)

func TestReusedOCIContainerMustMatchObservedWorkspaceIdentityAndConfig(t *testing.T){
 dir:=t.TempDir()
 workspace:=filepath.Join(dir,"projects","runtime-world","workspace")
 s:=ContainerSpec{
  RuntimeID:"runtime-world",ApplicationID:"godot",Image:"ghcr.io/example/godot@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  WorkspacePath:workspace,NetworkInternal:true,
  Command:[]string{"sleep","3600"},WorkingDir:"/workspace",
  Limits:ResourceLimits{CPUMillis:1000,MemoryMB:512,PIDs:64},
 }
 good:=ContainerState{
  RuntimeID:s.RuntimeID,ApplicationID:s.ApplicationID,
  SpecHash:specHash(s),Image:s.Image,Status:"running",
  IsolationVerified:true,NetworkInternal:true,
  Mounts:[]MountState{{Type:"bind",Source:workspace,Destination:"/workspace",RW:true}},
 }
 if !verifiedOwnedContainer(s,good)||!verifiedDesiredContainer(s,good){
  t.Fatal("verified exact Workspace container rejected")
 }
 cases:=[]struct{name string;mutate func(*ContainerState)}{
  {"unverified_isolation",func(x *ContainerState){x.IsolationVerified=false}},
  {"foreign_runtime",func(x *ContainerState){x.RuntimeID="runtime-story"}},
  {"foreign_application",func(x *ContainerState){x.ApplicationID="other"}},
  {"empty_spec_hash",func(x *ContainerState){x.SpecHash=""}},
  {"foreign_mount",func(x *ContainerState){x.Mounts[0].Source=filepath.Join(dir,"projects","story","workspace")}},
  {"extra_host_bind",func(x *ContainerState){x.Mounts=append(x.Mounts,MountState{Type:"bind",Source:"/host",Destination:"/etc",RW:true})}},
  {"extra_oci_volume",func(x *ContainerState){x.Mounts=append(x.Mounts,MountState{Type:"volume",Destination:"/cache",RW:true})}},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   bad:=good
   bad.Mounts=append([]MountState(nil),good.Mounts...)
   tc.mutate(&bad)
   if verifiedOwnedContainer(s,bad)||verifiedDesiredContainer(s,bad){
    t.Fatal("invalid existing container was accepted for managed reuse")
   }
  })
 }
 changes:=[]struct{name string;mutate func(*ContainerState)}{
  {"foreign_image",func(x *ContainerState){x.Image="ghcr.io/attacker/other@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},
  {"stale_spec_hash",func(x *ContainerState){x.SpecHash="sha256:old"}},
  {"wrong_internal_network",func(x *ContainerState){x.NetworkInternal=false}},
 }
 for _,tc:=range changes{
  t.Run(tc.name,func(t *testing.T){
   outdated:=good
   tc.mutate(&outdated)
   if !verifiedOwnedContainer(s,outdated){
    t.Fatal("verified old spec should remain identifiable for explicit upgrade")
   }
   if verifiedDesiredContainer(s,outdated){
    t.Fatal("changed OCI image/spec/network incorrectly reused")
   }
  })
 }
}
