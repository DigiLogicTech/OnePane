package projectruntime

import (
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestReconciliationRefusesChangedRuntimeIntentOrRemoteNode(t *testing.T){
 local:="node-local"
 other:="node-remote"
 pws:="workspace-build"
 base:=projectworkspace.ProjectRuntime{
  ID:"runtime-a",ProjectID:"project-a",ProjectWorkspaceID:&pws,NodeID:&local,
  Backend:"sandbox_runner",IsolationMode:projectworkspace.IsolationSandboxedContainer,
  DesiredState:projectworkspace.RuntimeDesiredRunning,Revision:7,
  RuntimeSpecJSON:json.RawMessage(`{"engine":"podman"}`),
  ResourceLimitsJSON:json.RawMessage(`{"cpu":2}`),
  NetworkPolicyJSON:json.RawMessage(`{"internet":false}`),
  FilesystemPolicyJSON:json.RawMessage(`{"readonly":true}`),
  EnvironmentBindingsJSON:json.RawMessage(`{"scope":"none"}`),
 }
 if !sameRuntimeReconciliationTarget(base,base){t.Fatal("identical runtime rejected")}
 if !localPlacementAllowed(local,base.NodeID){t.Fatal("local node unexpectedly rejected")}
 for _,tc:=range []struct{name string;mutate func(*projectworkspace.ProjectRuntime)}{
  {"revision",func(v *projectworkspace.ProjectRuntime){v.Revision++}},
  {"node_moved",func(v *projectworkspace.ProjectRuntime){v.NodeID=&other}},
  {"node_removed",func(v *projectworkspace.ProjectRuntime){v.NodeID=nil}},
  {"workspace_moved",func(v *projectworkspace.ProjectRuntime){w:="other";v.ProjectWorkspaceID=&w}},
  {"project_changed",func(v *projectworkspace.ProjectRuntime){v.ProjectID="other"}},
  {"desired_stop",func(v *projectworkspace.ProjectRuntime){v.DesiredState=projectworkspace.RuntimeDesiredStopped}},
  {"backend",func(v *projectworkspace.ProjectRuntime){v.Backend="other"}},
  {"isolation",func(v *projectworkspace.ProjectRuntime){v.IsolationMode="none"}},
  {"image",func(v *projectworkspace.ProjectRuntime){v.RuntimeSpecJSON=json.RawMessage(`{"engine":"other"}`)}},
  {"resources",func(v *projectworkspace.ProjectRuntime){v.ResourceLimitsJSON=json.RawMessage(`{"cpu":8}`)}},
  {"network",func(v *projectworkspace.ProjectRuntime){v.NetworkPolicyJSON=json.RawMessage(`{"internet":true}`)}},
  {"filesystem",func(v *projectworkspace.ProjectRuntime){v.FilesystemPolicyJSON=json.RawMessage(`{"readonly":false}`)}},
  {"secret_scope",func(v *projectworkspace.ProjectRuntime){v.EnvironmentBindingsJSON=json.RawMessage(`{"scope":"vault"}`)}},
 }{
  t.Run(tc.name,func(t *testing.T){
   changed:=base
   tc.mutate(&changed)
   if sameRuntimeReconciliationTarget(base,changed){t.Fatal("stale sandbox desired state accepted")}
  })
 }
 if !errors.Is(ErrReconciliationDrift,ErrReconciliationDrift){t.Fatal("missing typed drift error")}
}
func TestReconciliationRefusesChangedApplicationDesiredImageAndEnvironment(t *testing.T){
 tag:="pinned"
 app:=projectworkspace.Application{
  ID:"application-a",ProjectRuntimeID:"runtime-a",
  SourceKind:projectworkspace.AppOCIImage,
  SourceRef:"ghcr.io/example/a@sha256:abc",VersionRef:&tag,
  DesiredState:projectworkspace.AppDesiredRunning,Revision:3,
  InstallSpecJSON:json.RawMessage(`{"packages":[]}`),
  RuntimeSpecJSON:json.RawMessage(`{"entrypoint":"run"}`),
  EnvironmentBindingsJSON:json.RawMessage(`{"vault":false}`),
 }
 if !sameAppReconciliationTarget(app,app){t.Fatal("identical app rejected")}
 for _,tc:=range []struct{name string;mutate func(*projectworkspace.Application)}{
  {"revision",func(v *projectworkspace.Application){v.Revision++}},
  {"new_image",func(v *projectworkspace.Application){v.SourceRef="ghcr.io/example/other@sha256:def"}},
  {"runtime_moved",func(v *projectworkspace.Application){v.ProjectRuntimeID="other"}},
  {"desired_stop",func(v *projectworkspace.Application){v.DesiredState=projectworkspace.AppDesiredStopped}},
  {"tag_removed",func(v *projectworkspace.Application){v.VersionRef=nil}},
  {"source_kind",func(v *projectworkspace.Application){v.SourceKind="git"}},
  {"install",func(v *projectworkspace.Application){v.InstallSpecJSON=json.RawMessage(`{"packages":["other"]}`)}},
  {"runtime",func(v *projectworkspace.Application){v.RuntimeSpecJSON=json.RawMessage(`{"entrypoint":"changed"}`)}},
  {"secrets",func(v *projectworkspace.Application){v.EnvironmentBindingsJSON=json.RawMessage(`{"vault":true}`)}},
 }{
  t.Run(tc.name,func(t *testing.T){
   changed:=app
   tc.mutate(&changed)
   if sameAppReconciliationTarget(app,changed){t.Fatal("stale application mutation accepted")}
  })
 }
}
