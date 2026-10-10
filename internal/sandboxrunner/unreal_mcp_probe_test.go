package sandboxrunner

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/tool"
)

func TestEngineCatalogIsHonestAndEngineNeutral(t *testing.T){
 rows:=GameEngineCapabilities()
 if len(rows)<3{t.Fatalf("expected multiple engine profiles, got %d",len(rows))}
 ids:=map[string]bool{}
 for _,row:=range rows{
  if ids[row.EngineID]||row.EngineID==""||row.DisplayName==""||row.Adapter==""{t.Fatalf("duplicate/incomplete engine: %+v",row)}
  ids[row.EngineID]=true
 }
 if rows[0].EngineID!="godot"||rows[0].Adapter!="governed_oci_cli"{t.Fatal("Godot requires explicit fixed command adapter")}
 if rows[1].EngineID!="unreal_engine"||rows[1].Status!="experimental_read_only_probe"||
  len(rows[1].SupportedActions)!=1||rows[1].SupportedActions[0]!="probe_local_editor"{
  t.Fatalf("unverified Unreal editor edit capabilities advertised: %+v",rows[1])
 }
 if rows[2].EngineID!="uefn"||rows[2].Status!="planned_not_connected"||len(rows[2].SupportedActions)!=0{
  t.Fatalf("unverified UEFN functionality advertised: %+v",rows[2])
 }
}

func TestUnrealMCPProbeUsesFixedLocalEditorHandshake(t *testing.T){
 args:=unrealMCPProbeCommand()
 if len(args)!=5||args[0]!="python3"||args[1]!="-I"||args[2]!="-S"||args[3]!="-c"{
  t.Fatalf("untrusted Python tool execution: %+v",args)
 }
 script:=args[4]
 for _,required:=range []string{
  "http://127.0.0.1:8000/mcp","urllib.request.ProxyHandler({})",
  "NoRedirect","2025-06-18","initialize","unreal-mcp",
  "read(16385)","timeout=4",
 }{
  if !strings.Contains(script,required){t.Fatalf("missing safe handshake property %s",required)}
 }
 for _,bad:=range []string{"tools/call","tools/list","call_tool","list_toolsets","0.0.0.0","urllib.request.urlopen","subprocess","os.system"}{
  if strings.Contains(script,bad){t.Fatalf("mutating, forwarded or dynamic editor capability in readiness script: %s",bad)}
 }
 for _,bad:=range []string{
  `{"runtime_id":"world","application_id":"editor","action":"tools/call"}`,
  `{"runtime_id":"world","application_id":"editor","url":"http://192.168.1.1/mcp"}`,
  `{"runtime_id":"world","application_id":"editor","port":7777}`,
  `{"runtime_id":"world","application_id":"editor","token":"secret"}`,
  `{"runtime_id":"world","application_id":"editor","command":["curl","http://localhost"]}`,
  `{"runtime_id":"world","application_id":"editor","timeout_seconds":31}`,
  `{"runtime_id":"world","application_id":"editor","timeout_seconds":0}`,
  `{"runtime_id":"world","application_id":"editor","timeout_seconds":null}`,
  `{"runtime_id":"world","application_id":"editor","workspace_id":"other"}`,
 }{
  if validUnrealMCPEnvelope(json.RawMessage(bad)){t.Fatalf("unsafe MCP request approved: %s",bad)}
 }
 if !validUnrealMCPEnvelope(json.RawMessage(`{"runtime_id":"world","application_id":"editor","timeout_seconds":8}`)){
  t.Fatal("valid local probe input rejected")
 }
 for _,tc:=range []struct{json string;avail bool;reject bool}{
  {`{"available":true,"server":"unreal-mcp"}`,true,false},
  {`{"available":false,"server":"unavailable"}`,false,false},
  {`{"available":true,"server":"other"}`,false,true},
  {`{"available":true,"server":"unreal-mcp","host":"forged"}`,false,true},
  {`{"available":true,"server":"unreal-mcp"}{"available":true}`,false,true},
 }{
  available,err:=parseUnrealMCPProbe(tc.json)
  if (err!=nil)!=tc.reject||(!tc.reject&&available!=tc.avail){
   t.Fatalf("invalid output acceptance %q: available=%v err=%v",tc.json,available,err)
  }
 }
}

func TestUnrealMCPProbeRequiresSameVerifiedRootlessOCI(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true}}
 adapter:=NewAdapter(t.TempDir(),eng)
 root,ok,err:=managedWorkspacePath(adapter.dataDir,"world",true)
 if err!=nil||!ok{t.Fatal(err)}
 eng.inspectMount=root
 eng.execStdout=`{"available":true,"server":"unreal-mcp"}`
 req:=tool.AdapterRequest{ToolID:ToolAppUnrealMCPProbe,
  Input:json.RawMessage(`{"runtime_id":"world","application_id":"editor"}`)}
 out,err:=adapter.Invoke(context.Background(),req)
 if err!=nil{t.Fatal(err)}
 var result struct{
  Status string `json:"status"`
  IdentityVerified bool `json:"identity_verified"`
  EditorCalls bool `json:"editor_tool_calls_enabled"`
  ArtifactVerified bool `json:"artifact_verified"`
 }
 if err=json.Unmarshal(out.Result,&result);err!=nil{t.Fatal(err)}
 if result.Status!="ready"||!result.IdentityVerified||result.EditorCalls||result.ArtifactVerified||eng.execs!=1{
  t.Fatalf("untrusted Unreal readiness was overstated: %+v",result)
 }
 eng.execExitCode=2
 eng.execStdout=`{"available":false,"server":"unavailable"}`
 out,err=adapter.Invoke(context.Background(),req)
 if err!=nil{t.Fatal(err)}
 if err=json.Unmarshal(out.Result,&result);err!=nil{t.Fatal(err)}
 if result.Status!="unavailable"||result.IdentityVerified||result.EditorCalls{
  t.Fatalf("MCP failure was silently accepted: %+v",result)
 }
 eng.execExitCode=0
 eng.execStdout=`{"available":true,"server":"wrong-editor"}`
 if _,err=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrInvalidInput){
  t.Fatalf("spoofed editor identity accepted: %v",err)
 }
 before:=eng.execs
 eng.extraBind=true
 if _,err=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrInvalidInput)||eng.execs!=before{
  t.Fatalf("foreign Workspace bind allowed: %v",err)
 }
 eng.extraBind=false
 eng.profile.Rootless=false
 if _,err=adapter.Invoke(context.Background(),req);!errors.Is(err,ErrRootlessRequired)||eng.execs!=before{
  t.Fatalf("rootful engine allowed for MCP: %v",err)
 }
}

func TestUnrealMCPProbeRespectsContextDeadline(t *testing.T){
 eng:=&fakeEngine{profile:EngineProfile{Kind:"podman",Rootless:true},blockExec:true}
 a:=NewAdapter(t.TempDir(),eng)
 root,ok,err:=managedWorkspacePath(a.dataDir,"world",true)
 if err!=nil||!ok{t.Fatal(err)}
 eng.inspectMount=root
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Millisecond)
 defer cancel()
 _,err=a.Invoke(ctx,tool.AdapterRequest{ToolID:ToolAppUnrealMCPProbe,
  Input:json.RawMessage(`{"runtime_id":"world","application_id":"editor"}`)})
 if !errors.Is(err,context.DeadlineExceeded){t.Fatalf("editor probe timeout reported success: %v",err)}
}
