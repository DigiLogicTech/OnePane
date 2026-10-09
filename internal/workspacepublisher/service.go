package workspacepublisher

import (
 "bytes"
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "path"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
)

var ErrPublishDenied=errors.New("Workspace file publication denied")

// Service performs a second independent authority boundary behind the
// leased, Task-owned OCI tool. A model cannot assign its own source Project,
// principal, active Attempt, or destination Workspace via an input field.
type Service struct{
 db *sql.DB
 artifacts *artifact.Service
 projects *projectworkspace.Service
 localNodeID string
}
func New(db *sql.DB,artifacts *artifact.Service,projects *projectworkspace.Service,localNodeID string)*Service{
 return &Service{db:db,artifacts:artifacts,projects:projects,localNodeID:localNodeID}
}

func (s *Service) PublishWorkspaceFile(ctx context.Context,c sandboxrunner.WorkspacePublicationRequest)(sandboxrunner.WorkspacePublication,error){
 if s==nil||s.db==nil||s.artifacts==nil||s.projects==nil||
  strings.TrimSpace(s.localNodeID)==""||c.TaskID==""||c.AttemptID==""||
  c.WorkspaceID==""||c.RuntimeID==""||c.ApplicationID==""||
  c.Path==""||len(c.Content)>sandboxrunner.WorkspacePublicationLimit{
  return sandboxrunner.WorkspacePublication{},ErrPublishDenied
 }
 if strings.HasPrefix(c.Path,"/")||strings.ContainsAny(c.Path,"\\\x00")||
  path.Clean(c.Path)!=c.Path||strings.Contains(c.Path,"../")||
  strings.Contains(c.Path,"/.git/")||strings.HasPrefix(c.Path,".git/"){
  return sandboxrunner.WorkspacePublication{},ErrPublishDenied
 }
 hash:=sha256.Sum256(c.Content)
 if hex.EncodeToString(hash[:])!=c.ContentHash{
  return sandboxrunner.WorkspacePublication{},fmt.Errorf("%w: content checksum mismatch",ErrPublishDenied)
 }
 var tenant,projectID,projectWorkspaceID,taskState,attemptState string
 var runtimeProject,runtimeWorkspace,projectTenant,projectStatus,workspaceStatus,appRuntime,appStatus,appSource string
 var worker sql.NullString
 var node sql.NullString
 err:=s.db.QueryRowContext(ctx,`SELECT t.workspace_id,t.project_id,t.project_workspace_id,t.state,
 a.status,a.worker_principal_id,r.project_id,r.project_workspace_id,r.node_id,
 p.workspace_id,p.status,pw.status,app.project_runtime_id,app.status,app.source_kind
 FROM tasks t
 JOIN task_attempts a ON a.task_id=t.id AND a.id=?
 JOIN project_runtimes r ON r.id=?
 JOIN projects p ON p.id=r.project_id
 JOIN project_workspaces pw ON pw.id=r.project_workspace_id AND pw.project_id=r.project_id
 JOIN project_applications app ON app.id=? AND app.project_runtime_id=r.id
 WHERE t.id=?`,c.AttemptID,c.RuntimeID,c.ApplicationID,c.TaskID).
 Scan(&tenant,&projectID,&projectWorkspaceID,&taskState,
 &attemptState,&worker,&runtimeProject,&runtimeWorkspace,&node,
 &projectTenant,&projectStatus,&workspaceStatus,&appRuntime,&appStatus,&appSource)
 if err!=nil{return sandboxrunner.WorkspacePublication{},fmt.Errorf("%w: source ownership cannot be verified",ErrPublishDenied)}
 if tenant!=c.WorkspaceID||tenant!=projectTenant||projectID==""||
  projectID!=runtimeProject||projectWorkspaceID==""||
  projectWorkspaceID!=runtimeWorkspace||appRuntime!=c.RuntimeID||
  taskState!="running"||attemptState!="running"||!worker.Valid||
  worker.String==""||projectStatus!="active"||workspaceStatus!="active"||
  appStatus!="running"||appSource!="oci_image"||
  (node.Valid&&node.String!=""&&node.String!=s.localNodeID){
  return sandboxrunner.WorkspacePublication{},ErrPublishDenied
 }
 name:=strings.TrimSpace(c.Name)
 if name==""{name=path.Base(c.Path)}
 if name==""||name=="."||name==".."||len(name)>240||
  strings.ContainsAny(name,"/\\\x00") {
  return sandboxrunner.WorkspacePublication{},ErrPublishDenied
 }
 mime:=strings.TrimSpace(c.MediaType)
 if mime==""{mime="application/octet-stream"}
 if len(mime)>120||strings.ContainsAny(mime,"\r\n\x00")||!strings.Contains(mime,"/"){
  return sandboxrunner.WorkspacePublication{},ErrPublishDenied
 }
 actor:=worker.String
 meta,_:=json.Marshal(map[string]any{
  "source":"verified_workspace_oci_file","task_id":c.TaskID,"attempt_id":c.AttemptID,
  "runtime_id":c.RuntimeID,"application_id":c.ApplicationID,"project_workspace_id":projectWorkspaceID,
  "relative_path":c.Path,"content_hash":c.ContentHash,
 })
 label:=policy.DataLabel{WorkspaceID:tenant,Confidentiality:policy.ConfidentialityInternal,
  Residency:policy.ResidencyOriginNode,Trust:policy.TrustUntrustedContent}
 a,err:=s.artifacts.Create(ctx,artifact.CreateCommand{
  WorkspaceID:tenant,ProjectID:&projectID,MediaType:mime,Label:label,
  CreatedBy:&actor,ActorPrincipalID:&actor,Metadata:meta,
 },bytes.NewReader(c.Content))
 if err!=nil{return sandboxrunner.WorkspacePublication{},err}
 if a.ContentHash!=c.ContentHash||a.SizeBytes!=int64(len(c.Content))||
  s.artifacts.VerifyContent(ctx,a.ID)!=nil{
  return sandboxrunner.WorkspacePublication{},fmt.Errorf("%w: managed artifact integrity not verified",ErrPublishDenied)
 }
 item,err:=s.projects.ImportLibraryAsset(ctx,projectworkspace.ImportLibraryAssetCommand{
  ProjectID:projectID,Name:name,MIMEType:mime,ArtifactID:a.ID,
  ContentHash:a.ContentHash,SizeBytes:a.SizeBytes,
  SourceWorkspaceID:projectWorkspaceID,ActorPrincipalID:actor,
 })
 if err!=nil{return sandboxrunner.WorkspacePublication{},err}
 return sandboxrunner.WorkspacePublication{
  ArtifactID:a.ID,LibraryAssetID:item.ID,Version:item.CurrentVersion,
  ContentHash:a.ContentHash,SizeBytes:a.SizeBytes,SourceWorkspaceID:projectWorkspaceID,
 },nil
}
