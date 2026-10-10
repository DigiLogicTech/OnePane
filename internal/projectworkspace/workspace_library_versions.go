package projectworkspace

import (
 "context"
)

// WorkspaceLibraryVersions returns ONLY immutable versions currently readable
// by an active Workspace. A Project-owned history endpoint must never be used
// to enumerate peer Workspace outputs or older ungranted versions.
func (s *Service) WorkspaceLibraryVersions(ctx context.Context,projectID,workspaceID,assetID string)([]LibraryVersion,error){
 if projectID==""||workspaceID==""||assetID==""{return nil,ErrInvalidCommand}
 var active int
 err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces
  WHERE id=? AND project_id=? AND status='active'`,workspaceID,projectID).Scan(&active)
 if err!=nil{return nil,err}
 if active!=1{return nil,ErrCrossWorkspace}
 // This predicate deliberately matches ResolveWorkspaceLibraryVersion:
 // direct latest grants expose only the current version; pinned grants expose
 // only their exact version; incoming publications expose only their exact
 // version while the directional link remains enabled. Filtering precedes
 // LIMIT so an older pinned version is not displaced by newer denied rows.
 rows,err:=s.db.QueryContext(ctx,`SELECT v.asset_id,v.version,v.content_hash,
  COALESCE(v.size_bytes,0),COALESCE(v.mime_type,''),v.storage_uri,v.created_at
  FROM project_library_asset_versions v
  JOIN project_library_assets a ON a.id=v.asset_id
  WHERE a.id=? AND a.project_id=? AND a.archived=0
  AND (
   EXISTS(SELECT 1 FROM workspace_library_grants g
    WHERE g.asset_id=a.id AND g.project_workspace_id=? AND g.enabled=1
     AND json_extract(g.permissions_json,'$.read')=1
     AND ((g.version_policy='latest' AND v.version=a.current_version)
      OR (g.version_policy='pinned' AND g.pinned_version=v.version)))
   OR EXISTS(SELECT 1 FROM project_workspace_publications pub
    JOIN project_workspace_links l ON l.id=pub.link_id
    WHERE pub.asset_id=a.id AND pub.asset_version=v.version
     AND pub.content_hash=v.content_hash AND l.project_id=a.project_id
     AND l.target_workspace_id=? AND l.enabled=1
     AND (l.expires_at_ms IS NULL OR l.expires_at_ms>?)
     AND EXISTS(SELECT 1 FROM projects link_project WHERE link_project.id=l.project_id AND link_project.status='active')
     AND EXISTS(SELECT 1 FROM project_workspaces link_source WHERE link_source.id=l.source_workspace_id AND link_source.project_id=l.project_id AND link_source.status='active')
     AND EXISTS(SELECT 1 FROM project_workspaces link_target WHERE link_target.id=l.target_workspace_id AND link_target.project_id=l.project_id AND link_target.status='active'))
  )
  ORDER BY v.version DESC LIMIT 100`,assetID,projectID,workspaceID,workspaceID,s.clock.UnixMilli())
 if err!=nil{return nil,err}
 defer rows.Close()
 versions:=[]LibraryVersion{}
 for rows.Next(){
  var v LibraryVersion
  if err=rows.Scan(&v.AssetID,&v.Version,&v.ContentHash,&v.SizeBytes,
   &v.MIMEType,&v.StorageURI,&v.CreatedAt);err!=nil{return nil,err}
  versions=append(versions,v)
 }
 if err=rows.Err();err!=nil{return nil,err}
 // Do not distinguish ungranted, foreign, archived, and unknown assets.
 if len(versions)==0{return nil,ErrCrossWorkspace}
 return versions,nil
}
