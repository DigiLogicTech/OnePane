package projectworkspace

import "context"

// WorkspacePublishedOutput is a verified, currently readable output of a
// durable Workspace Task. A Task ID or a Library asset ID alone is never an
// authorisation to access content from another sandbox.
type WorkspacePublishedOutput struct {
 TaskID string `json:"task_id"`
 RelativePath string `json:"relative_path"`
 AssetID string `json:"asset_id"`
 Version int64 `json:"version"`
 ContentHash string `json:"content_hash"`
 SizeBytes int64 `json:"size_bytes"`
 MIMEType string `json:"mime_type"`
 PublishedAt int64 `json:"published_at"`
}

// WorkspacePublishedOutputs joins completed publication receipts to verified
// immutable Library versions and applies the SAME effective read permissions
// as ResolveWorkspaceLibraryVersion. In-progress/unknown publications, stale
// versions and revoked grants are never advertised as downloadable outputs.
func (s *Service) WorkspacePublishedOutputs(ctx context.Context,projectID,workspaceID string)([]WorkspacePublishedOutput,error){
 if projectID==""||workspaceID==""{return nil,ErrInvalidCommand}
 var active int
 err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces w
 JOIN projects p ON p.id=w.project_id
 WHERE w.id=? AND w.project_id=? AND w.status='active' AND p.status='active'`,
 workspaceID,projectID).Scan(&active)
 if err!=nil{return nil,err}
 if active!=1{return nil,ErrCrossWorkspace}
 rows,err:=s.db.QueryContext(ctx,`SELECT fp.task_id,fp.relative_path,
  v.asset_id,v.version,v.content_hash,COALESCE(v.size_bytes,0),
  COALESCE(v.mime_type,''),fp.updated_at
 FROM workspace_file_publications fp
 JOIN tasks t ON t.id=fp.task_id AND t.project_id=fp.project_id
  AND t.project_workspace_id=fp.project_workspace_id
 JOIN project_library_asset_versions v
  ON v.asset_id=fp.library_asset_id AND v.version=fp.asset_version
  AND v.content_hash=fp.content_hash
  AND v.storage_uri='artifact:' || fp.artifact_id
 JOIN project_library_assets a ON a.id=v.asset_id
  AND a.project_id=fp.project_id AND a.archived=0
 WHERE fp.project_id=? AND fp.project_workspace_id=? AND fp.status='complete'
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
 ORDER BY fp.updated_at DESC,fp.task_id DESC,fp.relative_path
 LIMIT 100`,projectID,workspaceID,workspaceID,workspaceID,s.clock.UnixMilli())
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]WorkspacePublishedOutput{}
 for rows.Next(){
  var item WorkspacePublishedOutput
  if err=rows.Scan(&item.TaskID,&item.RelativePath,&item.AssetID,&item.Version,
   &item.ContentHash,&item.SizeBytes,&item.MIMEType,&item.PublishedAt);err!=nil{return nil,err}
  out=append(out,item)
 }
 return out,rows.Err()
}
