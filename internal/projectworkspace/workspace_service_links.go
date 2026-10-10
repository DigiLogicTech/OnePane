package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// WorkspaceServiceLink is an operator-approved directional *policy record*.
// It does not grant general TCP access, host access, credentials or direct
// OCI-to-OCI networking. The transport remains disabled until independently
// verified on the physical Node.
type WorkspaceServiceLink struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 SourceWorkspaceID string `json:"source_workspace_id"`
 TargetWorkspaceID string `json:"target_workspace_id"`
 EndpointID string `json:"endpoint_id"`
 Name string `json:"name"`
 ApprovedPath string `json:"approved_path"`
 Enabled bool `json:"enabled"`
 Expired bool `json:"expired"`
 ExpiresAtMS int64 `json:"expires_at_ms"`
 Revision int64 `json:"revision"`
 CreatedBy string `json:"created_by"`
 CreatedAt int64 `json:"created_at"`
 UpdatedAt int64 `json:"updated_at"`
 // These are recorded for audit, not arbitrary caller-controlled addresses.
 ApplicationID string `json:"application_id"`
 ApplicationRevision int64 `json:"application_revision"`
 EndpointRevision int64 `json:"endpoint_revision"`
 SpecHash string `json:"-"`
 VerificationID string `json:"-"`
}

type CreateWorkspaceServiceLinkCommand struct {
 ProjectID string
 SourceWorkspaceID string
 TargetWorkspaceID string
 EndpointID string
 Name string
 ApprovedPath string
 ExpiresAtMS int64
 ActorPrincipalID string
}
type SetWorkspaceServiceLinkCommand struct {
 LinkID string
 ExpectedRevision int64
 Enabled bool
 ExpiresAtMS int64 // required for renewal; zero retains the previous deadline
 ActorPrincipalID string
}

// Safe single-path HTTP readiness requests only. No query/fragment/escaped
// characters, dot segments, host URL, authorization header or dynamic method.
// Approval by itself never sends a request.
func validWorkspaceServicePath(p string)bool {
 if len(p)<1||len(p)>128||p[0]!='/'||strings.HasPrefix(p,"//"){return false}
 for _,segment:=range strings.Split(p,"/"){
  if segment=="."||segment==".."{return false}
 }
 for _,r:=range p{
  if !((r>='a'&&r<='z')||(r>='A'&&r<='Z')||
   (r>='0'&&r<='9')||r=='/'||r=='_'||r=='-'||r=='.'){return false}
 }
 return true
}
const workspaceServiceGrantMaxMS int64=30*24*60*60*1000
func validWorkspaceServiceExpiry(now,expiry int64)bool {
 return expiry>now&&expiry-now<=workspaceServiceGrantMaxMS
}

const workspaceServiceSelect = `SELECT id,project_id,source_workspace_id,target_workspace_id,
 endpoint_id,name,approved_path,enabled,expires_at_ms,revision,created_by,
 created_at,updated_at,application_id,application_revision,endpoint_revision,
 container_spec_hash,verification_id
 FROM project_workspace_service_links`

func scanWorkspaceServiceLink(row scanner,now int64)(WorkspaceServiceLink,error){
 var out WorkspaceServiceLink
 var enabled int
 err:=row.Scan(&out.ID,&out.ProjectID,&out.SourceWorkspaceID,&out.TargetWorkspaceID,
  &out.EndpointID,&out.Name,&out.ApprovedPath,&enabled,&out.ExpiresAtMS,
  &out.Revision,&out.CreatedBy,&out.CreatedAt,&out.UpdatedAt,&out.ApplicationID,
  &out.ApplicationRevision,&out.EndpointRevision,&out.SpecHash,&out.VerificationID)
 if err!=nil{return WorkspaceServiceLink{},err}
 out.Enabled=enabled==1
 out.Expired=out.ExpiresAtMS<=now
 return out,nil
}

