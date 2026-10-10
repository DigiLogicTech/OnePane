package agentworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// A durable toolchain wait is distinct from a model wait, an external-effect
// retry, and human approval. It never replaces a pinned Workspace application
// or launches the originally proposed Tool automatically after waking.
type toolchainWaitRecord struct{
 ProjectID string `json:"project_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 RuntimeID string `json:"runtime_id"`
 ApplicationID string `json:"application_id"`
 ManifestSHA256 string `json:"manifest_sha256"`
 RetryAtMS int64 `json:"retry_at_ms"`
}
type toolchainWaitEnvelope struct {
 ToolchainWait *toolchainWaitRecord `json:"toolchain_wait,omitempty"`
}
// Corrupt persisted wait data cannot silently become a dependency-free wait.
func hasToolchainWaitField(raw json.RawMessage)bool{
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return true} // Fail closed on malformed persisted JSON.
 _,ok:=fields["toolchain_wait"]
 return ok
}
func decodeToolchainWait(raw json.RawMessage)*toolchainWaitRecord{
 var env toolchainWaitEnvelope
 if json.Unmarshal(raw,&env)!=nil||env.ToolchainWait==nil{return nil}
 r:=env.ToolchainWait
 if r.ProjectID==""||r.ProjectWorkspaceID==""||r.RuntimeID==""||
  r.ApplicationID==""||len(r.ManifestSHA256)!=64||r.RetryAtMS<=0{return nil}
 return r
}

// A validated approval is a prerequisite, but a running-state claim alone
// does not prove the OCI container or installed executable is live. The
// rootless Tool adapter *independently* checks the actual container and
// preflights its binaries after the Task resumes.
func approvedToolchainWaitCandidate(ctx context.Context,db *sql.DB,t task.Task,raw json.RawMessage)(*toolchainWaitRecord,error){
 if t.ProjectID==nil||t.ProjectWorkspaceID==nil{return nil,nil}
 manifest,err:=projectworkspace.NewService(db,nil,clock.Real{}).
  WorkspaceToolchainManifest(ctx,*t.ProjectID,*t.ProjectWorkspaceID)
 if errors.Is(err,sql.ErrNoRows){return nil,nil}
 if err!=nil{return nil,fmt.Errorf("check Workspace toolchain approval: %w",err)}
 if manifest.Status!="approved_unverified"||!manifest.CurrentApplicationMatches{
  return nil,fmt.Errorf("Workspace toolchain approval changed; explicit human reapproval required")
 }
 var input struct{RuntimeID string `json:"runtime_id"`;ApplicationID string `json:"application_id"`}
 if json.Unmarshal(raw,&input)!=nil||input.RuntimeID==""||input.ApplicationID!=manifest.ApplicationID{
  return nil,fmt.Errorf("Workspace Tool input disagrees with approved application")
 }
 wait:=&toolchainWaitRecord{
  ProjectID:*t.ProjectID,ProjectWorkspaceID:*t.ProjectWorkspaceID,
  RuntimeID:input.RuntimeID,ApplicationID:manifest.ApplicationID,
  ManifestSHA256:manifest.ManifestSHA256,
 }
 running,err:=isApprovedToolchainRegisteredRunning(ctx,db,wait)
 if err!=nil{return nil,err}
 if running{return nil,nil}
 // The Task remains in its *current* Attempt and the worker checks only
 // registered local state periodically, without polling remote registries.
 return wait,nil
}
func isApprovedToolchainRegisteredRunning(ctx context.Context,db *sql.DB,w *toolchainWaitRecord)(bool,error){
 if w==nil||db==nil{return false,fmt.Errorf("missing Workspace toolchain wait identity")}
 manifest,err:=projectworkspace.NewService(db,nil,clock.Real{}).
  WorkspaceToolchainManifest(ctx,w.ProjectID,w.ProjectWorkspaceID)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 if manifest.Status!="approved_unverified"||!manifest.CurrentApplicationMatches||
  manifest.ManifestSHA256!=w.ManifestSHA256||manifest.ApplicationID!=w.ApplicationID{
  return false,nil // No silent approval switch or substitution.
 }
 var runtimeStatus,desiredState,applicationStatus string
 err=db.QueryRowContext(ctx,`SELECT r.status,r.desired_state,a.status
 FROM project_runtimes r JOIN project_applications a ON a.project_runtime_id=r.id
 JOIN projects p ON p.id=r.project_id
 JOIN project_workspaces pw ON pw.id=r.project_workspace_id AND pw.project_id=p.id
 WHERE p.id=? AND p.status='active' AND pw.id=? AND pw.status='active'
 AND r.id=? AND a.id=? AND a.source_kind='oci_image'
 AND a.source_ref=? AND a.revision=?`,
  w.ProjectID,w.ProjectWorkspaceID,w.RuntimeID,w.ApplicationID,
  manifest.ImageRef,manifest.ApplicationRevision).
  Scan(&runtimeStatus,&desiredState,&applicationStatus)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 return runtimeStatus=="running"&&desiredState=="running"&&applicationStatus=="running",nil
}

func (s *Service) waitForWorkspaceToolchain(ctx context.Context,run Run,res TickResult,wait *toolchainWaitRecord)TickResult{
 if wait==nil{return failedResult(res,fmt.Errorf("nil Workspace toolchain wait"))}
 // An existing Attempt and the pinned approved toolchain survive restart.
 // This continuation contains no executable command to replay automatically.
 wait.RetryAtMS=s.clock.UnixMilli()+15000
 state,err:=json.Marshal(toolchainWaitEnvelope{ToolchainWait:wait})
 if err!=nil{return failedResult(res,err)}
 err=s.suspendForResource(ctx,run,state,
  "waiting for the explicitly approved Workspace toolchain runtime",
  "toolchain",map[string]any{
   "kind":"approved_toolchain_resources","manifest_sha256":wait.ManifestSHA256,
   "retry_at_ms":wait.RetryAtMS,
  },nil)
 if err!=nil{return failedResult(res,err)}
 res.Status="waiting_toolchain"
 res.Error="approved Workspace toolchain runtime unavailable; awaiting resource reconciliation"
 return res
}
