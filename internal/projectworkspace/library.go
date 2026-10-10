package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

type LibraryAsset struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 Name string `json:"name"`
 AssetType string `json:"asset_type"`
 CurrentVersion int64 `json:"current_version"`
 AccessibleVersion int64 `json:"accessible_version,omitempty"` // effective highest version permitted for this Workspace
 Archived bool `json:"archived"`
 CreatedAt int64 `json:"created_at"`
 UpdatedAt int64 `json:"updated_at"`
}
type LibraryVersion struct {
 AssetID string `json:"asset_id"`
 Version int64 `json:"version"`
 ContentHash string `json:"content_hash"`
 SizeBytes int64 `json:"size_bytes"`
 MIMEType string `json:"mime_type"`
 StorageURI string `json:"-"` // internal reference; never reveal blob identity to Library clients
 CreatedAt int64 `json:"created_at"`
}
type ImportLibraryAssetCommand struct {
 ProjectID, Name, MIMEType, ArtifactID, ContentHash, ActorPrincipalID, SourceWorkspaceID string
 SizeBytes int64
}
type GrantLibraryAssetCommand struct {
 ProjectID, AssetID, WorkspaceID, ActorPrincipalID string
 VersionPolicy string
 PinnedVersion int64
}

func (s *Service) ImportLibraryAsset(ctx context.Context,c ImportLibraryAssetCommand)(LibraryAsset,error){
 c.Name=strings.TrimSpace(c.Name);c.MIMEType=strings.TrimSpace(c.MIMEType)
 if c.ProjectID==""||c.ArtifactID==""||c.ContentHash==""||c.ActorPrincipalID==""||len(c.Name)<1||len(c.Name)>240||len(c.MIMEType)<1||len(c.MIMEType)>120||c.SizeBytes<0{return LibraryAsset{},ErrInvalidCommand}
 id,err:=s.ids.New("plasset");if err!=nil{return LibraryAsset{},err}
 eventID,err:=s.ids.New("evt");if err!=nil{return LibraryAsset{},err}
 now:=s.clock.UnixMilli()
 x:=LibraryAsset{ID:id,ProjectID:c.ProjectID,Name:c.Name,AssetType:c.MIMEType,CurrentVersion:1,CreatedAt:now,UpdatedAt:now}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID);if err!=nil{return err}
  if p.Status!="active"{return ErrProjectInactive}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if c.SourceWorkspaceID!=""{
   var count int
   if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces WHERE id=? AND project_id=? AND status='active'`,c.SourceWorkspaceID,p.ID).Scan(&count);err!=nil{return err}
   if count!=1{return ErrCrossWorkspace}
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`,id,c.ProjectID,c.Name,c.MIMEType,now,now);if err!=nil{return err}
  provenance,_:=json.Marshal(map[string]any{"origin":"user_upload","source_workspace_id":c.SourceWorkspaceID,"artifact_id":c.ArtifactID})
  _,err=tx.ExecContext(ctx,`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,provenance_json,created_at) VALUES(?,1,?,?,?,?,?,?)`,id,c.ContentHash,c.SizeBytes,c.MIMEType,"artifact:"+c.ArtifactID,string(provenance),now);if err!=nil{return err}
  if c.SourceWorkspaceID!=""{
   grantID,err:=s.ids.New("plgrant");if err!=nil{return err}
   _,err=tx.ExecContext(ctx,`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,permissions_json,created_at,updated_at) VALUES(?,?,?,1,'latest','{"read":true,"modify":true,"create_derivative":true,"execute":false,"delete":false}',?,?)`,grantID,c.SourceWorkspaceID,id,now,now)
   if err!=nil{return err}
  }
  payload,_:=json.Marshal(map[string]any{"asset_id":id,"source_workspace_id":c.SourceWorkspaceID,"content_hash":c.ContentHash,"artifact_id":c.ArtifactID})
  return s.events.Append(ctx,tx,event.Event{ID:eventID,WorkspaceID:&p.WorkspaceID,Type:"project.library_asset_imported",AggregateType:"library_asset",AggregateID:id,ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now})
 })
 return x,err
}