func (s *Service) WorkspaceServiceLink(ctx context.Context,id string)(WorkspaceServiceLink,error){
 if s==nil||s.db==nil||id==""{return WorkspaceServiceLink{},ErrInvalidCommand}
 return scanWorkspaceServiceLink(s.db.QueryRowContext(ctx,workspaceServiceSelect+` WHERE id=?`,id),s.clock.UnixMilli())
}
func (s *Service) WorkspaceServiceLinks(ctx context.Context,projectID string)([]WorkspaceServiceLink,error){
 if s==nil||s.db==nil||projectID==""{return nil,ErrInvalidCommand}
 rows,err:=s.db.QueryContext(ctx,workspaceServiceSelect+` WHERE project_id=? ORDER BY created_at,id LIMIT 100`,projectID)
 if err!=nil{return nil,err}
 defer rows.Close()
 links:=[]WorkspaceServiceLink{}
 for rows.Next(){
  v,e:=scanWorkspaceServiceLink(rows,s.clock.UnixMilli())
  if e!=nil{return nil,e}
  links=append(links,v)
 }
 return links,rows.Err()
}

// sourceRoute checks only the already-verified loopback HTTP route and its
// canonical Workspace ownership. Never accepts a model-selected host/port.
func (s *Service) sourceWorkspaceServiceRoute(ctx context.Context,projectID,sourceID,endpointID string)(IngressRoute,error){
 route,err:=s.ResolveIngressRoute(ctx,endpointID)
 if err!=nil{return IngressRoute{},ErrCrossWorkspace}
 if route.ProjectID!=projectID||route.Endpoint.Protocol!="http"||
  route.Endpoint.ApplicationID==nil||route.Route.ApplicationID!=*route.Endpoint.ApplicationID||
  route.Route.ContainerSpecHash==""||route.Route.VerificationID==""{
  return IngressRoute{},ErrCrossWorkspace
 }
 var valid int
 err=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_runtime_endpoints ep
 JOIN project_runtimes rt ON rt.id=ep.project_runtime_id
 JOIN project_applications app ON app.id=ep.application_id AND app.project_runtime_id=rt.id
 JOIN project_workspaces pw ON pw.id=rt.project_workspace_id
 WHERE ep.id=? AND rt.project_id=? AND rt.project_workspace_id=?
 AND rt.status='running' AND rt.isolation_mode='sandboxed_container'
 AND app.status='running' AND app.source_kind='oci_image'
 AND pw.status='active' AND pw.project_id=rt.project_id`,
 endpointID,projectID,sourceID).Scan(&valid)
 if err!=nil{return IngressRoute{},err}
 if valid!=1{return IngressRoute{},ErrCrossWorkspace}
 return route,nil
}

func (s *Service) CreateWorkspaceServiceLink(ctx context.Context,c CreateWorkspaceServiceLinkCommand)(WorkspaceServiceLink,error){
 now:=s.clock.UnixMilli()
 c.Name=strings.TrimSpace(c.Name)
 if c.ProjectID==""||c.SourceWorkspaceID==""||c.TargetWorkspaceID==""||
  c.SourceWorkspaceID==c.TargetWorkspaceID||c.EndpointID==""||
  c.ActorPrincipalID==""||len(c.Name)<1||len(c.Name)>120||
  !validWorkspaceServicePath(c.ApprovedPath)||!validWorkspaceServiceExpiry(now,c.ExpiresAtMS){
  return WorkspaceServiceLink{},ErrInvalidCommand
 }
 live,err:=s.sourceWorkspaceServiceRoute(ctx,c.ProjectID,c.SourceWorkspaceID,c.EndpointID)
 if err!=nil{return WorkspaceServiceLink{},err}
 id,err:=s.ids.New("pwsvc");if err!=nil{return WorkspaceServiceLink{},err}
 eventID,err:=s.ids.New("evt");if err!=nil{return WorkspaceServiceLink{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"{return ErrProjectInactive}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  var active int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces
   WHERE id IN (?,?) AND project_id=? AND status='active'`,
   c.SourceWorkspaceID,c.TargetWorkspaceID,c.ProjectID).Scan(&active)
  if err!=nil{return err}
  if active!=2{return ErrCrossWorkspace}
  // Guard the gap between independently verified route lookup and commit.
  if err=checkWorkspaceServiceApprovalTx(ctx,tx,c.EndpointID,live);err!=nil{return err}
  _,err=tx.ExecContext(ctx,`INSERT INTO project_workspace_service_links(
   id,project_id,source_workspace_id,target_workspace_id,endpoint_id,
   application_id,name,approved_path,enabled,expires_at_ms,revision,
   application_revision,endpoint_revision,container_spec_hash,verification_id,
   created_by,created_at,updated_at)
   VALUES(?,?,?,?,?,?,?,?,1,?,1,?,?,?,?,?,?,?)`,
   id,c.ProjectID,c.SourceWorkspaceID,c.TargetWorkspaceID,c.EndpointID,
   live.Route.ApplicationID,c.Name,c.ApprovedPath,c.ExpiresAtMS,
   live.Route.ApplicationRevision,live.Route.EndpointRevision,live.Route.ContainerSpecHash,
   live.Route.VerificationID,c.ActorPrincipalID,now,now)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{
   "service_link_id":id,"source_workspace_id":c.SourceWorkspaceID,
   "target_workspace_id":c.TargetWorkspaceID,"endpoint_id":c.EndpointID,
   "approved_path":c.ApprovedPath,"expires_at_ms":c.ExpiresAtMS,
   "verification_id":live.Route.VerificationID,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&p.WorkspaceID,Type:"project.workspace_service_link_approved",
   AggregateType:"workspace_service_link",AggregateID:id,ActorPrincipalID:&c.ActorPrincipalID,
   Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return WorkspaceServiceLink{},err}
 return s.WorkspaceServiceLink(ctx,id)
}

