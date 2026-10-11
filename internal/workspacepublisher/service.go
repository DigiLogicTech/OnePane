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
 "time"

 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
)

var ErrPublishDenied=errors.New("Workspace file publication denied")
var ErrPublicationRecoveryRequired=errors.New("Workspace publication has an uncertain previous outcome; recovery required before retry")

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
  c.Path==""||len(c.Content)>sandboxrunner.WorkspaceLargePublicationLimit{
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
 var runtimeProject,runtimeWorkspace,runtimeStatus,projectTenant,projectStatus,workspaceStatus,appRuntime,appStatus,appSource string
 var worker sql.NullString
 var node sql.NullString
 err:=s.db.QueryRowContext(ctx,`SELECT t.workspace_id,t.project_id,t.project_workspace_id,t.state,
 a.status,a.worker_principal_id,r.project_id,r.project_workspace_id,r.node_id,r.status,
 p.workspace_id,p.status,pw.status,app.project_runtime_id,app.status,app.source_kind
 FROM tasks t
 JOIN task_attempts a ON a.task_id=t.id AND a.id=?
 JOIN project_runtimes r ON r.id=?
 JOIN projects p ON p.id=r.project_id
 JOIN project_workspaces pw ON pw.id=r.project_workspace_id AND pw.project_id=r.project_id
 JOIN project_applications app ON app.id=? AND app.project_runtime_id=r.id
 WHERE t.id=?`,c.AttemptID,c.RuntimeID,c.ApplicationID,c.TaskID).
 Scan(&tenant,&projectID,&projectWorkspaceID,&taskState,
 &attemptState,&worker,&runtimeProject,&runtimeWorkspace,&node,&runtimeStatus,
 &projectTenant,&projectStatus,&workspaceStatus,&appRuntime,&appStatus,&appSource)
 if err!=nil{return sandboxrunner.WorkspacePublication{},fmt.Errorf("%w: source ownership cannot be verified",ErrPublishDenied)}
 if tenant!=c.WorkspaceID||tenant!=projectTenant||projectID==""||
  projectID!=runtimeProject||projectWorkspaceID==""||
  projectWorkspaceID!=runtimeWorkspace||appRuntime!=c.RuntimeID||
  taskState!="running"||attemptState!="running"||runtimeStatus!="running"||!worker.Valid||
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
 // Reserve the immutable (Task, relative path, source SHA-256) key
 // BEFORE creating a blob or a new Library asset. A repeated completed
 // publication returns the same artifact and Library version, not a copy.
 // An interrupted previous attempt stays in_progress until independently
 // reconciled; never assume a prior external side effect failed.
 existing,recorded,err:=s.reservePublication(ctx,c,projectID,projectWorkspaceID)
 if err!=nil{return sandboxrunner.WorkspacePublication{},err}
 if recorded{return existing,nil}
 actor:=worker.String
 // Reuse a verified immutable artifact for identical content at the same
 // source Workspace path, even across independent Tasks. No duplicate
 // artifact row, Library asset or version for an unchanged build.
 var stored artifact.Artifact
 previous,e:=s.projects.LatestWorkspacePublishedFileVersion(ctx,projectID,projectWorkspaceID,c.Path)
 if e==nil&&previous.ContentHash==c.ContentHash{
  if !strings.HasPrefix(previous.StorageURI,"artifact:"){
   return sandboxrunner.WorkspacePublication{},ErrPublishDenied
  }
  priorID:=strings.TrimPrefix(previous.StorageURI,"artifact:")
  stored,e=s.artifacts.Get(ctx,priorID)
  if e!=nil||stored.ProjectID==nil||*stored.ProjectID!=projectID||
   stored.WorkspaceID!=tenant||stored.ContentHash!="sha256:"+c.ContentHash||
   stored.SizeBytes!=int64(len(c.Content))||
   s.artifacts.VerifyContent(ctx,priorID)!=nil{
   return sandboxrunner.WorkspacePublication{},ErrPublishDenied
  }
 }else{
  if e!=nil&&!errors.Is(e,sql.ErrNoRows){return sandboxrunner.WorkspacePublication{},e}
  meta,_:=json.Marshal(map[string]any{
   "source":"verified_workspace_oci_file","task_id":c.TaskID,"attempt_id":c.AttemptID,
   "runtime_id":c.RuntimeID,"application_id":c.ApplicationID,"project_workspace_id":projectWorkspaceID,
   "relative_path":c.Path,"content_hash":c.ContentHash,
  })
  // Origin-scoped artifacts must identify their actual approved local Node.
  // Without this field policy.ValidateDataLabel rejects every genuine
  // Workspace publication before the immutable artifact can be recorded.
  label:=policy.DataLabel{WorkspaceID:tenant,Confidentiality:policy.ConfidentialityInternal,
   Residency:policy.ResidencyOriginNode,OriginNodeID:s.localNodeID,
   Trust:policy.TrustUntrustedContent}
  stored,e=s.artifacts.Create(ctx,artifact.CreateCommand{
   WorkspaceID:tenant,ProjectID:&projectID,MediaType:mime,Label:label,
   CreatedBy:&actor,ActorPrincipalID:&actor,Metadata:meta,
  },bytes.NewReader(c.Content))
  if e!=nil{return sandboxrunner.WorkspacePublication{},e}
  if stored.ContentHash!="sha256:"+c.ContentHash||stored.SizeBytes!=int64(len(c.Content))||
   s.artifacts.VerifyContent(ctx,stored.ID)!=nil{
   return sandboxrunner.WorkspacePublication{},fmt.Errorf("%w: managed artifact integrity not verified",ErrPublishDenied)
  }
 }
 if _,err=s.db.ExecContext(ctx,`UPDATE workspace_file_publications
 SET artifact_id=?,updated_at=? WHERE task_id=? AND relative_path=? AND content_hash=?
 AND status='in_progress'`,stored.ID,time.Now().UnixMilli(),c.TaskID,c.Path,c.ContentHash);err!=nil{
  return sandboxrunner.WorkspacePublication{},err
 }
 item,err:=s.projects.PublishWorkspaceFileVersion(ctx,projectworkspace.PublishWorkspaceFileVersionCommand{
  ProjectID:projectID,SourceWorkspaceID:projectWorkspaceID,RelativePath:c.Path,
  Name:name,MIMEType:mime,ArtifactID:stored.ID,ContentHash:c.ContentHash,
  SizeBytes:stored.SizeBytes,ActorPrincipalID:actor,TaskID:c.TaskID,AttemptID:c.AttemptID,
 })
 if err!=nil{return sandboxrunner.WorkspacePublication{},err}
 if _,err=s.db.ExecContext(ctx,`UPDATE workspace_file_publications
 SET status='complete',artifact_id=?,library_asset_id=?,asset_version=?,updated_at=?
 WHERE task_id=? AND relative_path=? AND content_hash=? AND status='in_progress'`,
 stored.ID,item.Asset.ID,item.Version.Version,time.Now().UnixMilli(),c.TaskID,c.Path,c.ContentHash);err!=nil{
  return sandboxrunner.WorkspacePublication{},err
 }
 return sandboxrunner.WorkspacePublication{
  ArtifactID:stored.ID,LibraryAssetID:item.Asset.ID,Version:item.Version.Version,
  ContentHash:item.Version.ContentHash,SizeBytes:item.Version.SizeBytes,
  SourceWorkspaceID:projectWorkspaceID,
 },nil
}

