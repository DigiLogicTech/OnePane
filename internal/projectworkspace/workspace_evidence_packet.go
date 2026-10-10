package projectworkspace

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "sort"
 "strings"
)

// WorkspaceEvidenceSelection is an explicit version selection, not a grant.
type WorkspaceEvidenceSelection struct {
 AssetID string `json:"asset_id"`
 Version int64 `json:"version"`
}
type WorkspaceEvidenceEntry struct {
 AssetID string `json:"asset_id"`
 Version int64 `json:"version"`
 Name string `json:"name"`
 ContentHash string `json:"content_hash"`
 SizeBytes int64 `json:"size_bytes"`
 MIMEType string `json:"mime_type"`
 CreatedAt int64 `json:"created_at"`
}
type WorkspaceEvidencePacket struct {
 Schema string `json:"schema"`
 ProjectID string `json:"project_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 SnapshotAtMS int64 `json:"snapshot_at_ms"`
 ManifestSHA256 string `json:"manifest_sha256"`
 Entries []WorkspaceEvidenceEntry `json:"entries"`
 // Deliberately no reusable read capability, blob URL, storage path, or
 // raw content; grants must be rechecked on every eventual file read.
}

// BuildWorkspaceEvidencePacket creates only a read-only *metadata* receipt
// from selected, explicitly authorized immutable versions. It does not read
// files, index content, persist an attachment, or grant Council access.
// The consistent SQLite read transaction is essential: every listed version
// must be authorized by the same observed grant/link state.
func(s *Service) BuildWorkspaceEvidencePacket(
 ctx context.Context,projectID,workspaceID string,selections []WorkspaceEvidenceSelection,
)(WorkspaceEvidencePacket,error){
 if s==nil||s.db==nil||strings.TrimSpace(projectID)!=projectID||projectID==""||
  strings.TrimSpace(workspaceID)!=workspaceID||workspaceID==""||
  len(selections)<1||len(selections)>16{
  return WorkspaceEvidencePacket{},ErrInvalidCommand
 }
 // Canonical order makes identical evidence selection hash to the same
 // scope-bound packet even if an operator clicks rows in a different order.
 items:=append([]WorkspaceEvidenceSelection(nil),selections...)
 sort.Slice(items,func(i,j int)bool{
  if items[i].AssetID!=items[j].AssetID{return items[i].AssetID<items[j].AssetID}
  return items[i].Version<items[j].Version
 })
 for i,item:=range items{
  if item.AssetID==""||len(item.AssetID)>128||
   strings.TrimSpace(item.AssetID)!=item.AssetID||item.Version<1{
   return WorkspaceEvidencePacket{},ErrInvalidCommand
  }
  if i>0&&item==items[i-1]{return WorkspaceEvidencePacket{},ErrInvalidCommand}
 }
 tx,err:=s.db.BeginTx(ctx,&sql.TxOptions{ReadOnly:true})
 if err!=nil{return WorkspaceEvidencePacket{},err}
 defer tx.Rollback()
 snapshot:=s.clock.UnixMilli()
 packet:=WorkspaceEvidencePacket{Schema:"onepane.workspace-evidence-metadata/v1",
  ProjectID:projectID,ProjectWorkspaceID:workspaceID,SnapshotAtMS:snapshot,
  Entries:make([]WorkspaceEvidenceEntry,0,len(items))}
 for _,item:=range items{
  var entry WorkspaceEvidenceEntry
  entry.AssetID=item.AssetID;entry.Version=item.Version
  err=tx.QueryRowContext(ctx,`SELECT a.name,v.content_hash,
   COALESCE(v.size_bytes,0),COALESCE(v.mime_type,''),v.created_at
   FROM project_library_assets a
   JOIN project_library_asset_versions v ON v.asset_id=a.id
   JOIN projects p ON p.id=a.project_id AND p.status='active'
   JOIN project_workspaces w ON w.id=? AND w.project_id=p.id AND w.status='active'
   WHERE p.id=? AND a.id=? AND a.archived=0 AND v.version=?
   AND (
    EXISTS (SELECT 1 FROM workspace_library_grants g
      WHERE g.asset_id=a.id AND g.project_workspace_id=w.id
        AND g.enabled=1 AND json_extract(g.permissions_json,'$.read')=1
        AND ((g.version_policy='latest' AND v.version=a.current_version)
          OR (g.version_policy='pinned' AND g.pinned_version=v.version)))
    OR EXISTS (SELECT 1 FROM project_workspace_publications pub
      JOIN project_workspace_links link ON link.id=pub.link_id
      WHERE pub.asset_id=a.id AND pub.asset_version=v.version
        AND pub.content_hash=v.content_hash
        AND link.project_id=p.id
        AND link.target_workspace_id=w.id
        AND link.enabled=1
        AND (link.expires_at_ms IS NULL OR link.expires_at_ms>?)
        AND EXISTS (SELECT 1 FROM project_workspaces source
          WHERE source.id=link.source_workspace_id AND source.project_id=p.id
            AND source.status='active')
    )
   )`,workspaceID,projectID,item.AssetID,item.Version,snapshot).
   Scan(&entry.Name,&entry.ContentHash,&entry.SizeBytes,&entry.MIMEType,&entry.CreatedAt)
  if errors.Is(err,sql.ErrNoRows){return WorkspaceEvidencePacket{},ErrCrossWorkspace}
  if err!=nil{return WorkspaceEvidencePacket{},err}
  if entry.ContentHash==""||entry.SizeBytes<0{
   return WorkspaceEvidencePacket{},ErrCrossWorkspace
  }
  packet.Entries=append(packet.Entries,entry)
 }
 canonical,_:=json.Marshal(struct{
  Schema string `json:"schema"`
  ProjectID string `json:"project_id"`
  WorkspaceID string `json:"project_workspace_id"`
  Entries []WorkspaceEvidenceEntry `json:"entries"`
 }{packet.Schema,packet.ProjectID,packet.ProjectWorkspaceID,packet.Entries})
 digest:=sha256.Sum256(canonical)
 packet.ManifestSHA256="sha256:"+hex.EncodeToString(digest[:])
 if err=tx.Commit();err!=nil{return WorkspaceEvidencePacket{},err}
 return packet,nil
}