func checkWorkspaceServiceApprovalTx(ctx context.Context,tx storage.Tx,endpointID string,route IngressRoute)error{
 var matched int
 err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_runtime_endpoints ep
 JOIN project_endpoint_routes er ON er.endpoint_id=ep.id
 JOIN project_applications app ON app.id=er.application_id AND app.id=ep.application_id
 JOIN project_runtimes rt ON rt.id=ep.project_runtime_id AND rt.id=er.project_runtime_id
 WHERE ep.id=? AND ep.desired_state='enabled' AND ep.status='ready'
 AND ep.revision=? AND ep.protocol='http'
 AND er.status='verified' AND er.host_ip='127.0.0.1'
 AND er.transport_protocol='tcp' AND er.host_port BETWEEN 1 AND 65535
 AND er.application_revision=? AND er.endpoint_revision=ep.revision
 AND er.verification_id=? AND er.container_spec_hash=?
 AND app.revision=er.application_revision AND app.status='running'
 AND rt.status='running' AND rt.project_id=?`,
 endpointID,route.Route.EndpointRevision,route.Route.ApplicationRevision,
 route.Route.VerificationID,route.Route.ContainerSpecHash,route.ProjectID).Scan(&matched)
 if err!=nil{return err}
 if matched!=1{return ErrInvalidTransition}
 return nil
}

func (s *Service) SetWorkspaceServiceLink(ctx context.Context,c SetWorkspaceServiceLinkCommand)(WorkspaceServiceLink,error){
 if c.LinkID==""||c.ActorPrincipalID==""||c.ExpectedRevision<1{return WorkspaceServiceLink{},ErrInvalidCommand}
 now:=s.clock.UnixMilli()
 var route IngressRoute
 if c.Enabled{
  old,err:=s.WorkspaceServiceLink(ctx,c.LinkID)
  if err!=nil{return WorkspaceServiceLink{},err}
  route,err=s.sourceWorkspaceServiceRoute(ctx,old.ProjectID,old.SourceWorkspaceID,old.EndpointID)
  if err!=nil{return WorkspaceServiceLink{},err}
 }
 eventID,err:=s.ids.New("evt");if err!=nil{return WorkspaceServiceLink{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  old,err:=scanWorkspaceServiceLink(tx.QueryRowContext(ctx,workspaceServiceSelect+` WHERE id=?`,c.LinkID),now)
  if err!=nil{return err}
  if old.Revision!=c.ExpectedRevision{return ErrRevisionConflict}
  p,err:=s.repo.ProjectTx(ctx,tx,old.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"{return ErrProjectInactive}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  expiry:=old.ExpiresAtMS
  if c.ExpiresAtMS!=0{expiry=c.ExpiresAtMS}
  if c.Enabled{
   if !validWorkspaceServiceExpiry(now,expiry){return ErrInvalidCommand}
   var active int
   if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces
    WHERE project_id=? AND status='active' AND id IN (?,?)`,
    old.ProjectID,old.SourceWorkspaceID,old.TargetWorkspaceID).Scan(&active);err!=nil{return err}
   if active!=2{return ErrCrossWorkspace}
   if err=checkWorkspaceServiceApprovalTx(ctx,tx,old.EndpointID,route);err!=nil{return err}
  }else if c.ExpiresAtMS!=0{
   // Disabling a grant must not silently extend its deadline.
   return ErrInvalidCommand
  }
  var appID,spec,verification string
  var appRev,endpointRev int64
  if c.Enabled {
   appID,spec,verification=route.Route.ApplicationID,route.Route.ContainerSpecHash,route.Route.VerificationID
   appRev,endpointRev=route.Route.ApplicationRevision,route.Route.EndpointRevision
  }else{
   appID,spec,verification=old.ApplicationID,old.SpecHash,old.VerificationID
   appRev,endpointRev=old.ApplicationRevision,old.EndpointRevision
  }
  enabled:=0;if c.Enabled{enabled=1}
  changed,err:=tx.ExecContext(ctx,`UPDATE project_workspace_service_links
   SET enabled=?,expires_at_ms=?,application_id=?,application_revision=?,
   endpoint_revision=?,container_spec_hash=?,verification_id=?,
   revision=revision+1,updated_at=?
   WHERE id=? AND revision=?`,
   enabled,expiry,appID,appRev,endpointRev,spec,verification,now,c.LinkID,c.ExpectedRevision)
  if err!=nil{return err}
  n,err:=changed.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  payload,_:=json.Marshal(map[string]any{
   "service_link_id":c.LinkID,"enabled":c.Enabled,"revision":old.Revision+1,
   "expires_at_ms":expiry,"verification_id":verification,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&p.WorkspaceID,Type:"project.workspace_service_link_updated",
   AggregateType:"workspace_service_link",AggregateID:c.LinkID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return WorkspaceServiceLink{},err}
 return s.WorkspaceServiceLink(ctx,c.LinkID)
}