func (s *Service) LibraryAssets(ctx context.Context,projectID string)([]LibraryAsset,error){
 if strings.TrimSpace(projectID)==""{return nil,ErrInvalidCommand}
 rows,err:=s.db.QueryContext(ctx,`SELECT id,project_id,name,asset_type,current_version,archived,created_at,updated_at FROM project_library_assets WHERE project_id=? AND archived=0 ORDER BY updated_at DESC,id`,projectID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]LibraryAsset{}
 for rows.Next(){
  var x LibraryAsset;var archived int
  if err=rows.Scan(&x.ID,&x.ProjectID,&x.Name,&x.AssetType,&x.CurrentVersion,&archived,&x.CreatedAt,&x.UpdatedAt);err!=nil{return nil,err}
  x.Archived=archived==1;out=append(out,x)
 }
 return out,rows.Err()
}
// SearchLibraryAssets searches only Project-owned, non-archived metadata.
// It intentionally never indexes or reads file contents; content ingestion and
// scoped evidence retrieval are separate, governed operations.
func (s *Service) SearchLibraryAssets(ctx context.Context,projectID,search string)([]LibraryAsset,error){
 if strings.TrimSpace(projectID)==""{return nil,ErrInvalidCommand}
 search=strings.TrimSpace(search)
 if len(search)>256{return nil,ErrInvalidCommand}
 rows,err:=s.db.QueryContext(ctx,`SELECT id,project_id,name,asset_type,current_version,archived,created_at,updated_at
 FROM project_library_assets WHERE project_id=? AND archived=0
 AND (instr(lower(name),lower(?))>0 OR instr(lower(asset_type),lower(?))>0)
 ORDER BY updated_at DESC,id LIMIT 200`,projectID,search,search)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]LibraryAsset{}
 for rows.Next(){
  var asset LibraryAsset;var archived int
  if err=rows.Scan(&asset.ID,&asset.ProjectID,&asset.Name,&asset.AssetType,&asset.CurrentVersion,&archived,&asset.CreatedAt,&asset.UpdatedAt);err!=nil{return nil,err}
  asset.Archived=archived!=0;out=append(out,asset)
 }
 return out,rows.Err()
}