func (s *Service) reservePublication(ctx context.Context,c sandboxrunner.WorkspacePublicationRequest,projectID,workspaceID string)(sandboxrunner.WorkspacePublication,bool,error){
 now:=time.Now().UnixMilli()
 res,err:=s.db.ExecContext(ctx,`INSERT INTO workspace_file_publications(
 task_id,project_id,project_workspace_id,runtime_id,application_id,
 relative_path,content_hash,status,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,'in_progress',?,?)
 ON CONFLICT(task_id,relative_path,content_hash) DO NOTHING`,
 c.TaskID,projectID,workspaceID,c.RuntimeID,c.ApplicationID,c.Path,c.ContentHash,now,now)
 if err!=nil{return sandboxrunner.WorkspacePublication{},false,err}
 count,err:=res.RowsAffected()
 if err!=nil{return sandboxrunner.WorkspacePublication{},false,err}
 if count==1{return sandboxrunner.WorkspacePublication{},false,nil}
 var p,w,r,a,status string
 var artifactID,assetID sql.NullString
 var version sql.NullInt64
 err=s.db.QueryRowContext(ctx,`SELECT project_id,project_workspace_id,runtime_id,
 application_id,status,artifact_id,library_asset_id,asset_version
 FROM workspace_file_publications WHERE task_id=? AND relative_path=? AND content_hash=?`,
 c.TaskID,c.Path,c.ContentHash).
 Scan(&p,&w,&r,&a,&status,&artifactID,&assetID,&version)
 if err!=nil||p!=projectID||w!=workspaceID||r!=c.RuntimeID||a!=c.ApplicationID{
  return sandboxrunner.WorkspacePublication{},false,ErrPublishDenied
 }
 recovered:=false
 if status!="complete"{
  // Recovery can only *recognize* a verified existing Library version:
  // it must never create a second Artifact or Library asset on uncertainty.
  if !artifactID.Valid{return sandboxrunner.WorkspacePublication{},false,ErrPublicationRecoveryRequired}
  rows,err:=s.db.QueryContext(ctx,`SELECT v.asset_id,v.version
   FROM project_library_asset_versions v JOIN project_library_assets a ON a.id=v.asset_id
   WHERE v.storage_uri=? AND a.project_id=? AND a.archived=0`,
   "artifact:"+artifactID.String,projectID)
  if err!=nil{return sandboxrunner.WorkspacePublication{},false,err}
  count:=0
  var candidateID string
  var candidateVersion int64
  for rows.Next(){
   if err=rows.Scan(&candidateID,&candidateVersion);err!=nil{break}
   count++
   if count>1{break}
  }
  if scanErr:=rows.Err();err==nil{err=scanErr}
  _=rows.Close()
  if err!=nil{return sandboxrunner.WorkspacePublication{},false,err}
  if count!=1{return sandboxrunner.WorkspacePublication{},false,ErrPublicationRecoveryRequired}
  assetID=sql.NullString{String:candidateID,Valid:true}
  version=sql.NullInt64{Int64:candidateVersion,Valid:true}
  recovered=true
 }
 if !artifactID.Valid||!assetID.Valid||!version.Valid||version.Int64<1{
  return sandboxrunner.WorkspacePublication{},false,ErrPublishDenied
 }
 // A ledger status is not evidence of content integrity or of an effective
 // source Workspace grant. Verify both before reusing a prior receipt.
 stored,err:=s.artifacts.Get(ctx,artifactID.String)
 if err!=nil||stored.ProjectID==nil||*stored.ProjectID!=projectID||
  stored.WorkspaceID!=c.WorkspaceID||stored.ContentHash!="sha256:"+c.ContentHash||
  stored.SizeBytes!=int64(len(c.Content))||
  s.artifacts.VerifyContent(ctx,artifactID.String)!=nil{
  return sandboxrunner.WorkspacePublication{},false,ErrPublishDenied
 }
 v,err:=s.projects.ResolveWorkspaceLibraryVersion(ctx,projectID,workspaceID,assetID.String,version.Int64)
 if err!=nil||v.ContentHash!=c.ContentHash||v.Version!=version.Int64{
  return sandboxrunner.WorkspacePublication{},false,ErrPublishDenied
 }
 if recovered{
  _,err=s.db.ExecContext(ctx,`UPDATE workspace_file_publications SET
   status='complete',library_asset_id=?,asset_version=?,updated_at=?
   WHERE task_id=? AND relative_path=? AND content_hash=?
   AND status='in_progress' AND artifact_id=?`,
   assetID.String,version.Int64,time.Now().UnixMilli(),c.TaskID,c.Path,c.ContentHash,artifactID.String)
  if err!=nil{return sandboxrunner.WorkspacePublication{},false,err}
 }
 return sandboxrunner.WorkspacePublication{
  ArtifactID:artifactID.String,LibraryAssetID:assetID.String,Version:v.Version,
  ContentHash:c.ContentHash,SizeBytes:stored.SizeBytes,SourceWorkspaceID:workspaceID,
 },true,nil
}
