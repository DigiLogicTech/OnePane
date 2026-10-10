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
 NodePlacement string `json:"node_placement,omitempty"`
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
 var assignedNode sql.NullString
 err=db.QueryRowContext(ctx,`SELECT r.id,r.status,r.desired_state,a.status,r.node_id
 FROM project_applications a JOIN project_runtimes r ON r.id=a.project_runtime_id
 JOIN projects p ON p.id=r.project_id
 JOIN project_workspaces pw ON pw.id=r.project_workspace_id AND pw.project_id=p.id
 WHERE p.id=? AND pw.id=? AND p.status='active' AND pw.status='active'
 AND a.id=? AND a.revision=? AND a.source_ref=? LIMIT 1`,
  projectID,workspaceID,approved.ApplicationID,approved.ApplicationRevision,approved.ImageRef).
  Scan(&runtimeID,&runtimeStatus,&desired,&appStatus,&assignedNode)
 if errors.Is(err,sql.ErrNoRows){
  out.Status="waiting_approval"
  out.NextAction="Registered OCI application no longer matches this human-approved revision."
  return out,nil
 }
 if err!=nil{return out,err}
 out.RuntimeID=runtimeID
 out.RuntimeStatus=runtimeStatus
 out.ApplicationStatus=appStatus
 // A running-state claim on another Node is not proof this origin can
 // perform sandbox Tool preflight. No cross-node OCI executor is wired yet.
 if assignedNode.Valid{
  var local int
  var trust string
  nodeErr:=db.QueryRowContext(ctx,`SELECT local,trust_state
   FROM harness_nodes WHERE id=?`,assignedNode.String).Scan(&local,&trust)
  if errors.Is(nodeErr,sql.ErrNoRows){
   out.NodePlacement="unregistered_node"
   out.Status="waiting_node"
   out.NextAction="The approved Workspace runtime references an unregistered Node; inspect the assignment before planning any build."
   return out,nil
  }
  if nodeErr!=nil{return out,nodeErr}
  if local!=1||trust!="local"{
   out.NodePlacement="remote_unsupported"
   out.Status="unsupported_node_placement"
   out.NextAction="This Workspace targets a different or untrusted Node; remote sandbox execution is not yet supported. Do not run a local Tool against another Node's Workspace."
   return out,nil
  }
  out.NodePlacement="origin_node"
 }else{
  // Legacy unassigned runtimes execute only through the origin-local
  // sandbox adapter; this is not an externally verified Node health claim.
  out.NodePlacement="legacy_local"
 }
 if desired!="running"||runtimeStatus!="running"||appStatus!="running"{
  out.Status="waiting_resources"
  out.NextAction="Request governed runtime/application startup on the approved Node; do not claim that the toolchain is installed or substitute images."
  return out,nil
 }
 out.Status="ready_for_preflight"
 out.NextAction="Before any build use the Task-owned project.app.toolchain.preflight; actual installed versions and runtime resource health remain unverified."
 return out,nil
}
