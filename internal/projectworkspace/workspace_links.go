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

// WorkspaceLink is an explicit, revocable Project-level transfer capability.
// It does not mount a filesystem, copy secrets, or grant agent tool authority.
type WorkspaceLink struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 SourceWorkspaceID string `json:"source_workspace_id"`
 TargetWorkspaceID string `json:"target_workspace_id"`
 Name string `json:"name"`
 Kind string `json:"kind"`
 Enabled bool `json:"enabled"`
 Revision int64 `json:"revision"`
 CreatedBy string `json:"created_by"`
 CreatedAt int64 `json:"created_at"`
 UpdatedAt int64 `json:"updated_at"`
}

type WorkspacePublication struct {
 ID string `json:"id"`
 LinkID string `json:"link_id"`
 AssetID string `json:"asset_id"`
 AssetName string `json:"asset_name"`
 AssetVersion int64 `json:"asset_version"`
 ContentHash string `json:"content_hash"`
 PublishedBy string `json:"published_by"`
 PublishedAt int64 `json:"published_at"`
}

type CreateWorkspaceLinkCommand struct {
 ProjectID, SourceWorkspaceID, TargetWorkspaceID, Name, ActorPrincipalID string
 Enable bool
}
type ToggleWorkspaceLinkCommand struct {
 LinkID, ActorPrincipalID string
 ExpectedRevision int64
 Enabled bool
}
type PublishWorkspaceAssetCommand struct {
 LinkID, AssetID, ActorPrincipalID string
 Version int64
}

func scanWorkspaceLink(row scanner) (WorkspaceLink,error) {
 var x WorkspaceLink
 var enabled int
 err:=row.Scan(&x.ID,&x.ProjectID,&x.SourceWorkspaceID,&x.TargetWorkspaceID,
  &x.Name,&x.Kind,&enabled,&x.Revision,&x.CreatedBy,&x.CreatedAt,&x.UpdatedAt)
 x.Enabled=enabled!=0
 return x,err
}
const workspaceLinkSelect = `SELECT id,project_id,source_workspace_id,target_workspace_id,name,kind,enabled,revision,created_by,created_at,updated_at FROM project_workspace_links WHERE id=?`

