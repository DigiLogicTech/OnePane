package projectworkspace

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "sort"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// ToolchainRequirement is a declaration of desired software, not an attested
// installed version. The preflight Tool checks executable presence separately.
type ToolchainRequirement struct {
 Executable string `json:"executable"`
 VersionConstraint string `json:"version_constraint,omitempty"`
}

// ApprovedToolchainManifest has an append-only human-authorized revision.
// State "approved_unverified" does NOT imply that installed binaries, versions,
// package provenance, CPU/GPU placement or the physical Node were verified.
type ApprovedToolchainManifest struct {
 ProjectID string `json:"project_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 ApplicationID string `json:"application_id"`
 ImageRef string `json:"image_ref"`
 ApplicationRevision int64 `json:"application_revision"`
 Requirements []ToolchainRequirement `json:"requirements"`
 ManifestSHA256 string `json:"manifest_sha256"`
 Revision int64 `json:"revision"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
 CurrentApplicationMatches bool `json:"current_application_matches"`
 Status string `json:"status"`
}

type ApproveToolchainManifestCommand struct {
 ProjectID string
 ProjectWorkspaceID string
 ApplicationID string
 ExpectedRevision int64 // zero means none yet; compare-and-swap otherwise
 Requirements []ToolchainRequirement
 ActorPrincipalID string
 RequestID,TraceID *string
}

const maxApprovedToolchainRequirements=32

func approvedToolchainName(name string)bool{
 if name==""||len(name)>128||name=="."||name==".."{return false}
 for _,r:=range name{
  if !((r>='A'&&r<='Z')||(r>='a'&&r<='z')||(r>='0'&&r<='9')||r=='_'||r=='-'||r=='.'||r=='+'){
   return false
  }
 }
 return true
}
func approvedVersionConstraint(v string)bool{
 if len(v)>64{return false}
 for _,c:=range v{
  if !((c>='A'&&c<='Z')||(c>='a'&&c<='z')||
   (c>='0'&&c<='9')||strings.ContainsRune(" ._-+^~=<>*,",c)){
   return false
  }
 }
 return true
}
func normalizeToolchainRequirements(in []ToolchainRequirement)([]ToolchainRequirement,error){
 if len(in)<1||len(in)>maxApprovedToolchainRequirements{return nil,ErrInvalidCommand}
 out:=make([]ToolchainRequirement,len(in))
 seen:=map[string]bool{}
 for i,r:=range in{
  r.VersionConstraint=strings.TrimSpace(r.VersionConstraint)
  if !approvedToolchainName(r.Executable)||!approvedVersionConstraint(r.VersionConstraint)||
   seen[r.Executable]{return nil,ErrInvalidCommand}
  seen[r.Executable]=true
  out[i]=r
 }
 sort.Slice(out,func(i,j int)bool{return out[i].Executable<out[j].Executable})
 return out,nil
}

func toolchainManifestHash(projectID,workspaceID,appID,image string,appRevision int64,req []ToolchainRequirement)string{
 // Marshal a stable typed structure, never caller-defined JSON object order.
 canonical,_:=json.Marshal(struct{
  ProjectID string `json:"project_id"`
  WorkspaceID string `json:"project_workspace_id"`
  ApplicationID string `json:"application_id"`
  ImageRef string `json:"image_ref"`
  ApplicationRevision int64 `json:"application_revision"`
  Requirements []ToolchainRequirement `json:"requirements"`
 }{projectID,workspaceID,appID,image,appRevision,req})
 sum:=sha256.Sum256(canonical)
 return hex.EncodeToString(sum[:])
}
const latestToolchainManifestSQL=`SELECT project_id,project_workspace_id,application_id,image_ref,
 application_revision,requirements_json,manifest_sha256,revision,approved_by,approved_at
 FROM project_workspace_toolchain_manifests
 WHERE project_id=? AND project_workspace_id=? ORDER BY revision DESC LIMIT 1`

func scanToolchainManifest(row scanner)(ApprovedToolchainManifest,error){
 var out ApprovedToolchainManifest
 var required string
 if err:=row.Scan(&out.ProjectID,&out.ProjectWorkspaceID,&out.ApplicationID,
  &out.ImageRef,&out.ApplicationRevision,&required,&out.ManifestSHA256,
  &out.Revision,&out.ApprovedBy,&out.ApprovedAt);err!=nil{return out,err}
 if err:=json.Unmarshal([]byte(required),&out.Requirements);err!=nil{return out,err}
 canonical,err:=normalizeToolchainRequirements(out.Requirements)
 if err!=nil{return out,fmt.Errorf("persisted toolchain requirements invalid: %w",err)}
 if toolchainManifestHash(out.ProjectID,out.ProjectWorkspaceID,out.ApplicationID,
  out.ImageRef,out.ApplicationRevision,canonical)!=out.ManifestSHA256{
  return out,errors.New("persisted toolchain manifest integrity mismatch")
 }
 out.Requirements=canonical
 out.Status="approved_unverified"
 return out,nil
}

