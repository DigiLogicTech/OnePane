//go:build integration

package projectworkspace

import (
 "context"
 "testing"
 
 "github.com/DigiLogicTech/OnePane/internal/clock"
 sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestScopedWorkspaceServiceApprovalsCannotEscapeOrOutliveVerifiedRoute(t *testing.T){
 ctx:=context.Background()
 db,err:=sqlitestore.Open(t.TempDir()+"/service-links.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err=db.Migrate(ctx);err!=nil{t.Fatal(err)}
 now:=clock.Real{}.UnixMilli()
 for _,q:=range []string{
  `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at)
   VALUES('tenant','Tenant','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('operator','human','Operator','active',1,?,?)`,
  `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at)
   VALUES('agent','service','Agent','active',1,?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','operator','active',?,?)`,
  `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at)
   VALUES('tenant','agent','active',?,?)`,
 }{
  if _,err=db.SQL().ExecContext(ctx,q,now,now);err!=nil{t.Fatal(err)}
 }
 svc:=NewService(db.SQL(),db,clock.Real{})
 project,err:=svc.CreateProject(ctx,CreateProjectCommand{
  WorkspaceID:"tenant",Name:"Service collaboration",CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 source,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"World",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 target,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Story",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 stranger,err:=svc.CreateWorkspaceView(ctx,CreateWorkspaceViewCommand{
  ProjectID:project.ID,Name:"Unrelated research",ActorPrincipalID:"operator",
 })
 if err!=nil{t.Fatal(err)}
 runtime,err:=svc.CreateRuntime(ctx,CreateRuntimeCommand{
  ProjectID:project.ID,ProjectWorkspaceID:&source.ID,CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 app,err:=svc.DeclareApplication(ctx,DeclareApplicationCommand{
  RuntimeID:runtime.ID,Name:"World preview",SourceKind:AppOCIImage,
  SourceRef:"ghcr.io/example/world@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 endpoint,err:=svc.DeclareEndpoint(ctx,DeclareEndpointCommand{
  RuntimeID:runtime.ID,ApplicationID:&app.ID,Name:"World HTTP",
  Protocol:"http",InternalPort:8080,Exposure:ExposurePreview,CreatedBy:"operator",
 })
 if err!=nil{t.Fatal(err)}
 for _,q:=range []struct{sql string;args []any}{
  {`UPDATE project_runtimes SET status='running' WHERE id=?`,[]any{runtime.ID}},
  {`UPDATE project_applications SET status='running' WHERE id=?`,[]any{app.ID}},
 }{
  if _,err=db.SQL().ExecContext(ctx,q.sql,q.args...);err!=nil{t.Fatal(err)}
 }
 observation:=`{"container":{"application_id":"`+app.ID+`","status":"running","isolation_verified":true,"spec_hash":"approved-spec","ports":[{"internal_port":8080,"protocol":"tcp","host_ip":"127.0.0.1","host_port":49152}]}}`
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO observations(
  id,workspace_id,subject_ref,observation_type,probe_tool_id,probe_tool_version,
  source_principal_id,adapter_id,adapter_version,value_json,
  confidentiality,residency,trust,integrity_hash,observed_at,created_at)
  VALUES('service-obs','tenant',?,'project_application_state','project.app.inspect','1',
  'operator','sandbox_runner','1',?,'internal','origin_node','unverified_derived','hash',?,?)`,
  "project_app:"+app.ID,observation,now,now);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`INSERT INTO verifications(
  id,workspace_id,subject_ref,required_level,achieved_level,status,
  spec_json,result_json,verified_by,started_at,completed_at,revision)
  VALUES('service-ver','tenant',?,'V2','V2','pass','{}',
  '{"observation_id":"service-obs"}','operator',?,?,1)`,
  "project_app:"+app.ID,now,now);err!=nil{t.Fatal(err)}
 if _,err=svc.ApplyEndpointRoute(ctx,ApplyEndpointRouteCommand{
  EndpointID:endpoint.ID,ApplicationID:app.ID,
  ApplicationRevision:app.Revision,HostIP:"127.0.0.1",HostPort:49152,
  TransportProtocol:"tcp",ContainerSpecHash:"approved-spec",
  ObservationID:"service-obs",VerificationID:"service-ver",
  ActorPrincipalID:"operator",
 });err!=nil{t.Fatalf("verified endpoint route unavailable: %v",err)}

 request:=CreateWorkspaceServiceLinkCommand{
  ProjectID:project.ID,SourceWorkspaceID:source.ID,
  TargetWorkspaceID:target.ID,EndpointID:endpoint.ID,Name:"World readiness",
  ApprovedPath:"/health",ExpiresAtMS:now+60000,ActorPrincipalID:"operator",
 }
 for _,test:=range []struct{name string;mutate func(*CreateWorkspaceServiceLinkCommand)}{
  {"self_link",func(x *CreateWorkspaceServiceLinkCommand){x.TargetWorkspaceID=source.ID}},
  {"foreign_endpoint",func(x *CreateWorkspaceServiceLinkCommand){x.SourceWorkspaceID=stranger.ID}},
  {"unknown_target",func(x *CreateWorkspaceServiceLinkCommand){x.TargetWorkspaceID="foreign"}},
  {"untrusted_agent",func(x *CreateWorkspaceServiceLinkCommand){x.ActorPrincipalID="agent"}},
  {"host_url",func(x *CreateWorkspaceServiceLinkCommand){x.ApprovedPath="http://localhost:8080"}},
  {"query_injection",func(x *CreateWorkspaceServiceLinkCommand){x.ApprovedPath="/health?url=evil"}},
  {"path_traversal",func(x *CreateWorkspaceServiceLinkCommand){x.ApprovedPath="/../secret"}},
  {"url_escape",func(x *CreateWorkspaceServiceLinkCommand){x.ApprovedPath="/%2e%2e/secret"}},
  {"unbounded_grant",func(x *CreateWorkspaceServiceLinkCommand){x.ExpiresAtMS=now+40*24*60*60*1000}},
  {"past_expiry",func(x *CreateWorkspaceServiceLinkCommand){x.ExpiresAtMS=now-1000}},
 }{
  t.Run(test.name,func(t *testing.T){bad:=request;test.mutate(&bad)
   if _,err:=svc.CreateWorkspaceServiceLink(ctx,bad);err==nil{
    t.Fatal("unsafe service approval accepted")
   }
  })
 }
 approved,err:=svc.CreateWorkspaceServiceLink(ctx,request)
 if err!=nil{t.Fatal(err)}
 if approved.Expired||!approved.Enabled||approved.ApprovedPath!="/health"{
  t.Fatalf("approved service identity changed: %+v",approved)
 }
 resolved,err:=svc.ResolveWorkspaceServiceRoute(ctx,project.ID,target.ID,approved.ID)
 if err!=nil||resolved.Route.HostIP!="127.0.0.1"||
  resolved.Route.HostPort!=49152||resolved.Route.VerificationID!="service-ver"{
  t.Fatalf("approved exact loopback route unavailable: %+v %v",resolved,err)
 }
 for _,wrongTarget:=range []string{source.ID,stranger.ID,"foreign"}{
  if _,err=svc.ResolveWorkspaceServiceRoute(ctx,project.ID,wrongTarget,approved.ID);err==nil{
   t.Fatalf("foreign Workspace %s inherited approved route",wrongTarget)
  }
 }
 if _,err=svc.ResolveWorkspaceServiceRoute(ctx,"different-project",target.ID,approved.ID);err==nil{
  t.Fatal("foreign Project resolved route")
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_endpoint_routes
  SET container_spec_hash='different-spec' WHERE endpoint_id=?`,endpoint.ID);err!=nil{t.Fatal(err)}
 if _,err=svc.ResolveWorkspaceServiceRoute(ctx,project.ID,target.ID,approved.ID);err==nil{
  t.Fatal("changed verified OCI spec silently reused previous approval")
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_endpoint_routes
  SET container_spec_hash='approved-spec' WHERE endpoint_id=?`,endpoint.ID);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_workspaces SET status='archived'
  WHERE id=?`,source.ID);err!=nil{t.Fatal(err)}
 if _,err=svc.ResolveWorkspaceServiceRoute(ctx,project.ID,target.ID,approved.ID);err==nil{
  t.Fatal("archived source retained access")
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_workspaces SET status='active'
  WHERE id=?`,source.ID);err!=nil{t.Fatal(err)}
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_workspace_service_links
  SET expires_at_ms=? WHERE id=?`,now-1000,approved.ID);err!=nil{t.Fatal(err)}
 if _,err=svc.ResolveWorkspaceServiceRoute(ctx,project.ID,target.ID,approved.ID);err==nil{
  t.Fatal("expired service grant still resolved")
 }
 if _,err=svc.SetWorkspaceServiceLink(ctx,SetWorkspaceServiceLinkCommand{
  LinkID:approved.ID,ExpectedRevision:approved.Revision,
  Enabled:true,ActorPrincipalID:"operator",
 });err==nil{t.Fatal("expired grant reactivated without explicit new deadline")}
 renewed,err:=svc.SetWorkspaceServiceLink(ctx,SetWorkspaceServiceLinkCommand{
  LinkID:approved.ID,ExpectedRevision:approved.Revision,Enabled:true,
  ExpiresAtMS:clock.Real{}.UnixMilli()+120000,ActorPrincipalID:"operator",
 })
 if err!=nil||renewed.Expired{t.Fatalf("operator renewal failed: %+v %v",renewed,err)}
 if _,err=svc.SetWorkspaceServiceLink(ctx,SetWorkspaceServiceLinkCommand{
  LinkID:approved.ID,ExpectedRevision:approved.Revision,
  Enabled:false,ActorPrincipalID:"operator",
 });err==nil{t.Fatal("stale revision revoked a newer approval")}
 disabled,err:=svc.SetWorkspaceServiceLink(ctx,SetWorkspaceServiceLinkCommand{
  LinkID:approved.ID,ExpectedRevision:renewed.Revision,
  Enabled:false,ActorPrincipalID:"operator",
 })
 if err!=nil||disabled.Enabled{t.Fatalf("revocation failed: %+v %v",disabled,err)}
 if _,err=svc.ResolveWorkspaceServiceRoute(ctx,project.ID,target.ID,approved.ID);err==nil{
  t.Fatal("revoked grant still resolved")
 }
 if _,err=db.SQL().ExecContext(ctx,`UPDATE project_applications
  SET revision=revision+1 WHERE id=?`,app.ID);err!=nil{t.Fatal(err)}
 if _,err=svc.SetWorkspaceServiceLink(ctx,SetWorkspaceServiceLinkCommand{
  LinkID:approved.ID,ExpectedRevision:disabled.Revision,
  Enabled:true,ExpiresAtMS:clock.Real{}.UnixMilli()+120000,
  ActorPrincipalID:"operator",
 });err==nil{t.Fatal("new application revision reused stale verified route")}
}