func (s *Service) WorkspaceLink(ctx context.Context,id string) (WorkspaceLink,error) {
 if s==nil||s.db==nil||strings.TrimSpace(id)=="" {return WorkspaceLink{},ErrInvalidCommand}
 return scanWorkspaceLink(s.db.QueryRowContext(ctx,workspaceLinkSelect,id))
}
func (s *Service) WorkspaceLinks(ctx context.Context,projectID string) ([]WorkspaceLink,error) {
 if strings.TrimSpace(projectID)=="" {return nil,ErrInvalidCommand}
 rows,err:=s.db.QueryContext(ctx,`SELECT id,project_id,source_workspace_id,target_workspace_id,name,kind,enabled,revision,created_by,created_at,updated_at FROM project_workspace_links WHERE project_id=? ORDER BY created_at,id`,projectID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]WorkspaceLink{}
 for rows.Next(){link,err:=scanWorkspaceLink(rows);if err!=nil{return nil,err};out=append(out,link)}
 return out,rows.Err()
}
func (s *Service) CreateWorkspaceLink(ctx context.Context,c CreateWorkspaceLinkCommand) (WorkspaceLink,error) {
 c.Name=strings.TrimSpace(c.Name)
 if c.ProjectID==""||c.SourceWorkspaceID==""||c.TargetWorkspaceID==""||
 c.SourceWorkspaceID==c.TargetWorkspaceID||c.ActorPrincipalID==""||
 len(c.Name)==0||len(c.Name)>120 {return WorkspaceLink{},ErrInvalidCommand}
 id,err:=s.ids.New("pwlink");if err!=nil{return WorkspaceLink{},err}
 eventID,err:=s.ids.New("evt");if err!=nil{return WorkspaceLink{},err}
 now:=s.clock.UnixMilli()
 link:=WorkspaceLink{ID:id,ProjectID:c.ProjectID,SourceWorkspaceID:c.SourceWorkspaceID,
 TargetWorkspaceID:c.TargetWorkspaceID,Name:c.Name,Kind:"artifacts",Enabled:c.Enable,
 Revision:1,CreatedBy:c.ActorPrincipalID,CreatedAt:now,UpdatedAt:now}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx) error {
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID);if err!=nil{return err}
  if p.Status!="active"{return ErrProjectInactive}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  var count int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces WHERE project_id=? AND status='active' AND id IN (?,?)`,
   c.ProjectID,c.SourceWorkspaceID,c.TargetWorkspaceID).Scan(&count)
  if err!=nil{return err}
  if count!=2{return ErrCrossWorkspace}
  enabled:=0;if c.Enable{enabled=1}
  _,err=tx.ExecContext(ctx,`INSERT INTO project_workspace_links(id,project_id,source_workspace_id,target_workspace_id,name,kind,enabled,revision,created_by,created_at,updated_at) VALUES(?,?,?,?,?,'artifacts',?,1,?,?,?)`,
   id,c.ProjectID,c.SourceWorkspaceID,c.TargetWorkspaceID,c.Name,enabled,c.ActorPrincipalID,now,now)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{"link_id":id,"source_workspace_id":c.SourceWorkspaceID,"target_workspace_id":c.TargetWorkspaceID,"enabled":c.Enable,"kind":"artifacts"})
  return s.events.Append(ctx,tx,event.Event{ID:eventID,WorkspaceID:&p.WorkspaceID,Type:"project.workspace_link_created",AggregateType:"workspace_link",AggregateID:id,ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now})
 })
 if err!=nil{return WorkspaceLink{},err}
 return link,nil
}
func (s *Service) SetWorkspaceLinkEnabled(ctx context.Context,c ToggleWorkspaceLinkCommand) (WorkspaceLink,error) {
 if c.LinkID==""||c.ActorPrincipalID==""||c.ExpectedRevision<1 {return WorkspaceLink{},ErrInvalidCommand}
 eventID,err:=s.ids.New("evt");if err!=nil{return WorkspaceLink{},err}
 now:=s.clock.UnixMilli()
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  link,err:=scanWorkspaceLink(tx.QueryRowContext(ctx,workspaceLinkSelect,c.LinkID));if err!=nil{return err}
  p,err:=s.repo.ProjectTx(ctx,tx,link.ProjectID);if err!=nil{return err}
  if p.Status!="active"{return ErrProjectInactive}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if link.Revision!=c.ExpectedRevision{return ErrInvalidTransition}
  if c.Enabled{
   var active int
   err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces WHERE project_id=? AND status='active' AND id IN (?,?)`,
    link.ProjectID,link.SourceWorkspaceID,link.TargetWorkspaceID).Scan(&active)
   if err!=nil{return err};if active!=2{return ErrCrossWorkspace}
  }
  enabled:=0;if c.Enabled{enabled=1}
  changed,err:=tx.ExecContext(ctx,`UPDATE project_workspace_links SET enabled=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`,enabled,now,c.LinkID,c.ExpectedRevision)
  if err!=nil{return err}
  n,_:=changed.RowsAffected();if n!=1{return ErrInvalidTransition}
  payload,_:=json.Marshal(map[string]any{"link_id":link.ID,"enabled":c.Enabled})
  return s.events.Append(ctx,tx,event.Event{ID:eventID,WorkspaceID:&p.WorkspaceID,Type:"project.workspace_link_updated",AggregateType:"workspace_link",AggregateID:link.ID,ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now})
 })
 if err!=nil{return WorkspaceLink{},err}
 return s.WorkspaceLink(ctx,c.LinkID)
}
func (s *Service) PublishWorkspaceAsset(ctx context.Context,c PublishWorkspaceAssetCommand)(WorkspacePublication,error){
 if c.LinkID==""||c.AssetID==""||c.ActorPrincipalID==""||c.Version<1{return WorkspacePublication{},ErrInvalidCommand}
 id,err:=s.ids.New("pwpub");if err!=nil{return WorkspacePublication{},err}
 eventID,err:=s.ids.New("evt");if err!=nil{return WorkspacePublication{},err}
 now:=s.clock.UnixMilli()
 var published WorkspacePublication
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  link,err:=scanWorkspaceLink(tx.QueryRowContext(ctx,workspaceLinkSelect,c.LinkID));if err!=nil{return err}
  if !link.Enabled{return ErrInvalidTransition}
  p,err:=s.repo.ProjectTx(ctx,tx,link.ProjectID);if err!=nil{return err}
  if p.Status!="active"{return ErrProjectInactive}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  var active int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces WHERE id IN (?,?) AND project_id=? AND status='active'`,link.SourceWorkspaceID,link.TargetWorkspaceID,link.ProjectID).Scan(&active)
  if err!=nil{return err};if active!=2{return ErrCrossWorkspace}
  var name,hash string
  err=tx.QueryRowContext(ctx,`SELECT a.name,v.content_hash FROM project_library_assets a
   JOIN project_library_asset_versions v ON v.asset_id=a.id
   JOIN workspace_library_grants g ON g.asset_id=a.id
   WHERE a.id=? AND v.version=? AND a.project_id=? AND a.archived=0
     AND g.project_workspace_id=? AND g.enabled=1
     AND json_extract(g.permissions_json,'$.read')=1
     AND json_extract(g.permissions_json,'$.create_derivative')=1
     AND ((g.version_policy='latest' AND v.version=a.current_version) OR (g.version_policy='pinned' AND g.pinned_version=?))`,
   c.AssetID,c.Version,link.ProjectID,link.SourceWorkspaceID,c.Version).Scan(&name,&hash)
  if errors.Is(err,sql.ErrNoRows){return ErrCrossWorkspace}
  if err!=nil{return err}
  if strings.TrimSpace(hash)==""{return ErrInvalidCommand}
  _,err=tx.ExecContext(ctx,`INSERT INTO project_workspace_publications(id,link_id,asset_id,asset_version,content_hash,published_by,published_at) VALUES(?,?,?,?,?,?,?)`,
   id,c.LinkID,c.AssetID,c.Version,hash,c.ActorPrincipalID,now)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{"publication_id":id,"link_id":link.ID,"asset_id":c.AssetID,"version":c.Version,"content_hash":hash})
  if err=s.events.Append(ctx,tx,event.Event{ID:eventID,WorkspaceID:&p.WorkspaceID,Type:"project.workspace_asset_published",AggregateType:"workspace_publication",AggregateID:id,ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now});err!=nil{return err}
  published=WorkspacePublication{ID:id,LinkID:link.ID,AssetID:c.AssetID,AssetName:name,AssetVersion:c.Version,ContentHash:hash,PublishedBy:c.ActorPrincipalID,PublishedAt:now}
  return nil
 })
 return published,err
}
// ListWorkspacePublications returns metadata only. It does NOT issue a Library
// read grant or return bytes. A future attachment/retrieval endpoint must
// check the active link + specific publication + version on every access.
func (s *Service) WorkspacePublications(ctx context.Context,linkID string)([]WorkspacePublication,error){
 if linkID==""{return nil,ErrInvalidCommand}
 link,err:=s.WorkspaceLink(ctx,linkID);if err!=nil{return nil,err}
 if !link.Enabled{return nil,ErrInvalidTransition}
 rows,err:=s.db.QueryContext(ctx,`SELECT pub.id,pub.link_id,pub.asset_id,a.name,pub.asset_version,pub.content_hash,pub.published_by,pub.published_at
  FROM project_workspace_publications pub
  JOIN project_library_assets a ON a.id=pub.asset_id AND a.project_id=?
  JOIN project_library_asset_versions v ON v.asset_id=pub.asset_id AND v.version=pub.asset_version AND v.content_hash=pub.content_hash
  WHERE pub.link_id=? AND a.archived=0 ORDER BY pub.published_at DESC,pub.id`,link.ProjectID,linkID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]WorkspacePublication{}
 for rows.Next(){var x WorkspacePublication;if err=rows.Scan(&x.ID,&x.LinkID,&x.AssetID,&x.AssetName,&x.AssetVersion,&x.ContentHash,&x.PublishedBy,&x.PublishedAt);err!=nil{return nil,err};out=append(out,x)}
 return out,rows.Err()
}
