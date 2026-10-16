package goalobserveradapter

import (
 "bytes"
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/authority"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/tool"
)

const (
 PauseToolID="project.goal.tcp_observer.pause"
 ResumeToolID="project.goal.tcp_observer.resume"
 AdapterID="onepane.goal.tcp_observer.control"
 AdapterVersion="1"
 ToolVersion="1"
 CapabilityID="goal.tcp_observer.control"
)

type fenceWriter interface{
 SetGoalTCPObservationFence(context.Context,projectworkspace.SetGoalTCPObservationFenceCommand)(projectworkspace.GoalTCPObservationFence,error)
 GetGoalTCPObservationFence(context.Context,string,string)(projectworkspace.GoalTCPObservationFence,error)
}

// Adapter modifies only a local SQLite dispatch fence; no socket, subprocess,
// device configuration or Task is created. Both Tool Gateway and this adapter
// independently require a current, human-owned approved Operation.
type Adapter struct{
 db *sql.DB
 goal fenceWriter
}
func New(db *sql.DB,goal fenceWriter)*Adapter{return &Adapter{db:db,goal:goal}}
func(a *Adapter)ID()string{return AdapterID}
func(a *Adapter)Version()string{return AdapterVersion}
type input struct{
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ExpectedGoalRevision int64 `json:"expected_goal_revision"`
 ExpectedFenceRevision int64 `json:"expected_fence_revision"`
}

func ResourceRef(projectID,goalID string)string{
 return "goal-tcp-observer://"+projectID+"/"+goalID
}

func parseInput(raw []byte)(input,error){
 var v input
 // Effect admission may not guess missing revisions or accept duplicate
 // JSON keys (including differently-cased aliases). Only the four exact
 // canonical fields approved by the human Operation are admissible.
 if len(raw)==0||len(raw)>4096{
  return v,errors.New("observer effect input size invalid")
 }
 d:=json.NewDecoder(bytes.NewReader(raw))
 tok,err:=d.Token()
 if err!=nil||tok!=json.Delim('{'){
  return v,errors.New("observer effect input must be a JSON object")
 }
 seen:=make(map[string]bool,4)
 for d.More(){
  nameToken,e:=d.Token()
  if e!=nil{return v,e}
  name,ok:=nameToken.(string)
  if !ok||seen[name]{return v,errors.New("duplicate observer effect field")}
  switch name{
  case "project_id","goal_id","expected_goal_revision","expected_fence_revision":
  default:return v,errors.New("unknown observer effect field")
  }
  seen[name]=true
  var value json.RawMessage
  if e=d.Decode(&value);e!=nil{return v,e}
 }
 end,e:=d.Token()
 if e!=nil||end!=json.Delim('}'){
  return v,errors.New("invalid observer effect object ending")
 }
 var extra any
 if e=d.Decode(&extra);!errors.Is(e,io.EOF){
  return v,errors.New("one JSON object required")
 }
 if len(seen)!=4{
  return v,errors.New("observer effect revisions and scope must be explicit")
 }
 d=json.NewDecoder(bytes.NewReader(raw))
 d.DisallowUnknownFields()
 if err:=d.Decode(&v);err!=nil{return v,err}
 if strings.TrimSpace(v.ProjectID)!=v.ProjectID||
  strings.TrimSpace(v.GoalID)!=v.GoalID||
  v.ProjectID==""||v.GoalID==""||
  strings.ContainsAny(v.ProjectID,"/\\ \t\n")||
  strings.ContainsAny(v.GoalID,"/\\ \t\n")||
  len(v.ProjectID)>128||len(v.GoalID)>128||
  v.ExpectedGoalRevision<1||v.ExpectedFenceRevision<0{
  return v,errors.New("invalid exact Goal fence scope")
 }
 return v,nil
}

func Register(reg *tool.Registry,a *Adapter)error{
 if reg==nil||a==nil||a.db==nil||a.goal==nil{
  return errors.New("observer adapter dependencies missing")
 }
 for _,id:=range []string{PauseToolID,ResumeToolID}{
  if err:=reg.Register(tool.Definition{
   ID:id,Version:ToolVersion,CapabilityID:CapabilityID,
   Mode:authority.ActionMutate,AdapterID:AdapterID,AdapterVersion:AdapterVersion,
   Risk:policy.RiskMedium,MinimumVerification:policy.VerificationV2,
   MinimumApproval:policy.ApprovalApprover,
  },a);err!=nil{return err}
 }
 return nil
}

// The Tool Gateway hashes canonical JSON (sorted exact keys) at
// invocation creation. The final effect adapter must check those same
// bytes, rather than trusting a caller-provided AdapterRequest.Input for
// an otherwise valid executing Operation.
func canonicalFenceInputHash(v input)(string,error){
 canonical,err:=json.Marshal(map[string]any{
  "project_id":v.ProjectID,"goal_id":v.GoalID,
  "expected_goal_revision":v.ExpectedGoalRevision,
  "expected_fence_revision":v.ExpectedFenceRevision,
 })
 if err!=nil{return "",err}
 sum:=sha256.Sum256(canonical)
 return "sha256:"+hex.EncodeToString(sum[:]),nil
}

