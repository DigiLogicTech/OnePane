package agentworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// workspaceExecutionManifest returns only resources owned by the Task's
// persisted Project + named Workspace, never a tenant-wide tool inventory.
// Its contents help the reasoning model identify the right OCI tool IDs;
// they are observations, never an authority grant or proof of a live sandbox.
func workspaceExecutionManifest(ctx context.Context, db *sql.DB, t task.Task) (json.RawMessage, error) {
 if t.ProjectID==nil||t.ProjectWorkspaceID==nil{return nil,nil}
 if db==nil {return nil,errors.New("Workspace execution inventory database unavailable")}
 var runtimeID,observed,desired string
 err:=db.QueryRowContext(ctx,`SELECT r.id,r.status,r.desired_state
 FROM project_runtimes r
 JOIN projects p ON p.id=r.project_id
 JOIN project_workspaces pw ON pw.id=r.project_workspace_id AND pw.project_id=p.id
 WHERE p.id=? AND p.workspace_id=? AND p.status='active'
 AND pw.id=? AND pw.status='active'
 LIMIT 1`,*t.ProjectID,t.WorkspaceID,*t.ProjectWorkspaceID).
 Scan(&runtimeID,&observed,&desired)
 if errors.Is(err,sql.ErrNoRows) {
  return json.Marshal(map[string]any{
   "project_workspace_id":*t.ProjectWorkspaceID,
   "runtime_state":"not_provisioned",
   "applications":[]any{},
   "note":"No active registered Workspace runtime. Do not invent runtime or application IDs; request governed provisioning.",
  })
 }
 if err!=nil{return nil,fmt.Errorf("scoped Workspace runtime inventory: %w",err)}
 type app struct {
  ID string `json:"application_id"`
  ObservedStatus string `json:"observed_status"`
  DesiredState string `json:"desired_state"`
  Image string `json:"oci_image"`
 }
 apps:=make([]app,0)
 rows,err:=db.QueryContext(ctx,`SELECT a.id,a.status,a.desired_state,a.source_ref
 FROM project_applications a
 WHERE a.project_runtime_id=? AND a.source_kind='oci_image'
 ORDER BY a.id LIMIT 20`,runtimeID)
 if err!=nil{return nil,fmt.Errorf("scoped Workspace OCI application inventory: %w",err)}
 for rows.Next(){
  var a app
  if err=rows.Scan(&a.ID,&a.ObservedStatus,&a.DesiredState,&a.Image);err!=nil{break}
  apps=append(apps,a)
 }
 if rowsErr:=rows.Err();err==nil {err=rowsErr}
 if closeErr:=rows.Close();err==nil {err=closeErr}
 if err!=nil{return nil,fmt.Errorf("scan Workspace OCI tool inventory: %w",err)}
 return json.Marshal(map[string]any{
  "project_workspace_id":*t.ProjectWorkspaceID,
  "runtime_id":runtimeID,
  "resource_ref":"project_runtime:"+runtimeID,
  "runtime_observed_status":observed,
  "runtime_desired_state":desired,
  "applications":apps,
  "file_inspect_tool":map[string]any{
   "tool_id":"project.app.files.inspect",
   "tool_version":"1",
   "capability_id":"project.app.execute",
   "actions":[]string{"list","preview_text"},
   "input_schema":map[string]any{
    "runtime_id":"registered runtime_id above",
    "application_id":"one of the application_id values above",
    "action":"list or preview_text",
    "path":"Workspace-relative regular file path, required only for preview_text",
    "timeout_seconds":"optional integer 1..60; default 15",
   },
   "note":"List limited to depth 4; preview_text limited to 64KiB, not a lossless edit source; .git internals, symlinks and host paths denied",
  },
  "git_inspect_tool":map[string]any{
   "tool_id":"project.app.git.inspect",
   "tool_version":"1",
   "capability_id":"project.app.execute",
   "actions":[]string{"status","diff","log","tracked_files"},
   "input_schema":map[string]any{
    "runtime_id":"registered runtime_id above",
    "application_id":"one of the application_id values above",
    "action":"one of the four fixed read-only Git inspections",
    "timeout_seconds":"optional integer 1..120; default 30",
   },
  },
  "command_tool":map[string]any{
   "tool_id":"project.app.exec",
   "tool_version":"1",
   "capability_id":"project.app.execute",
   "input_schema":map[string]any{
    "runtime_id":"registered runtime_id above",
    "application_id":"one of the application_id values above",
    "command":[]string{"executable","arg1"},
    "timeout_seconds":"optional integer 1..7200; default 900",
   },
  },
  "note":"Read-only resource inventory, not authority. Only use registered OCI applications; executions require a capability lease, an independently verified running rootless sandbox, and a Workspace-only bind. Never invent IDs, access host files, or treat a requested state as verified.",
 })
}
