package workspacebroker

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/authority"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/tool"
)

const (
 ToolID="project.workspace.service.health"
 ToolVersion="1"
 AdapterID="workspace.service.health-broker"
 AdapterVersion="1"
 CapabilityID="project.workspace.service.health"
 ResourcePrefix="project_workspace_service_link:"
)

// ProbeRunner accepts only the trusted, database-resolved Project and target
// Workspace plus the explicit human-approved link. No model-selected URL.
type ProbeRunner interface {
 Probe(context.Context,Request)(Receipt,error)
}

// TaskAdapter is deliberately unregistered and disabled in normal bootstrap.
// Calling RegisterTaskTool in a trusted Gateway is an explicit integration
// step AFTER physical rootless acceptance and full Task Gateway review.
type TaskAdapter struct{
 db *sql.DB
 broker ProbeRunner
 enabled bool
}
func NewTaskAdapter(db *sql.DB,broker ProbeRunner,enableAfterPhysicalAcceptance bool)*TaskAdapter{
 return &TaskAdapter{db:db,broker:broker,enabled:enableAfterPhysicalAcceptance}
}
func (*TaskAdapter) ID()string{return AdapterID}
func (*TaskAdapter) Version()string{return AdapterVersion}
func RegisterTaskTool(reg *tool.Registry,adapter *TaskAdapter)error{
 if reg==nil||adapter==nil{return errors.New("Workspace service Task adapter unavailable")}
 return reg.Register(tool.Definition{
  ID:ToolID,Version:ToolVersion,CapabilityID:CapabilityID,
  Mode:authority.ActionRead,
  AdapterID:AdapterID,AdapterVersion:AdapterVersion,
  Risk:policy.RiskMedium,
  MinimumVerification:policy.VerificationV1,
  MinimumApproval:policy.ApprovalNone,
 },adapter)
}

// Reject unknown input fields, forged project/target IDs, URLs and controls.
func parseLinkID(raw json.RawMessage)(string,error){
 var input map[string]json.RawMessage
 if len(raw)==0||json.Unmarshal(raw,&input)!=nil||len(input)!=1{
  return "",ErrDenied
 }
 var id string
 if json.Unmarshal(input["link_id"],&id)!=nil||
  strings.TrimSpace(id)!=id||len(id)<1||len(id)>128{
  return "",ErrDenied
 }
 for _,r:=range id{
  if !((r>='A'&&r<='Z')||(r>='a'&&r<='z')||
   (r>='0'&&r<='9')||r=='-'||r=='_'){return "",ErrDenied}
 }
 return id,nil
}
type taskAdmission struct{
 projectID string
 targetWorkspaceID string
}

// A consumed Tool Gateway capability lease creates the authorized invocation.
// The adapter independently requires THAT EXACT invocation to be running,
// with the correct canonical input hash, resource, Task, Attempt and Agent
// Worker incarnation. Its Project Workspace must be the grant TARGET.
// This check is repeated after the network read before returning any receipt.
func (a *TaskAdapter) admission(ctx context.Context,req tool.AdapterRequest,linkID string)(taskAdmission,error){
 if a==nil||a.db==nil||req.InvocationID==""||req.TaskID==nil||req.AttemptID==nil||
  *req.TaskID==""||*req.AttemptID==""||req.WorkspaceID==""||
  req.ToolID!=ToolID||req.ToolVersion!=ToolVersion||
  req.ResourceRef!=ResourcePrefix+linkID{return taskAdmission{},ErrDenied}
 digest:=sha256.Sum256(req.Input)
 hash:="sha256:"+hex.EncodeToString(digest[:])
 var info taskAdmission
 var count int
 err:=a.db.QueryRowContext(ctx,`SELECT COUNT(*),
  COALESCE(MAX(t.project_id),''),COALESCE(MAX(t.project_workspace_id),'')
 FROM tool_invocations i
 JOIN tasks t ON t.id=i.task_id AND t.workspace_id=i.workspace_id
 JOIN task_attempts att ON att.id=i.attempt_id AND att.task_id=t.id
   AND att.status='running' AND att.worker_principal_id=i.principal_id
 JOIN agent_worker_runs wr ON wr.task_id=t.id AND wr.attempt_id=att.id
   AND wr.workspace_id=t.workspace_id AND wr.worker_principal_id=i.principal_id
   AND wr.status='running'
 JOIN principals principal ON principal.id=i.principal_id AND principal.status='active'
 JOIN workspaces tenant ON tenant.id=t.workspace_id AND tenant.status='active'
 JOIN workspace_memberships member ON member.workspace_id=t.workspace_id
   AND member.principal_id=i.principal_id AND member.status='active'
 JOIN projects p ON p.id=t.project_id AND p.workspace_id=t.workspace_id
   AND p.status='active'
 JOIN project_workspaces target ON target.id=t.project_workspace_id
   AND target.project_id=p.id AND target.status='active'
 JOIN project_workspace_service_links link ON link.id=? AND link.project_id=p.id
   AND link.target_workspace_id=target.id AND link.enabled=1
   AND link.expires_at_ms>?
 WHERE i.id=? AND i.workspace_id=? AND i.task_id=? AND i.attempt_id=?
  AND i.status='running' AND i.tool_id=? AND i.tool_version=?
  AND i.adapter_id=? AND i.adapter_version=? AND i.resource_ref=?
  AND i.input_hash=? AND t.state='running'
  AND t.cancel_requested_at IS NULL AND t.archived_at IS NULL`,
 linkID,time.Now().UnixMilli(),req.InvocationID,req.WorkspaceID,
 *req.TaskID,*req.AttemptID,ToolID,ToolVersion,
 AdapterID,AdapterVersion,req.ResourceRef,hash).Scan(
 &count,&info.projectID,&info.targetWorkspaceID)
 if err!=nil||count!=1||info.projectID==""||info.targetWorkspaceID==""{
  return taskAdmission{},ErrDenied
 }
 return info,nil
}

func (a *TaskAdapter) Invoke(ctx context.Context,req tool.AdapterRequest)(tool.AdapterResult,error){
 if a==nil||!a.enabled||a.broker==nil{return tool.AdapterResult{},tool.KnownFailure(ErrDenied)}
 linkID,err:=parseLinkID(req.Input)
 if err!=nil{return tool.AdapterResult{},tool.KnownFailure(ErrDenied)}
 before,err:=a.admission(ctx,req,linkID)
 if err!=nil{return tool.AdapterResult{},tool.KnownFailure(ErrDenied)}
 result,err:=a.broker.Probe(ctx,Request{
  ProjectID:before.projectID,TargetWorkspaceID:before.targetWorkspaceID,
  LinkID:linkID,
 })
 if err!=nil{return tool.AdapterResult{},tool.KnownFailure(ErrUnavailable)}
 after,err:=a.admission(ctx,req,linkID)
 if err!=nil||after!=before{return tool.AdapterResult{},tool.KnownFailure(ErrDenied)}
 encoded,err:=json.Marshal(result)
 if err!=nil{return tool.AdapterResult{},tool.KnownFailure(ErrInvalidResponse)}
 return tool.AdapterResult{Summary:"Workspace service readiness checked",Result:encoded},nil
}