// ResolveWorkspaceServiceRoute is for a future privileged broker ONLY.
// It yields no network access by itself. The target identity is mandatory;
// direct source access, transitive forwarding and expired/stale links deny.
func (s *Service) ResolveWorkspaceServiceRoute(ctx context.Context,projectID,targetID,linkID string)(IngressRoute,error){
 if s==nil||projectID==""||targetID==""||linkID==""{return IngressRoute{},ErrInvalidCommand}
 link,err:=s.WorkspaceServiceLink(ctx,linkID)
 if err!=nil{return IngressRoute{},ErrCrossWorkspace}
 if link.ProjectID!=projectID||link.TargetWorkspaceID!=targetID||
  !link.Enabled||link.Expired{return IngressRoute{},ErrCrossWorkspace}
 var count int
 err=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspace_service_links l
 JOIN projects p ON p.id=l.project_id AND p.status='active'
 JOIN project_workspaces source ON source.id=l.source_workspace_id
  AND source.project_id=p.id AND source.status='active'
 JOIN project_workspaces target ON target.id=l.target_workspace_id
  AND target.project_id=p.id AND target.status='active'
 WHERE l.id=? AND l.project_id=? AND l.target_workspace_id=? AND l.enabled=1
 AND l.expires_at_ms>?`,linkID,projectID,targetID,s.clock.UnixMilli()).Scan(&count)
 if err!=nil{return IngressRoute{},err}
 if count!=1{return IngressRoute{},ErrCrossWorkspace}
 route,err:=s.sourceWorkspaceServiceRoute(ctx,projectID,link.SourceWorkspaceID,link.EndpointID)
 if err!=nil{return IngressRoute{},ErrCrossWorkspace}
 if route.Route.ApplicationID!=link.ApplicationID||
  route.Route.ApplicationRevision!=link.ApplicationRevision||
  route.Route.EndpointRevision!=link.EndpointRevision||
  route.Route.ContainerSpecHash!=link.SpecHash||
  route.Route.VerificationID!=link.VerificationID{
  return IngressRoute{},ErrInvalidTransition
 }
 return route,nil
}