// Validate the actual persisted ToolInvocation, its EXECUTING Operation,
// active human principal, membership and canonical tenant Project binding.
// NEVER read an "actor" field out of untrusted adapter JSON. A registered
// adapter cannot mint a coordinator permit or approval itself.
func(a *Adapter)actor(ctx context.Context,r tool.AdapterRequest,v input)(string,error){
 if r.TaskID==nil||strings.TrimSpace(*r.TaskID)==""{
  return "",errors.New("observer mutation requires canonical Project Task")
 }
 requestedHash,err:=canonicalFenceInputHash(v)
 if err!=nil{return "",err}
 var actor string
 err=a.db.QueryRowContext(ctx,`SELECT inv.principal_id
  FROM tool_invocations inv
  JOIN operations op ON op.id=inv.operation_id
   AND op.workspace_id=inv.workspace_id
   AND op.principal_id=inv.principal_id
   AND op.resource_ref=inv.resource_ref
   AND op.tool_id=inv.tool_id AND op.tool_version=inv.tool_version
   AND op.state='executing'
  JOIN principals principal ON principal.id=inv.principal_id
   AND principal.status='active' AND principal.principal_type='human'
  JOIN projects p ON p.id=? AND p.workspace_id=inv.workspace_id AND p.status='active'
  JOIN workspace_memberships member ON member.workspace_id=p.workspace_id
   AND member.principal_id=principal.id AND member.status='active'
  JOIN project_continuous_goals goal ON goal.id=? AND goal.project_id=p.id
  JOIN project_workspaces pw ON pw.id=goal.project_workspace_id
   AND pw.project_id=p.id AND pw.status='active'
  JOIN tasks task ON task.id=inv.task_id
   AND task.workspace_id=inv.workspace_id
   AND task.project_id=p.id
   AND task.project_workspace_id=goal.project_workspace_id
  WHERE inv.id=? AND inv.status='running'
   AND inv.task_id=? AND op.task_id=inv.task_id
   AND inv.mode='mutate'
   AND op.adapter_id=inv.adapter_id AND op.adapter_version=inv.adapter_version
   AND inv.workspace_id=? AND inv.tool_id=? AND inv.tool_version=?
   AND inv.resource_ref=? AND inv.adapter_id=? AND inv.adapter_version=?
   AND inv.operation_id IS NOT NULL AND inv.input_hash=?`,
  v.ProjectID,v.GoalID,r.InvocationID,*r.TaskID,r.WorkspaceID,
  r.ToolID,r.ToolVersion,r.ResourceRef,AdapterID,AdapterVersion,requestedHash).Scan(&actor)
 if err!=nil{return "",fmt.Errorf("human-approved executing operation required: %w",err)}
 return actor,nil
}

func(a *Adapter)Invoke(ctx context.Context,r tool.AdapterRequest)(tool.AdapterResult,error){
 if a==nil||a.db==nil||a.goal==nil{return tool.AdapterResult{},errors.New("adapter unavailable")}
 if r.ToolVersion!=ToolVersion{return tool.AdapterResult{},errors.New("unknown observer action version")}
 status:=""
 switch r.ToolID{
 case PauseToolID:status="paused"
 case ResumeToolID:status="running"
 default:return tool.AdapterResult{},errors.New("unregistered observer action")
 }
 v,err:=parseInput(r.Input)
 if err!=nil{return tool.AdapterResult{},err}
 if r.ResourceRef!=ResourceRef(v.ProjectID,v.GoalID){
  return tool.AdapterResult{},errors.New("resource lease not scoped to exact Goal")
 }
 actor,err:=a.actor(ctx,r,v)
 if err!=nil{return tool.AdapterResult{},err}
 state,err:=a.goal.SetGoalTCPObservationFence(ctx,
  projectworkspace.SetGoalTCPObservationFenceCommand{
   ProjectID:v.ProjectID,GoalID:v.GoalID,
   ActorPrincipalID:actor,TargetStatus:status,
   AttachedTaskID:*r.TaskID,
   ExpectedGoalRevision:v.ExpectedGoalRevision,
   ExpectedFenceRevision:v.ExpectedFenceRevision,
  })
 if err!=nil{return tool.AdapterResult{},err}
 // A separate read must corroborate persisted local dispatch state;
 // this is NOT independent physical-device or TCP health verification.
 observed,err:=a.goal.GetGoalTCPObservationFence(ctx,v.ProjectID,v.GoalID)
 if err!=nil{return tool.AdapterResult{},fmt.Errorf("postcondition unavailable: %w",err)}
 if state.Status!=status||observed.Status!=status||
  state.Revision!=v.ExpectedFenceRevision+1||
  observed.Revision!=state.Revision||state.ChangedBy!=actor||
  observed.ChangedBy!=actor||observed.ObservationAuthorized{
  return tool.AdapterResult{},errors.New("postcondition uncertain: human reconciliation required")
 }
 result,_:=json.Marshal(map[string]any{
  "project_id":v.ProjectID,"goal_id":v.GoalID,
  "observer_dispatch_state":observed.Status,"fence_revision":observed.Revision,
  "execution_effect":"local_recurring_tcp_dispatch_only",
  "independent_device_health_verified":false,
  "independent_external_postcondition_verified":false,
  "network_permission_granted":false,
 })
 return tool.AdapterResult{
  Summary:"Operator-approved local observer dispatch fence persisted (device health unverified)",
  Result:result,
 },nil
}
