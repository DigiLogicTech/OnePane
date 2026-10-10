//go:build integration

package workspacebroker

import (
 "context"
 "encoding/json"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
 "github.com/DigiLogicTech/OnePane/internal/tool"
)

// TestRealRootlessWorkspaceHealthBroker is an administrator-opt-in physical
// acceptance. Green compilation and skipped jobs are NEVER proof of Podman
// or actual service connectivity. No OCI images or software are downloaded.
func TestRealRootlessWorkspaceHealthBroker(t *testing.T){
 image:=strings.TrimSpace(os.Getenv("ONEPANE_LARGE_ARTIFACT_SMOKE_IMAGE"))
 if image==""{t.Skip("physical broker test requires an approved pre-pulled Python OCI image and rootless Podman")}
 if matched,_:=regexp.MatchString(`^[^[:space:]]+@sha256:[a-fA-F0-9]{64}$`,image);!matched{
  t.Fatal("physical broker requires an immutable digest-pinned image")
 }
 ctx,cancel:=context.WithTimeout(context.Background(),7*time.Minute)
 defer cancel()
 engine:=sandboxrunner.NewCLIEngine()
 profile,err:=engine.Probe(ctx)
 if err!=nil||!profile.Rootless||profile.Kind!="podman"{
  t.Fatalf("trusted unprivileged rootless Podman required: %v",err)
 }
 if _,err=engine.InspectImage(ctx,image);err!=nil{
  t.Fatalf("approved OCI Python image missing: %v",err)
 }
 dataRoot:=t.TempDir()
 db,err:=sqlitestore.Open(filepath.Join(t.TempDir(),"physical-broker.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 const tenant,operator,node="physical-service-tenant","physical-service-operator","physical-service-node"
 now:=clock.Real{}.UnixMilli()
 for _,statement:=range []string{
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,protocol_json,
   capabilities_json,revision,created_at,updated_at)
   VALUES('physical-service-node','Physical service Node',1,'qa-service-trusted','local','{}','{}',1,?,?)`,
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('physical-service-tenant','Service tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('physical-service-operator','human','Physical service operator','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('physical-service-tenant','physical-service-operator','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,statement,now,now);err!=nil{t.Fatal(err)}
 }
 svc:=projectworkspace.NewService(db.SQL(),db,clock.Real{})
 p,err:=svc.CreateProject(ctx,projectworkspace.CreateProjectCommand{
  WorkspaceID:tenant,Name:"Physical service broker QA",CreatedBy:operator,
 })
 if err!=nil{t.Fatal(err)}
 world,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"World Service",ActorPrincipalID:operator,
 })
 if err!=nil{t.Fatal(err)}
 story,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Story Client",ActorPrincipalID:operator,
 })
 if err!=nil{t.Fatal(err)}
 research,err:=svc.CreateWorkspaceView(ctx,projectworkspace.CreateWorkspaceViewCommand{
  ProjectID:p.ID,Name:"Isolated Research",ActorPrincipalID:operator,
 })
 if err!=nil{t.Fatal(err)}
 runtime,err:=svc.CreateRuntime(ctx,projectworkspace.CreateRuntimeCommand{
  ProjectID:p.ID,ProjectWorkspaceID:&world.ID,NodeID:physicalPtr(node),CreatedBy:operator,
 })
 if err!=nil{t.Fatal(err)}
 app,err:=svc.DeclareApplication(ctx,projectworkspace.DeclareApplicationCommand{
  RuntimeID:runtime.ID,Name:"Pinned Python HTTP readiness",
  SourceKind:projectworkspace.AppOCIImage,SourceRef:image,CreatedBy:operator,
 })
 if err!=nil{t.Fatal(err)}
 endpoint,err:=svc.DeclareEndpoint(ctx,projectworkspace.DeclareEndpointCommand{
  RuntimeID:runtime.ID,ApplicationID:&app.ID,Name:"HTTP readiness",
  Protocol:"http",InternalPort:18080,Exposure:projectworkspace.ExposurePreview,
  CreatedBy:operator,
 })
 if err!=nil{t.Fatal(err)}

 adapter:=sandboxrunner.NewAdapter(dataRoot,engine)
 input,_:=json.Marshal(map[string]any{"runtime_id":runtime.ID})
 provisioned,err:=adapter.Invoke(ctx,tool.AdapterRequest{
  ToolID:sandboxrunner.ToolRuntimeEnsure,WorkspaceID:tenant,Input:input,
 })
 if err!=nil{t.Fatalf("managed OCI workspace unavailable: %v",err)}
 var provision struct{ WorkspacePath string `json:"workspace_path"` }
 if err=json.Unmarshal(provisioned.Result,&provision);err!=nil||
  provision.WorkspacePath==""{t.Fatalf("invalid managed Workspace path: %v",err)}
 privateNet,err:=engine.InspectNetwork(ctx,runtime.ID)
 if err!=nil||!privateNet.Internal||privateNet.RuntimeID!=runtime.ID{
  t.Fatalf("rootless internal network not verified: %+v %v",privateNet,err)
 }
 // Only new QA-owned container/network names can be removed. The immutable
 // image, machine-wide Podman store and pre-existing Workspaces are untouched.
 t.Cleanup(func(){
  stopCtx,stop:=context.WithTimeout(context.Background(),40*time.Second)
  defer stop()
  observed,e:=engine.InspectContainer(stopCtx,runtime.ID,app.ID)
  if e==nil&&observed.RuntimeID==runtime.ID&&observed.ApplicationID==app.ID&&
   observed.IsolationVerified&&observed.Image==image&&observed.Name!=""{
   if output,removeErr:=exec.CommandContext(stopCtx,profile.Executable,
    "rm","-f",observed.Name).CombinedOutput();removeErr!=nil{
    t.Errorf("failed to clean owned physical broker container: %v %s",removeErr,string(output))
   }
  }
  network,e:=engine.InspectNetwork(stopCtx,runtime.ID)
  if e==nil&&network.RuntimeID==runtime.ID&&network.Name==privateNet.Name{
   if output,removeErr:=exec.CommandContext(stopCtx,profile.Executable,
    "network","rm",network.Name).CombinedOutput();removeErr!=nil{
    t.Errorf("failed to clean owned physical broker network: %v %s",removeErr,string(output))
   }
  }
 })
 const script=`from http.server import HTTPServer,BaseHTTPRequestHandler
class H(BaseHTTPRequestHandler):
 def do_GET(self):
  if self.path=="/health":
   content=b'{"status":"healthy","private_service_secret":"must_not_escape"}'
   self.send_response(200)
   self.send_header("Content-Type","application/json")
   self.send_header("Content-Length",str(len(content)))
   self.send_header("Set-Cookie","do_not_echo=yes")
   self.end_headers()
   self.wfile.write(content)
  else:
   self.send_response(404)
   self.end_headers()
 def log_message(self,*args): pass
HTTPServer(("0.0.0.0",18080),H).serve_forever()`
 if err=os.WriteFile(filepath.Join(provision.WorkspacePath,"qa_http_service.py"),
  []byte(script),0600);err!=nil{t.Fatal(err)}
 spec:=sandboxrunner.ContainerSpec{
  RuntimeID:runtime.ID,ApplicationID:app.ID,Image:image,
  WorkspacePath:provision.WorkspacePath,NetworkInternal:true,
  Command:[]string{"python3","-u","/workspace/qa_http_service.py"},
  WorkingDir:"/workspace",
  Ports:[]sandboxrunner.PortSpec{{InternalPort:18080,Protocol:"tcp"}},
  Limits:sandboxrunner.ResourceLimits{CPUMillis:1000,MemoryMB:256,PIDs:64},
 }
 container,err:=engine.EnsureContainer(ctx,spec)
 if err!=nil||!sandboxrunner.VerifyNodeLocalServiceSource(dataRoot,runtime.ID,app.ID,container){
  t.Fatalf("real rootless private HTTP app did not verify: %+v %v",container,err)
 }
 var hostPort int
 for _,port:=range container.Ports{
  if port.InternalPort==18080&&port.HostIP=="127.0.0.1"&&port.Protocol=="tcp"{
   hostPort=port.HostPort
  }
 }
 if hostPort<1||hostPort>65535{t.Fatalf("no verified loopback mapping: %+v",container.Ports)}
 if _,err=db.SQL().ExecContext(ctx,
  `UPDATE project_runtimes SET status='running',desired_state='running' WHERE id=?`,runtime.ID);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,
  `UPDATE project_applications SET status='running' WHERE id=?`,app.ID);err!=nil{t.Fatal(err)}
 value,_:=json.Marshal(map[string]any{"container":container})
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO observations(
  id,workspace_id,subject_ref,observation_type,probe_tool_id,probe_tool_version,
  source_principal_id,adapter_id,adapter_version,value_json,confidentiality,
  residency,trust,integrity_hash,observed_at,created_at)
  VALUES('physical-service-obs',? ,?,'project_application_state',
  'project.app.inspect','1',?,'sandbox_runner','1',?,'internal',
  'origin_node','unverified_derived','physical-service-hash',?,?)`,
  tenant,"project_app:"+app.ID,operator,string(value),now,now);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO verifications(
  id,workspace_id,subject_ref,required_level,achieved_level,status,
  spec_json,result_json,verified_by,started_at,completed_at,revision)
  VALUES('physical-service-ver',?,?,'V2','V2','pass','{}',
  '{"observation_id":"physical-service-obs"}',?,?,?,1)`,
  tenant,"project_app:"+app.ID,operator,now,now);err!=nil{t.Fatal(err)}
 if _,err=svc.ApplyEndpointRoute(ctx,projectworkspace.ApplyEndpointRouteCommand{
  EndpointID:endpoint.ID,ApplicationID:app.ID,ApplicationRevision:app.Revision,
  HostIP:"127.0.0.1",HostPort:hostPort,TransportProtocol:"tcp",
  ContainerSpecHash:container.SpecHash,ObservationID:"physical-service-obs",
  VerificationID:"physical-service-ver",ActorPrincipalID:operator,
 });err!=nil{t.Fatalf("actual physical port failed V2 verified route: %v",err)}
 link,err:=svc.CreateWorkspaceServiceLink(ctx,projectworkspace.CreateWorkspaceServiceLinkCommand{
  ProjectID:p.ID,SourceWorkspaceID:world.ID,TargetWorkspaceID:story.ID,
  EndpointID:endpoint.ID,ApprovedPath:"/health",Name:"Private World health",
  ExpiresAtMS:clock.Real{}.UnixMilli()+180000,ActorPrincipalID:operator,
 })
 if err!=nil{t.Fatalf("physical service grant failed: %v",err)}
 broker:=New(svc,engine,dataRoot,node)
 request:=Request{ProjectID:p.ID,TargetWorkspaceID:story.ID,LinkID:link.ID}
 var receipt Receipt
 // The Podman process may take a moment to start listening.
 for tries:=0;tries<8;tries++{
  receipt,err=broker.Probe(ctx,request)
  if err==nil{break}
  if ctx.Err()!=nil{break}
  time.Sleep(300*time.Millisecond)
 }
 if err!=nil||!receipt.Healthy||receipt.Status!="healthy"{
  t.Fatalf("real OCI HTTP route was not reached safely: %+v %v",receipt,err)
 }
 encoded,_:=json.Marshal(receipt)
 for _,secret:=range []string{"must_not_escape","Set-Cookie","127.0.0.1",fmt.Sprintf("%d",hostPort)}{
  if strings.Contains(string(encoded),secret){
   t.Fatalf("broker receipt included raw service metadata or secret: %s",encoded)
  }
 }
 if _,err=broker.Probe(ctx,Request{
  ProjectID:p.ID,TargetWorkspaceID:research.ID,LinkID:link.ID,
 });err==nil{t.Fatal("ungranted Research Workspace reached World service")}
 revoked,err:=svc.SetWorkspaceServiceLink(ctx,projectworkspace.SetWorkspaceServiceLinkCommand{
  LinkID:link.ID,ExpectedRevision:link.Revision,Enabled:false,
  ActorPrincipalID:operator,
 })
 if err!=nil||revoked.Enabled{t.Fatalf("operator service revocation failed: %+v %v",revoked,err)}
 if _,err=broker.Probe(ctx,request);err==nil{
  t.Fatal("revoked Workspace service could still be probed")
 }
 t.Log("Physical rootless World -> Story health probe, declassification, sibling denial and revocation VERIFIED (only when actually executed).")
}
func physicalPtr(v string)*string{return &v}