func (s *Service) WorkspaceToolchainManifest(ctx context.Context,projectID,workspaceID string)(ApprovedToolchainManifest,error){
 if s==nil||s.db==nil||projectID==""||workspaceID==""{return ApprovedToolchainManifest{},ErrInvalidCommand}
 // Authoritative Project/Workspace membership on every read; archived
 // Workspaces cannot retain access to historical approval settings.
 var active int
 err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces pw JOIN projects p
 ON p.id=pw.project_id WHERE pw.id=? AND p.id=? AND pw.status='active'
 AND p.status='active'`,workspaceID,projectID).Scan(&active)
 if err!=nil{return ApprovedToolchainManifest{},err}
 if active!=1{return ApprovedToolchainManifest{},ErrCrossWorkspace}
 m,err:=scanToolchainManifest(s.db.QueryRowContext(ctx,latestToolchainManifestSQL,projectID,workspaceID))
 if err!=nil{return ApprovedToolchainManifest{},err}
 var currentImage string
 var revision int64
 err=s.db.QueryRowContext(ctx,`SELECT a.source_ref,a.revision
 FROM project_applications a JOIN project_runtimes r ON r.id=a.project_runtime_id
 WHERE a.id=? AND r.project_id=? AND r.project_workspace_id=?
 AND a.source_kind='oci_image'`,m.ApplicationID,projectID,workspaceID).Scan(&currentImage,&revision)
 if err!=nil&&err!=sql.ErrNoRows{return ApprovedToolchainManifest{},err}
 m.CurrentApplicationMatches=err==nil&&currentImage==m.ImageRef&&revision==m.ApplicationRevision
 if !m.CurrentApplicationMatches{m.Status="stale_application_changed"}
 return m,nil
}

func (s *Service) ApproveWorkspaceToolchainManifest(ctx context.Context,c ApproveToolchainManifestCommand)(ApprovedToolchainManifest,error){
 if s==nil||s.db==nil||s.tx==nil||c.ProjectID==""||c.ProjectWorkspaceID==""||
  c.ApplicationID==""||c.ActorPrincipalID==""||c.ExpectedRevision<0{
  return ApprovedToolchainManifest{},ErrInvalidCommand
 }
 requirements,err:=normalizeToolchainRequirements(c.Requirements)
 if err!=nil{return ApprovedToolchainManifest{},err}
 reqJSON,err:=json.Marshal(requirements);if err!=nil{return ApprovedToolchainManifest{},err}
 eventID,err:=s.ids.New("evt");if err!=nil{return ApprovedToolchainManifest{},err}
 now:=s.clock.UnixMilli()
 revision:=c.ExpectedRevision+1
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"{return ErrProjectInactive}
  if err:=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  // The approval cannot be forged by autonomous agents, service principals
  // or Watchdogs; only a live human member with API project.write/project.run
  // authorization may approve the record.
  var principalType string
  if err:=tx.QueryRowContext(ctx,`SELECT principal_type FROM principals WHERE id=? AND status='active'`,
   c.ActorPrincipalID).Scan(&principalType);err!=nil{return err}
  if principalType!="human"{return ErrPrincipalIneligible}
  var workspaceActive int
  if err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces
 WHERE id=? AND project_id=? AND status='active'`,c.ProjectWorkspaceID,c.ProjectID).Scan(&workspaceActive);err!=nil{return err}
  if workspaceActive!=1{return ErrCrossWorkspace}
  var image,kind string
  var appRevision int64
  err=tx.QueryRowContext(ctx,`SELECT a.source_ref,a.source_kind,a.revision
 FROM project_applications a JOIN project_runtimes r ON r.id=a.project_runtime_id
 WHERE a.id=? AND r.project_id=? AND r.project_workspace_id=?`,
   c.ApplicationID,c.ProjectID,c.ProjectWorkspaceID).Scan(&image,&kind,&appRevision)
  if errors.Is(err,sql.ErrNoRows){return ErrCrossWorkspace}
  if err!=nil{return err}
  if kind!="oci_image"||!pinnedOCIImageSource(image){return ErrInvalidCommand}
  var latest int64
  if err:=tx.QueryRowContext(ctx,`SELECT COALESCE(MAX(revision),0)
 FROM project_workspace_toolchain_manifests WHERE project_workspace_id=?`,
   c.ProjectWorkspaceID).Scan(&latest);err!=nil{return err}
  if latest!=c.ExpectedRevision{return ErrRevisionConflict}
  hash:=toolchainManifestHash(c.ProjectID,c.ProjectWorkspaceID,c.ApplicationID,image,appRevision,requirements)
  _,err=tx.ExecContext(ctx,`INSERT INTO project_workspace_toolchain_manifests(
   project_workspace_id,revision,project_id,application_id,image_ref,application_revision,
   requirements_json,manifest_sha256,approved_by,approved_at)
   VALUES(?,?,?,?,?,?,?,?,?,?)`,
   c.ProjectWorkspaceID,revision,c.ProjectID,c.ApplicationID,image,appRevision,
   string(reqJSON),hash,c.ActorPrincipalID,now)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{
   "project_id":c.ProjectID,"project_workspace_id":c.ProjectWorkspaceID,
   "application_id":c.ApplicationID,"manifest_sha256":hash,
   "manifest_revision":revision,"requirements_count":len(requirements),
  })
  return s.events.Append(ctx,tx,event.Event{ID:eventID,WorkspaceID:&p.WorkspaceID,
   Type:"project_workspace.toolchain_manifest_approved",
   AggregateType:"project_workspace",AggregateID:c.ProjectWorkspaceID,
   ActorPrincipalID:&c.ActorPrincipalID,RequestID:c.RequestID,TraceID:c.TraceID,
   Payload:payload,OccurredAt:now})
 })
 if err!=nil{return ApprovedToolchainManifest{},err}
 return s.WorkspaceToolchainManifest(ctx,c.ProjectID,c.ProjectWorkspaceID)
}
