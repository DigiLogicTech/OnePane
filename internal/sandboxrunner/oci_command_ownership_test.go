package sandboxrunner

import (
 "testing"
)

func TestOCIToolActionsRequireVerifiedRootlessWorkspaceOwner(t *testing.T){
 r,a:="runtime-one","builder"
 good:=ContainerState{
  ID:"full-immutable-oci-identifier",Name:containerName(r,a),
  RuntimeID:r,ApplicationID:a,Status:"running",
  SpecHash:"sha256:verified",IsolationVerified:true,
 }
 if !verifiedCommandTarget(r,a,good){t.Fatal("verified owner rejected")}
 cases:=[]struct{name string;change func(*ContainerState)}{
  {"missing_oci_id",func(v *ContainerState){v.ID=""}},
  {"absent",func(v *ContainerState){v.Status="absent"}},
  {"stopped",func(v *ContainerState){v.Status="stopped"}},
  {"starting",func(v *ContainerState){v.Status="starting"}},
  {"unverified_network_or_mount",func(v *ContainerState){v.IsolationVerified=false}},
  {"other_runtime",func(v *ContainerState){v.RuntimeID="other"}},
  {"other_app",func(v *ContainerState){v.ApplicationID="other"}},
  {"forged_name",func(v *ContainerState){v.Name="harness-attacker"}},
  {"unverified_spec_hash",func(v *ContainerState){v.SpecHash=""}},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   altered:=good
   tc.change(&altered)
   if verifiedCommandTarget(r,a,altered){
    t.Fatal("CLI command target accepted an unverified OCI container")
   }
  })
 }
}
