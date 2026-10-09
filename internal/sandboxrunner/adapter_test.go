package sandboxrunner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/tool"
)

type fakeEngine struct {
	profile                      EngineProfile
	ensure                       ContainerSpec
	pulls, ensures, stops, execs int
	inspectMount string
	extraBind bool
}

func (f *fakeEngine) Probe(context.Context) (EngineProfile, error) {
	if f.profile.Kind == "" {
		return EngineProfile{}, ErrEngineUnavailable
	}
	return f.profile, nil
}
func (f *fakeEngine) EnsureNetwork(_ context.Context, runtimeID string, internal bool) (NetworkState, error) {
	return NetworkState{Name: "harness-" + runtimeID + "-net", ID: "net", RuntimeID: runtimeID, Internal: true}, nil
}
func (f *fakeEngine) InspectNetwork(_ context.Context, runtimeID string) (NetworkState, error) {
	return NetworkState{Name: "harness-" + runtimeID + "-net", ID: "net", RuntimeID: runtimeID, Internal: true}, nil
}
func (f *fakeEngine) PullImage(_ context.Context, image string) (ImageState, error) {
	f.pulls++
	return ImageState{Reference: image, Digests: []string{"example@sha256:abc"}}, nil
}
func (f *fakeEngine) InspectImage(_ context.Context, image string) (ImageState, error) {
	return ImageState{Reference: image, Digests: []string{"example@sha256:abc"}}, nil
}
func (f *fakeEngine) InspectContainer(_ context.Context, runtimeID, appID string) (ContainerState, error) {
 st := ContainerState{Name: "c", ID: "id", Status: "running", RuntimeID: runtimeID,
  ApplicationID: appID, SpecHash: "sha256:x", IsolationVerified: true}
 if f.inspectMount!="" {
  st.Mounts=[]MountState{{Type:"bind",Source:f.inspectMount,Destination:"/workspace",RW:true}}
 }
 if f.extraBind {
  st.Mounts=append(st.Mounts,MountState{Type:"bind",Source:"/host",Destination:"/other",RW:true})
 }
 return st,nil
}
func (f *fakeEngine) ListRuntime(context.Context, string) ([]ContainerState, error) {
	return []ContainerState{{Name: "c", ID: "id", Status: "running", SpecHash: "sha256:x"}}, nil
}
func (f *fakeEngine) EnsureContainer(_ context.Context, s ContainerSpec) (ContainerState, error) {
	f.ensures++
	s.Environment = cloneStringMap(s.Environment)
	s.EnvironmentIdentity = cloneStringMap(s.EnvironmentIdentity)
	f.ensure = s
	return ContainerState{Name: "c", ID: "id", Status: "running", SpecHash: "sha256:x"}, nil
}
func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func (f *fakeEngine) StopContainer(context.Context, string, string) (ContainerState, error) {
	f.stops++
	return ContainerState{Name: "c", Status: "stopped"}, nil
}
func (f *fakeEngine) ExecContainer(_ context.Context, _, _ string, command []string) (ExecResult, error) {
	f.execs++
	return ExecResult{Stdout: strings.Join(command, " ")}, nil
}
func (f *fakeEngine) StopRuntime(context.Context, string) ([]ContainerState, error) {
	f.stops++
	return []ContainerState{{Name: "c", Status: "stopped"}}, nil
}

