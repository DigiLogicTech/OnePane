package projectorchestrator

import (
 "context"
 "database/sql"
 "errors"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// workspaceToolchainReadiness is a bounded, canonical Project Workspace
// planning observation. Unlike "ready", "ready_for_preflight" means only
// the persisted runtime/application claim is running; actual executables,
// versions, image integrity and physical Node health remain unverified.
type workspaceToolchainReadiness struct{
 Status string `json:"status"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 ApplicationID string `json:"application_id,omitempty"`
 RuntimeID string `json:"runtime_id,omitempty"`
 ManifestSHA256 string `json:"manifest_sha256,omitempty"`
 ManifestRevision int64 `json:"manifest_revision,omitempty"`
 Requirements []projectworkspace.ToolchainRequirement `json:"requirements,omitempty"`
 RuntimeStatus string `json:"runtime_status,omitempty"`
 ApplicationStatus string `json:"application_status,omitempty"`
 NextAction string `json:"next_action"`
}

func inspectWorkspaceToolchain(ctx context.Context,db *sql.DB,projectID,workspaceID string)(workspaceToolchainReadiness,error){
 out:=workspaceToolchainReadiness{Status:"not_approved",ProjectWorkspaceID:workspaceID,
  NextAction:"Planning may continue; human must approve an immutable Workspace image/toolchain before claiming controlled build readiness."}
 if db==nil||projectID==""||workspaceID==""{return out,fmt.Errorf("Workspace toolchain scope unavailable")}
 svc:=projectworkspace.NewService(db,nil,clock.Real{})
 approved,err:=svc.WorkspaceToolchainManifest(ctx,projectID,workspaceID)
 if errors.Is(err,sql.ErrNoRows){return out,nil}
 if err!=nil{return out,err}
 out.ApplicationID=approved.ApplicationID
 out.ManifestSHA256=approved.ManifestSHA256
 out.ManifestRevision=approved.Revision
 out.Requirements=approved.Requirements
 if !approved.CurrentApplicationMatches{
  out.Status="waiting_approval"
  out.NextAction="The pinned OCI application changed; operator must review and approve a new Workspace toolchain revision."
  return out,nil
 }
 var runtimeID,runtimeStatus,desired,appStatus string
 err=db.QueryRowContext(ctx,`SELECT r.id,r.status,r.desired_state,a.status
 FROM project_applications a JOIN project_runtimes r ON r.id=a.project_runtime_id
 JOIN projects p ON p.id=r.project_id
 JOIN project_workspaces pw ON pw.id=r.project_workspace_id AND pw.project_id=p.id
 WHERE p.id=? AND pw.id=? AND p.status='active' AND pw.status='active'
 AND a.id=? AND a.revision=? AND a.source_ref=? LIMIT 1`,
  projectID,workspaceID,approved.ApplicationID,approved.ApplicationRevision,approved.ImageRef).
  Scan(&runtimeID,&runtimeStatus,&desired,&appStatus)
 if errors.Is(err,sql.ErrNoRows){
  out.Status="waiting_approval"
  out.NextAction="Registered OCI application no longer matches this human-approved revision."
  return out,nil
 }
 if err!=nil{return out,err}
 out.RuntimeID=runtimeID
 out.RuntimeStatus=runtimeStatus
 out.ApplicationStatus=appStatus
 if desired!="running"||runtimeStatus!="running"||appStatus!="running"{
  out.Status="waiting_resources"
  out.NextAction="Request governed runtime/application startup on the approved Node; do not claim that the toolchain is installed or substitute images."
  return out,nil
 }
 out.Status="ready_for_preflight"
 out.NextAction="Before any build use the Task-owned project.app.toolchain.preflight; actual installed versions and runtime resource health remain unverified."
 return out,nil
}
