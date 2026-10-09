package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// PublishWorkspaceFileVersionCommand is trusted service-side data already
// validated against an active Task Attempt, pinned OCI application and
// checksum by workspacepublisher. Models cannot select a foreign Project.
type PublishWorkspaceFileVersionCommand struct {
 ProjectID string
 SourceWorkspaceID string
 RelativePath string
 Name string
 MIMEType string
 ArtifactID string
 ContentHash string
 SizeBytes int64
 ActorPrincipalID string
 TaskID string
 AttemptID string
}
type PublishedWorkspaceVersion struct{
 Asset LibraryAsset `json:"asset"`
 Version LibraryVersion `json:"version"`
 Unchanged bool `json:"unchanged"`
}

// PublishWorkspaceFileVersion keeps a single stable Library asset identity
// per source Workspace+path. Every changed content hash appends an immutable
// version, respecting existing cross-Workspace pinned/latest grants. Only the
// creating Workspace receives an automatic grant.
func (s *Service) PublishWorkspaceFileVersion(ctx context.Context,c PublishWorkspaceFileVersionCommand)(PublishedWorkspaceVersion,error){
 if c.ProjectID==""||c.SourceWorkspaceID==""||c.RelativePath==""||
  c.ArtifactID==""||c.ContentHash==""||len(c.ContentHash)!=64||
  c.ActorPrincipalID==""||c.TaskID==""||c.AttemptID==""||
  c.SizeBytes<0||len(c.RelativePath)>512||len(c.Name)>240||
  strings.TrimSpace(c.Name)==""||len(c.MIMEType)>120||c.MIMEType==""{
  return PublishedWorkspaceVersion{},ErrInvalidCommand
 }
 now:=s.clock.UnixMilli()
 result:=PublishedWorkspaceVersion{}
 err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"{return ErrProjectInactive}
  if err:=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  var activeCount int
  if err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces
   WHERE id=? AND project_id=? AND status='active'`,c.SourceWorkspaceID,c.ProjectID).Scan(&activeCount);err!=nil{return err}
  if activeCount!=1{return ErrCrossWorkspace}

  var assetID string
  err=tx.QueryRowContext(ctx,`SELECT asset_id FROM workspace_published_file_assets
   WHERE project_workspace_id=? AND relative_path=?`,c.SourceWorkspaceID,c.RelativePath).Scan(&assetID)
  first:=errors.Is(err,sql.ErrNoRows)
  if err!=nil&&!first{return err}
  version:=int64(1)
  if first{
   assetID,err=s.ids.New("plasset")
   if err!=nil{return err}
   if _,err=tx.ExecContext(ctx,`INSERT INTO project_library_assets
    (id,project_id,name,asset_type,current_version,created_at,updated_at)
    VALUES(?,?,?,?,1,?,?)`,assetID,c.ProjectID,c.Name,c.MIMEType,now,now);err!=nil{return err}
   grantID,e:=s.ids.New("plgrant");if e!=nil{return e}
   if _,err=tx.ExecContext(ctx,`INSERT INTO workspace_library_grants
    (id,project_workspace_id,asset_id,enabled,version_policy,permissions_json,created_at,updated_at)
    VALUES(?,?,?,1,'latest','{"read":true,"modify":true,"create_derivative":true,"execute":false,"delete":false}',?,?)`,
    grantID,c.SourceWorkspaceID,assetID,now,now);err!=nil{return err}
   if _,err=tx.ExecContext(ctx,`INSERT INTO workspace_published_file_assets
    (project_workspace_id,relative_path,asset_id,created_at,updated_at)
    VALUES(?,?,?,?,?)`,c.SourceWorkspaceID,c.RelativePath,assetID,now,now);err!=nil{return err}
  }else{
   var mappedProject string
   var currentVersion int64
   var archived int
   if err=tx.QueryRowContext(ctx,`SELECT project_id,current_version,archived
    FROM project_library_assets WHERE id=?`,assetID).
    Scan(&mappedProject,&currentVersion,&archived);err!=nil{return err}
   if mappedProject!=c.ProjectID||archived!=0||currentVersion<1{return ErrCrossWorkspace}
   var previousHash string
   if err=tx.QueryRowContext(ctx,`SELECT content_hash FROM project_library_asset_versions
    WHERE asset_id=? AND version=?`,assetID,currentVersion).Scan(&previousHash);err!=nil{return err}
   if previousHash==c.ContentHash{
    result.Unchanged=true
    version=currentVersion
   }else{
    version=currentVersion+1
    q,err:=tx.ExecContext(ctx,`UPDATE project_library_assets
     SET current_version=?,updated_at=? WHERE id=? AND current_version=? AND archived=0`,
     version,now,assetID,currentVersion)
    if err!=nil{return err}
    n,e:=q.RowsAffected()
    if e!=nil||n!=1{return fmt.Errorf("%w: concurrent Library version modification",ErrInvalidCommand)}
   }
  }
  if !result.Unchanged{
   provenance,_:=json.Marshal(map[string]any{
    "origin":"verified_workspace_oci_file",
    "source_workspace_id":c.SourceWorkspaceID,"relative_path":c.RelativePath,
    "task_id":c.TaskID,"attempt_id":c.AttemptID,"artifact_id":c.ArtifactID,
   })
   if _,err=tx.ExecContext(ctx,`INSERT INTO project_library_asset_versions
    (asset_id,version,content_hash,size_bytes,mime_type,storage_uri,provenance_json,created_at)
    VALUES(?,?,?,?,?,?,?,?)`,assetID,version,c.ContentHash,c.SizeBytes,
    c.MIMEType,"artifact:"+c.ArtifactID,string(provenance),now);err!=nil{return err}
  }
  // Return the *stored* exact version on unchanged publishes. Never claim
  // a fresh blob became the latest version without actually linking it.
  var storedHash,storedMime,storedURI string
  var storedBytes,storedCreated int64
  if err=tx.QueryRowContext(ctx,`SELECT content_hash,size_bytes,mime_type,storage_uri,created_at
   FROM project_library_asset_versions WHERE asset_id=? AND version=?`,assetID,version).
   Scan(&storedHash,&storedBytes,&storedMime,&storedURI,&storedCreated);err!=nil{return err}
  result.Version=LibraryVersion{AssetID:assetID,Version:version,ContentHash:storedHash,
   SizeBytes:storedBytes,MIMEType:storedMime,StorageURI:storedURI,CreatedAt:storedCreated}
  result.Asset=LibraryAsset{ID:assetID,ProjectID:c.ProjectID,Name:c.Name,AssetType:c.MIMEType,
   CurrentVersion:version,AccessibleVersion:version,CreatedAt:now,UpdatedAt:now}
  if result.Unchanged{return nil}
  eventID,e:=s.ids.New("evt");if e!=nil{return e}
  payload,_:=json.Marshal(map[string]any{
   "asset_id":assetID,"version":version,"content_hash":c.ContentHash,
   "source_workspace_id":c.SourceWorkspaceID,"relative_path":c.RelativePath,
   "task_id":c.TaskID,"artifact_id":c.ArtifactID,
  })
  return s.events.Append(ctx,tx,event.Event{ID:eventID,WorkspaceID:&p.WorkspaceID,
   Type:"project.library_asset_version_published",AggregateType:"library_asset",
   AggregateID:assetID,ActorPrincipalID:&c.ActorPrincipalID,
   Payload:payload,OccurredAt:now})
 })
 return result,err
}

 // LatestWorkspacePublishedFileVersion resolves only an existing stable
 // mapping for an active source Workspace; it never creates/grants an asset.
func (s *Service) LatestWorkspacePublishedFileVersion(ctx context.Context,projectID,workspaceID,relativePath string)(LibraryVersion,error){
 var v LibraryVersion
 if projectID==""||workspaceID==""||relativePath==""{return v,ErrInvalidCommand}
 err:=s.db.QueryRowContext(ctx,`SELECT v.asset_id,v.version,v.content_hash,
 v.size_bytes,v.mime_type,v.storage_uri,v.created_at
 FROM workspace_published_file_assets m
 JOIN project_workspaces pw ON pw.id=m.project_workspace_id AND pw.status='active'
 JOIN project_library_assets a ON a.id=m.asset_id AND a.project_id=pw.project_id AND a.archived=0
 JOIN project_library_asset_versions v ON v.asset_id=a.id AND v.version=a.current_version
 WHERE m.project_workspace_id=? AND m.relative_path=? AND a.project_id=?`,
 workspaceID,relativePath,projectID).Scan(&v.AssetID,&v.Version,&v.ContentHash,
 &v.SizeBytes,&v.MIMEType,&v.StorageURI,&v.CreatedAt)
 return v,err
}