// WorkspaceLibraryAssets lists only assets visible through a direct grant or
// an explicitly enabled, hash-pinned incoming publication. This is a metadata
// inventory, not a capability to read arbitrary versions or artifact bytes.
func (s *Service) WorkspaceLibraryAssets(ctx context.Context,projectID,workspaceID,search string)([]LibraryAsset,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(workspaceID)==""{return nil,ErrInvalidCommand}
 search=strings.TrimSpace(search)
 if len(search)>256{return nil,ErrInvalidCommand}
 var count int
 err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces WHERE id=? AND project_id=? AND status='active'`,workspaceID,projectID).Scan(&count)
 if err!=nil{return nil,err}
 if count!=1{return nil,ErrCrossWorkspace}
 rows,err:=s.db.QueryContext(ctx,`SELECT a.id,a.project_id,a.name,a.asset_type,a.current_version,a.archived,a.created_at,a.updated_at,
  COALESCE((SELECT MAX(v.version) FROM project_library_asset_versions v
    WHERE v.asset_id=a.id AND (
      EXISTS(SELECT 1 FROM workspace_library_grants ag
        WHERE ag.asset_id=a.id AND ag.project_workspace_id=? AND ag.enabled=1
          AND json_extract(ag.permissions_json,'$.read')=1
          AND ((ag.version_policy='latest' AND v.version=a.current_version)
            OR (ag.version_policy='pinned' AND v.version=ag.pinned_version)))
      OR EXISTS(SELECT 1 FROM project_workspace_publications ap
        JOIN project_workspace_links al ON al.id=ap.link_id
        WHERE ap.asset_id=a.id AND ap.asset_version=v.version AND ap.content_hash=v.content_hash
          AND al.project_id=a.project_id AND al.target_workspace_id=? AND al.enabled=1
        AND (al.expires_at_ms IS NULL OR al.expires_at_ms>?)
        AND EXISTS(SELECT 1 FROM projects link_project WHERE link_project.id=al.project_id AND link_project.status='active')
        AND EXISTS(SELECT 1 FROM project_workspaces link_source WHERE link_source.id=al.source_workspace_id AND link_source.project_id=al.project_id AND link_source.status='active')
        AND EXISTS(SELECT 1 FROM project_workspaces link_target WHERE link_target.id=al.target_workspace_id AND link_target.project_id=al.project_id AND link_target.status='active'))
    )),0) AS accessible_version
  FROM project_library_assets a
  WHERE a.project_id=? AND a.archived=0 AND (instr(lower(a.name),lower(?))>0 OR instr(lower(a.asset_type),lower(?))>0)
  AND (
    EXISTS(SELECT 1 FROM workspace_library_grants g
     WHERE g.asset_id=a.id AND g.project_workspace_id=? AND g.enabled=1
       AND json_extract(g.permissions_json,'$.read')=1
       AND (g.version_policy='latest' OR
        EXISTS(SELECT 1 FROM project_library_asset_versions v WHERE v.asset_id=a.id AND v.version=g.pinned_version)))
    OR EXISTS(SELECT 1 FROM project_workspace_publications pub
       JOIN project_workspace_links l ON l.id=pub.link_id
       JOIN project_library_asset_versions v ON v.asset_id=pub.asset_id AND v.version=pub.asset_version AND v.content_hash=pub.content_hash
       WHERE pub.asset_id=a.id AND l.project_id=a.project_id
        AND l.target_workspace_id=? AND l.enabled=1
        AND (l.expires_at_ms IS NULL OR l.expires_at_ms>?)
        AND EXISTS(SELECT 1 FROM projects link_project WHERE link_project.id=l.project_id AND link_project.status='active')
        AND EXISTS(SELECT 1 FROM project_workspaces link_source WHERE link_source.id=l.source_workspace_id AND link_source.project_id=l.project_id AND link_source.status='active')
        AND EXISTS(SELECT 1 FROM project_workspaces link_target WHERE link_target.id=l.target_workspace_id AND link_target.project_id=l.project_id AND link_target.status='active'))
  ) ORDER BY a.updated_at DESC,a.id LIMIT 100`,workspaceID,workspaceID,s.clock.UnixMilli(),projectID,search,search,workspaceID,workspaceID,s.clock.UnixMilli())
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]LibraryAsset{}
 for rows.Next(){var x LibraryAsset;var archived int
  if err=rows.Scan(&x.ID,&x.ProjectID,&x.Name,&x.AssetType,&x.CurrentVersion,&archived,&x.CreatedAt,&x.UpdatedAt,&x.AccessibleVersion);err!=nil{return nil,err}
  if x.AccessibleVersion<1{return nil,ErrCrossWorkspace}
  x.Archived=archived!=0;out=append(out,x)
 }
 return out,rows.Err()
}
func (s *Service) LibraryVersions(ctx context.Context,projectID,assetID string)([]LibraryVersion,error){
 if projectID==""||assetID==""{return nil,ErrInvalidCommand}
 rows,err:=s.db.QueryContext(ctx,`SELECT v.asset_id,v.version,v.content_hash,COALESCE(v.size_bytes,0),COALESCE(v.mime_type,''),v.storage_uri,v.created_at
 FROM project_library_asset_versions v JOIN project_library_assets a ON a.id=v.asset_id
 WHERE v.asset_id=? AND a.project_id=? AND a.archived=0 ORDER BY v.version DESC`,assetID,projectID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]LibraryVersion{}
 for rows.Next(){var x LibraryVersion;if err=rows.Scan(&x.AssetID,&x.Version,&x.ContentHash,&x.SizeBytes,&x.MIMEType,&x.StorageURI,&x.CreatedAt);err!=nil{return nil,err};out=append(out,x)}
 return out,rows.Err()
}
func (s *Service) GrantLibraryAsset(ctx context.Context,c GrantLibraryAssetCommand)error{
 if c.ProjectID==""||c.AssetID==""||c.WorkspaceID==""||c.ActorPrincipalID==""||!(c.VersionPolicy=="latest"||(c.VersionPolicy=="pinned"&&c.PinnedVersion>=1)){return ErrInvalidCommand}
 now:=s.clock.UnixMilli()
 eventID,err:=s.ids.New("evt");if err!=nil{return err}
 return s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID);if err!=nil{return err}
  if p.Status!="active"{return ErrProjectInactive}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  var current int64
  err=tx.QueryRowContext(ctx,`SELECT current_version FROM project_library_assets WHERE id=? AND project_id=? AND archived=0`,c.AssetID,c.ProjectID).Scan(&current)
  if err!=nil{return err}
  var active int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces WHERE id=? AND project_id=? AND status='active'`,c.WorkspaceID,c.ProjectID).Scan(&active)
  if err!=nil{return err};if active!=1{return ErrCrossWorkspace}
  if c.VersionPolicy=="pinned" {
   var count int
   err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_library_asset_versions WHERE asset_id=? AND version=?`,c.AssetID,c.PinnedVersion).Scan(&count)
   if err!=nil{return err};if count!=1{return ErrInvalidCommand}
  }
  id,err:=s.ids.New("plgrant");if err!=nil{return err}
  var pinned any
  if c.VersionPolicy=="pinned"{pinned=c.PinnedVersion}
  _,err=tx.ExecContext(ctx,`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,pinned_version,permissions_json,created_at,updated_at)
    VALUES(?,?,?,1,?,?,'{"read":true,"modify":false,"create_derivative":false,"execute":false,"delete":false}',?,?)
    ON CONFLICT(project_workspace_id,asset_id) DO UPDATE SET enabled=1,version_policy=excluded.version_policy,
      pinned_version=excluded.pinned_version,permissions_json=excluded.permissions_json,updated_at=excluded.updated_at`,id,c.WorkspaceID,c.AssetID,c.VersionPolicy,pinned,now,now)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{"asset_id":c.AssetID,"target_workspace_id":c.WorkspaceID,"policy":c.VersionPolicy,"pinned_version":pinned})
  return s.events.Append(ctx,tx,event.Event{ID:eventID,WorkspaceID:&p.WorkspaceID,Type:"project.library_asset_granted",AggregateType:"library_asset",AggregateID:c.AssetID,ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now})
 })
}

// RevokeLibraryAsset removes direct Workspace access without deleting the
// immutable Project asset, breaking historical evidence references.
func (s *Service) RevokeLibraryAsset(ctx context.Context,projectID,assetID,workspaceID,actor string)error{
 if projectID==""||assetID==""||workspaceID==""||actor==""{return ErrInvalidCommand}
 eventID,err:=s.ids.New("evt");if err!=nil{return err}
 now:=s.clock.UnixMilli()
 return s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  p,err:=s.repo.ProjectTx(ctx,tx,projectID);if err!=nil{return err}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,actor);err!=nil{return err}
  // Project/Workspace/asset ownership are checked together before revocation.
  var valid int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM workspace_library_grants g
    JOIN project_library_assets a ON a.id=g.asset_id
    JOIN project_workspaces w ON w.id=g.project_workspace_id
    WHERE a.id=? AND a.project_id=? AND w.id=? AND w.project_id=a.project_id`,
   assetID,projectID,workspaceID).Scan(&valid)
  if err!=nil{return err};if valid!=1{return ErrCrossWorkspace}
  _,err=tx.ExecContext(ctx,`UPDATE workspace_library_grants SET enabled=0,updated_at=? WHERE asset_id=? AND project_workspace_id=?`,now,assetID,workspaceID)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{"asset_id":assetID,"workspace_id":workspaceID})
  return s.events.Append(ctx,tx,event.Event{ID:eventID,WorkspaceID:&p.WorkspaceID,Type:"project.library_asset_revoked",AggregateType:"library_asset",AggregateID:assetID,ActorPrincipalID:&actor,Payload:payload,OccurredAt:now})
 })
}

