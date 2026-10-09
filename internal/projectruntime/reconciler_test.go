package projectruntime

import (
 "encoding/json"
 "testing"
)

func TestPostconditionsRequireExactRuntimeApplicationAndIsolation(t *testing.T) {
 good:=json.RawMessage(`{"runtime_id":"r1","application_id":"a1","container":{"runtime_id":"r1","application_id":"a1","status":"running","spec_hash":"sha256:spec","isolation_verified":true},"engine":{"kind":"podman","rootless":true}}`)
 if !appRunningIsolated(good,"r1","a1"){t.Fatal("verified application rejected")}
 for _,tc:=range []struct{name string;raw json.RawMessage;runtime,app string}{
  {"wrong_runtime",good,"r2","a1"},
  {"wrong_application",good,"r1","a2"},
  {"foreign_container_runtime",json.RawMessage(`{"runtime_id":"r1","application_id":"a1","container":{"runtime_id":"r2","application_id":"a1","status":"running","spec_hash":"sha256:x","isolation_verified":true},"engine":{"rootless":true}}`),"r1","a1"},
  {"missing_spec_hash",json.RawMessage(`{"runtime_id":"r1","application_id":"a1","container":{"runtime_id":"r1","application_id":"a1","status":"running","isolation_verified":true},"engine":{"rootless":true}}`),"r1","a1"},
  {"isolation_not_verified",json.RawMessage(`{"runtime_id":"r1","application_id":"a1","container":{"runtime_id":"r1","application_id":"a1","status":"running","spec_hash":"sha256:x","isolation_verified":false},"engine":{"rootless":true}}`),"r1","a1"},
  {"engine_not_rootless",json.RawMessage(`{"runtime_id":"r1","application_id":"a1","container":{"runtime_id":"r1","application_id":"a1","status":"running","spec_hash":"sha256:x","isolation_verified":true},"engine":{"rootless":false}}`),"r1","a1"},
 }{
  t.Run(tc.name,func(t *testing.T){
   if appRunningIsolated(tc.raw,tc.runtime,tc.app){t.Fatal("accepted wrong or unverified application")}
  })
 }
}

func TestRuntimePostconditionsRequireOwnedNetworkAndFailClosedStates(t *testing.T){
 good:=json.RawMessage(`{"runtime_id":"r1","workspace_exists":true,"network":{"name":"harness-r1-net","runtime_id":"r1","internal":true},"engine":{"rootless":true}}`)
 if !runtimeWorkspacePresent(good,"r1",true){t.Fatal("valid Workspace not recognised")}
 if runtimeWorkspacePresent(good,"r2",true){t.Fatal("foreign runtime accepted")}
 if runtimeWorkspacePresent(good,"r1",false){t.Fatal("unexpected external network accepted")}
 for _,bad:=range []json.RawMessage{
  json.RawMessage(`{"runtime_id":"r1","workspace_exists":true,"network":{"name":"foreign","runtime_id":"other","internal":true},"engine":{"rootless":true}}`),
  json.RawMessage(`{"runtime_id":"r1","workspace_exists":true,"network":{"name":"","runtime_id":"r1","internal":true},"engine":{"rootless":true}}`),
  json.RawMessage(`{"runtime_id":"r1","workspace_exists":true,"network":{"name":"n","runtime_id":"r1","internal":true},"engine":{"rootless":false}}`),
 }{
  if runtimeWorkspacePresent(bad,"r1",true){t.Fatalf("unauthorised network accepted: %s",bad)}
 }
 stopped:=json.RawMessage(`{"runtime_id":"r1","containers":[{"status":"stopped"},{"status":"absent"}],"engine":{"rootless":true}}`)
 if !runtimeStopped(stopped,"r1"){t.Fatal("confirmed stop rejected")}
 if runtimeStopped(stopped,"r2"){t.Fatal("foreign runtime stop accepted")}
 for _,state:=range []string{"running","starting","paused","restarting","removing","unknown",""}{
  raw,_:=json.Marshal(map[string]any{"runtime_id":"r1","containers":[]map[string]any{{"status":state}},"engine":map[string]any{"rootless":true}})
  if runtimeStopped(raw,"r1"){t.Fatalf("unverified stop accepted for state=%q",state)}
 }
}

func TestAppStoppedRequiresExactOwnership(t *testing.T){
 good:=json.RawMessage(`{"runtime_id":"r1","application_id":"a1","container":{"status":"absent","runtime_id":"r1","application_id":"a1"},"engine":{"rootless":true}}`)
 if !appStopped(good,"r1","a1"){t.Fatal("absent app rejected")}
 if appStopped(good,"r2","a1")||appStopped(good,"r1","a2"){t.Fatal("foreign app stop accepted")}
 foreign:=json.RawMessage(`{"runtime_id":"r1","application_id":"a1","container":{"status":"stopped","runtime_id":"other","application_id":"a1"},"engine":{"rootless":true}}`)
 if appStopped(foreign,"r1","a1"){t.Fatal("foreign container stop accepted")}
}

func TestImagePresentRequiresExactReference(t *testing.T) {
 raw:=json.RawMessage(`{"image":{"reference":"ghcr.io/acme/app@sha256:abc","digests":["ghcr.io/acme/app@sha256:abc"]},"engine":{"rootless":true}}`)
 if !imagePresent(raw,"ghcr.io/acme/app@sha256:abc"){t.Fatal("image not recognized")}
 if imagePresent(raw,"ghcr.io/acme/app:latest"){t.Fatal("wrong image accepted")}
}

func TestReconcilerNodePlacementNeverFallsBackToServiceHost(t *testing.T){
 local:="node-windows"
 same:="node-windows"
 remote:="node-ubuntu"
 empty:=""
 cases:=[]struct{name,host string;requested *string;allowed bool}{
  {"legacy_unassigned_local","node-windows",nil,true},
  {"explicit_local","node-windows",&same,true},
  {"explicit_remote","node-windows",&remote,false},
  {"no_local_identity_with_explicit_target","",&same,false},
  {"explicit_empty_target","node-windows",&empty,false},
  {"explicit_target_with_empty_host","",&remote,false},
  {"remote_never_ignored_even_when_windows_service","node-windows",&remote,false},
 }
 for _,tc:=range cases {
  t.Run(tc.name,func(t *testing.T){
   if got:=localPlacementAllowed(tc.host,tc.requested);got!=tc.allowed{
    t.Fatalf("wrong local OCI placement decision: got %v want %v",got,tc.allowed)
   }
  })
 }
}
