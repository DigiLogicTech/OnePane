package agentworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// enforceSandboxToolOwnership is independent of the capability lease: a
// model-supplied resource_ref and input cannot choose the runtime, application
// or image owned by another Project Workspace. The Task's persisted identity,
// not its completion text, is the source of the execution boundary.
func enforceSandboxToolOwnership(ctx context.Context, db *sql.DB, t task.Task, toolID, resourceRef string, raw json.RawMessage) error {
 isApp := false
 requiresImage := false
 switch toolID {
 case sandboxrunner.ToolRuntimeEnsure, sandboxrunner.ToolRuntimeStop, sandboxrunner.ToolRuntimeInspect:
 case sandboxrunner.ToolAppPull, sandboxrunner.ToolAppEnsure, sandboxrunner.ToolImageInspect:
  isApp = true
  requiresImage = true
 case sandboxrunner.ToolAppStop, sandboxrunner.ToolAppInspect, sandboxrunner.ToolAppExec, sandboxrunner.ToolAppGitInspect:
  isApp = true
 default:
  return nil // Non-sandbox tools use their own resource and lease scopes.
 }
 if db == nil || t.ProjectID == nil || strings.TrimSpace(*t.ProjectID) == "" {
  return fmt.Errorf("sandbox execution requires a Project-owned Task")
 }
 var in struct {
  RuntimeID string `json:"runtime_id"`
  ApplicationID string `json:"application_id"`
  Image string `json:"image"`
 }
 if err:=json.Unmarshal(raw,&in);err!=nil {
  return fmt.Errorf("invalid sandbox tool input: %w",err)
 }
 if in.RuntimeID=="" || resourceRef!="project_runtime:"+in.RuntimeID {
  return fmt.Errorf("sandbox tool input and resource reference must identify the same runtime")
 }

 var projectID, tenancy, status string
 var workspaceID sql.NullString
 var workspaceStatus sql.NullString
 err:=db.QueryRowContext(ctx,`SELECT r.project_id,r.project_workspace_id,p.workspace_id,p.status,
  (SELECT pw.status FROM project_workspaces pw
    WHERE pw.id=r.project_workspace_id AND pw.project_id=r.project_id)
 FROM project_runtimes r
 JOIN projects p ON p.id=r.project_id
 WHERE r.id=?`,in.RuntimeID).Scan(&projectID,&workspaceID,&tenancy,&status,&workspaceStatus)
 if err!=nil {
  return fmt.Errorf("sandbox runtime ownership cannot be verified: %w",err)
 }
 if projectID!=*t.ProjectID || tenancy!=t.WorkspaceID || status!="active" {
  return fmt.Errorf("sandbox runtime is not owned by the Task's active Project")
 }

 if workspaceID.Valid {
  if t.ProjectWorkspaceID==nil || *t.ProjectWorkspaceID!=workspaceID.String ||
   !workspaceStatus.Valid || workspaceStatus.String!="active" {
   return fmt.Errorf("sandbox runtime does not belong to this active Project Workspace")
  }
 }else if t.ProjectWorkspaceID!=nil {
  return fmt.Errorf("Workspace-scoped Task cannot access legacy shared Project runtime")
 }
 route:=routingPolicyFromCompletion(t.Completion)
 if route.ProjectWorkspaceID!="" && (t.ProjectWorkspaceID==nil || route.ProjectWorkspaceID!=*t.ProjectWorkspaceID) {
  return fmt.Errorf("Task Workspace routing reference disagrees with its persisted scope")
 }
 if route.WorkspaceAccess.ProjectWorkspaceID!="" &&
  (t.ProjectWorkspaceID==nil || route.WorkspaceAccess.ProjectWorkspaceID!=*t.ProjectWorkspaceID) {
  return fmt.Errorf("Task Workspace access reference disagrees with its persisted scope")
 }
 if !isApp {return nil}
 if strings.TrimSpace(in.ApplicationID)=="" {
  return fmt.Errorf("sandbox application identity required")
 }
 var runtimeID, sourceRef, sourceKind string
 err=db.QueryRowContext(ctx,`SELECT project_runtime_id,source_ref,source_kind
  FROM project_applications WHERE id=?`,in.ApplicationID).
  Scan(&runtimeID,&sourceRef,&sourceKind)
 if err!=nil {
  return fmt.Errorf("sandbox application ownership cannot be verified: %w",err)
 }
 if runtimeID!=in.RuntimeID || sourceKind!="oci_image"{
  return fmt.Errorf("sandbox application is not an approved OCI application of this runtime")
 }
 if requiresImage && (in.Image=="" || in.Image!=sourceRef) {
  return fmt.Errorf("sandbox image does not match the registered OCI application")
 }
 return nil
}