// ResolveWorkspaceLibraryVersion enforces an active explicit Library grant OR
// an exact hash-pinned publication on an enabled source -> target channel.
// It exposes a blob reference to the authorised server, not to an AI model.
func (s *Service) ResolveWorkspaceLibraryVersion(ctx context.Context,projectID,workspaceID,assetID string,version int64)(LibraryVersion,error){
 if projectID==""||workspaceID==""||assetID==""||version<1{return LibraryVersion{},ErrInvalidCommand}
 var x LibraryVersion
 var canonicalWorkspace int
 err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces WHERE id=? AND project_id=? AND status='active'`,workspaceID,projectID).Scan(&canonicalWorkspace)
 if err!=nil{return x,err};if canonicalWorkspace!=1{return x,ErrCrossWorkspace}
 err=s.db.QueryRowContext(ctx,`SELECT v.asset_id,v.version,v.content_hash,COALESCE(v.size_bytes,0),COALESCE(v.mime_type,''),v.storage_uri,v.created_at
 FROM project_library_asset_versions v JOIN project_library_assets a ON a.id=v.asset_id
 WHERE a.project_id=? AND a.id=? AND a.archived=0 AND v.version=?
 AND (
  EXISTS(SELECT 1 FROM workspace_library_grants g WHERE g.asset_id=a.id AND g.project_workspace_id=? AND g.enabled=1
    AND json_extract(g.permissions_json,'$.read')=1 AND ((g.version_policy='latest' AND v.version=a.current_version) OR (g.version_policy='pinned' AND g.pinned_version=v.version)))
  OR EXISTS(SELECT 1 FROM project_workspace_publications pub JOIN project_workspace_links l ON l.id=pub.link_id
    WHERE pub.asset_id=a.id AND pub.asset_version=v.version AND pub.content_hash=v.content_hash AND l.project_id=a.project_id AND l.target_workspace_id=? AND l.enabled=1
        AND (l.expires_at_ms IS NULL OR l.expires_at_ms>?)
        AND EXISTS(SELECT 1 FROM projects link_project WHERE link_project.id=l.project_id AND link_project.status='active')
        AND EXISTS(SELECT 1 FROM project_workspaces link_source WHERE link_source.id=l.source_workspace_id AND link_source.project_id=l.project_id AND link_source.status='active')
        AND EXISTS(SELECT 1 FROM project_workspaces link_target WHERE link_target.id=l.target_workspace_id AND link_target.project_id=l.project_id AND link_target.status='active'))
 )`,projectID,assetID,version,workspaceID,workspaceID,s.clock.UnixMilli()).
 Scan(&x.AssetID,&x.Version,&x.ContentHash,&x.SizeBytes,&x.MIMEType,&x.StorageURI,&x.CreatedAt)
 if err==sql.ErrNoRows{return x,ErrCrossWorkspace}
 return x,err
}