func TestAdapterEnsuresApplicationOnlyInsideManagedWorkspace(t *testing.T) {
	eng := &fakeEngine{profile: EngineProfile{Kind: "podman", Executable: "/usr/bin/podman", Rootless: true}}
	a := NewAdapter(t.TempDir(), eng)
	input := json.RawMessage(`{"runtime_id":"runtime-1","application_id":"app-1","image":"ghcr.io/example/app@sha256:abc","runtime_spec":{"command":["serve"],"working_dir":"/workspace"},"resource_limits":{"cpu_millis":1000,"memory_mb":512,"pids":64},"environment_bindings":{"MODE":{"literal":"dev"}},"network_policy":{"mode":"deny_by_default","ingress":"proxy_only","egress":[]}}`)
	res, err := a.Invoke(context.Background(), tool.AdapterRequest{ToolID: ToolAppEnsure, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(res.Result) || eng.ensures != 1 {
		t.Fatalf("result=%s ensures=%d", res.Result, eng.ensures)
	}
	want := filepath.Join(a.dataDir, "projects", "runtime-1", "workspace")
	if eng.ensure.WorkspacePath != want {
		t.Fatalf("workspace=%q want %q", eng.ensure.WorkspacePath, want)
	}
	if eng.ensure.Environment["MODE"] != "dev" || eng.ensure.Limits.MemoryMB != 512 {
		t.Fatalf("spec=%+v", eng.ensure)
	}
}

type fakeSecretResolver struct {
	workspace string
	logical   string
}

func (f *fakeSecretResolver) ResolveWorkspaceLogical(_ context.Context, workspaceID, logical string) (string, string, int64, error) {
	f.workspace, f.logical = workspaceID, logical
	return "top-secret", "secret-record", 3, nil
}

func TestAdapterResolvesWorkspaceScopedSecretsAtTrustedBoundary(t *testing.T) {
	eng := &fakeEngine{profile: EngineProfile{Kind: "podman", Rootless: true}}
	secrets := &fakeSecretResolver{}
	a := NewAdapter(t.TempDir(), eng, secrets)
	_, err := a.Invoke(context.Background(), tool.AdapterRequest{WorkspaceID: "ws", ToolID: ToolAppEnsure, Input: json.RawMessage(`{"runtime_id":"r","application_id":"a","image":"example/app:1","environment_bindings":{"API_KEY":{"secret_ref":"secret:openai"}}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if secrets.workspace != "ws" || secrets.logical != "openai" {
		t.Fatalf("resolution scope=%q logical=%q", secrets.workspace, secrets.logical)
	}
	if eng.ensure.Environment["API_KEY"] != "top-secret" || eng.ensure.EnvironmentIdentity["API_KEY"] != "vault:secret-record:v3" {
		t.Fatalf("resolved env=%+v identities=%+v", eng.ensure.Environment, eng.ensure.EnvironmentIdentity)
	}
}

func TestAdapterRejectsSecretsWithoutBroker(t *testing.T) {
	eng := &fakeEngine{profile: EngineProfile{Kind: "podman", Rootless: true}}
	a := NewAdapter(t.TempDir(), eng)
	_, err := a.Invoke(context.Background(), tool.AdapterRequest{WorkspaceID: "ws", ToolID: ToolAppEnsure, Input: json.RawMessage(`{"runtime_id":"r","application_id":"a","image":"example/app:1","environment_bindings":{"API_KEY":{"secret_ref":"secret:x"}}}`)})
	if err == nil || !errors.Is(err, ErrSecretBrokerRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestAdapterRejectsNetworkUntilProxyBackend(t *testing.T) {
	eng := &fakeEngine{profile: EngineProfile{Kind: "podman", Rootless: true}}
	a := NewAdapter(t.TempDir(), eng)
	_, err := a.Invoke(context.Background(), tool.AdapterRequest{ToolID: ToolAppEnsure, Input: json.RawMessage(`{"runtime_id":"r","application_id":"a","image":"example/app:1","network_policy":{"mode":"deny_by_default","egress":["https://example.com"]}}`)})
	if !errors.Is(err, ErrNetworkBackendRequired) {
		t.Fatalf("err=%v", err)
	}
	if eng.ensures != 0 {
		t.Fatal("networked app executed before backend")
	}
}
func TestAdapterRequiresRootlessEngine(t *testing.T) {
	eng := &fakeEngine{profile: EngineProfile{Kind: "docker", Rootless: false}}
	a := NewAdapter(t.TempDir(), eng)
	_, err := a.Invoke(context.Background(), tool.AdapterRequest{ToolID: ToolRuntimeEnsure, Input: json.RawMessage(`{"runtime_id":"r"}`)})
	if !errors.Is(err, ErrRootlessRequired) {
		t.Fatalf("err=%v", err)
	}
}
func TestInspectToolsAreReadOnlyObservations(t *testing.T) {
	eng := &fakeEngine{profile: EngineProfile{Kind: "podman", Rootless: true}}
	a := NewAdapter(t.TempDir(), eng)
	res, err := a.Invoke(context.Background(), tool.AdapterRequest{ToolID: ToolAppInspect, Input: json.RawMessage(`{"runtime_id":"runtime-1","application_id":"app-1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(res.Result) {
		t.Fatalf("invalid result=%s", res.Result)
	}
	res, err = a.Invoke(context.Background(), tool.AdapterRequest{ToolID: ToolRuntimeInspect, Input: json.RawMessage(`{"runtime_id":"runtime-1"}`)})
	if err != nil || !json.Valid(res.Result) {
		t.Fatalf("runtime inspect err=%v result=%s", err, res.Result)
	}
}

func TestAppExecRequiresVerifiedSandbox(t *testing.T) {
 eng := &fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 a:=NewAdapter(t.TempDir(),eng)
 path,exists,err:=managedWorkspacePath(a.dataDir,"r",true)
 if err!=nil||!exists{t.Fatal(err)}
 eng.inspectMount=path
 input:=tool.AdapterRequest{ToolID:ToolAppExec,Input:json.RawMessage(`{"runtime_id":"r","application_id":"a","command":["echo","hello"]}`)}
 result,err:=a.Invoke(context.Background(),input)
 if err!=nil||!json.Valid(result.Result)||eng.execs!=1{
  t.Fatalf("valid sandbox rejected: %v %s execs=%d",err,result.Result,eng.execs)
 }
 // A different Workspace's host volume must never be executable.
 eng.inspectMount=filepath.Join(a.dataDir,"projects","other","workspace")
 if _,err=a.Invoke(context.Background(),input);err==nil||eng.execs!=1{
  t.Fatalf("foreign Workspace mount executed: %v execs=%d",err,eng.execs)
 }
 eng.inspectMount=path
 eng.extraBind=true
 if _,err=a.Invoke(context.Background(),input);err==nil||eng.execs!=1{
  t.Fatalf("additional host bind executed: %v execs=%d",err,eng.execs)
 }
}

func TestRegisterSandboxToolsAreMutations(t *testing.T) {
	reg := tool.NewRegistry()
	a := NewAdapter(t.TempDir(), &fakeEngine{profile: EngineProfile{Kind: "podman", Rootless: true}})
	if err := Register(reg, a); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{ToolRuntimeEnsure, ToolRuntimeStop, ToolAppPull, ToolAppEnsure, ToolAppStop} {
		b, err := reg.Resolve(id, "1")
		if err != nil {
			t.Fatal(err)
		}
		if string(b.Definition.Mode) != "mutate" {
			t.Fatalf("%s mode=%s", id, b.Definition.Mode)
		}
	}
	for _, id := range []string{ToolRuntimeInspect, ToolAppInspect, ToolImageInspect} {
		b, err := reg.Resolve(id, "1")
		if err != nil {
			t.Fatal(err)
		}
		if string(b.Definition.Mode) != "observe" || b.Definition.CapabilityID != CapabilityObserve {
			t.Fatalf("%s mode=%s capability=%s", id, b.Definition.Mode, b.Definition.CapabilityID)
		}
	}
	b, err := reg.Resolve(ToolAppExec, "1")
	if err != nil {
		t.Fatal(err)
	}
	if string(b.Definition.Mode) != "execute_sandboxed" || b.Definition.CapabilityID != CapabilityExecute {
		t.Fatalf("exec mode=%s capability=%s", b.Definition.Mode, b.Definition.CapabilityID)
	}
}


func TestManagedWorkspaceRejectsSymlinksAndFiles(t *testing.T){
 for _,location:=range []string{"projects","runtime","workspace"}{
  t.Run(location,func(t *testing.T){
   root:=t.TempDir()
   outside:=t.TempDir()
   base:=filepath.Join(root,"projects","runtime","workspace")
   var target string
   switch location {
   case "projects": target=filepath.Join(root,"projects")
   case "runtime":
    if err:=os.Mkdir(filepath.Join(root,"projects"),0o700);err!=nil{t.Fatal(err)}
    target=filepath.Join(root,"projects","runtime")
   case "workspace":
    if err:=os.MkdirAll(filepath.Join(root,"projects","runtime"),0o700);err!=nil{t.Fatal(err)}
    target=base
   }
   if err:=os.Symlink(outside,target);err!=nil{t.Skipf("symlink unsupported: %v",err)}
   if _,_,err:=managedWorkspacePath(root,"runtime",true);!errors.Is(err,ErrInvalidInput){
    t.Fatalf("followed symlink %s: %v",location,err)
   }
   if _,_,err:=managedWorkspacePath(root,"runtime",false);!errors.Is(err,ErrInvalidInput){
    t.Fatalf("read-only inspect followed symlink %s: %v",location,err)
   }
  })
 }
}

func TestInspectDoesNotClaimIsolationWithForeignWorkspaceMount(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 a:=NewAdapter(t.TempDir(),eng)
 expected,ok,err:=managedWorkspacePath(a.dataDir,"r",true)
 if err!=nil||!ok{t.Fatal(err)}
 in:=tool.AdapterRequest{ToolID:ToolAppInspect,Input:json.RawMessage(`{"runtime_id":"r","application_id":"a"}`)}
 eng.inspectMount=filepath.Join(a.dataDir,"projects","different","workspace")
 bad,err:=a.Invoke(context.Background(),in)
 if err!=nil{t.Fatal(err)}
 var denied struct{Container ContainerState `json:"container"`}
 if err=json.Unmarshal(bad.Result,&denied);err!=nil{t.Fatal(err)}
 if denied.Container.IsolationVerified{t.Fatal("foreign mount was reported as isolated")}
 eng.inspectMount=expected
 good,err:=a.Invoke(context.Background(),in)
 if err!=nil{t.Fatal(err)}
 var allowed struct{Container ContainerState `json:"container"`}
 if err=json.Unmarshal(good.Result,&allowed);err!=nil{t.Fatal(err)}
 if !allowed.Container.IsolationVerified{t.Fatal("managed mount not verified")}
}
