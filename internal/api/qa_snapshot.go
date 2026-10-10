package api

import (
 "archive/zip"
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "io"
 "regexp"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/buildinfo"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// This is the first, intentionally narrow RC11 QA bundle format. It uses
// immutable, typed, allowlisted state instead of serialising arbitrary logs,
// event payloads, prompts, tool results or model continuation JSON.
const (
 qaSnapshotVersion=2
 qaSnapshotTaskCap=50
 qaSnapshotArchiveCap=128<<10
)
var qaSafeID=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,95}$`)

func qaIdentifier(s string)string{
 if !qaSafeID.MatchString(s){return "unavailable"}
 return s
}
func qaTaskState(s task.State)string{
 if !task.ValidState(s){return "unavailable"}
 return string(s)
}
func qaRunStatus(s string)string{
 switch s {
 case "created","running","waiting","succeeded","failed","blocked","interrupted","cancelled":
  return s
 default:return "unavailable"
 }
}
func qaStepStatus(s string)string{
 switch s {
 case "started","waiting","succeeded","failed","blocked","interrupted","unknown","skipped":
  return s
 default:return "unavailable"
 }
}
func qaStepKind(s string)string{
 // The DB column is not a diagnostic permission boundary. Export known
 // high-level worker categories only, never model-defined text.
 switch s {
 case "route","model","agent_runtime","tool","operation","delegate","replan","complete",
      "human","escalate","wait","fail","verification":
  return s
 default:return "unavailable"
 }
}
type qaExecution struct {
 Status string `json:"status"`
 RunID string `json:"run_id"`
 StepsUsed int64 `json:"steps_used"`
 MaxSteps int64 `json:"max_steps"`
 LastStepKind string `json:"last_step_kind"`
 LastStepStatus string `json:"last_step_status"`
 ReviewRequired bool `json:"review_required"`
 RunRef string `json:"run_ref"`
}
type qaTaskEntry struct {
 ID string `json:"id"`
 TaskRef string `json:"task_ref"`
 State string `json:"state"`
 Revision int64 `json:"revision"`
 UpdatedAt int64 `json:"updated_at_ms"`
 Execution *qaExecution `json:"execution,omitempty"`
 Dependencies *taskDependencyEvidence `json:"hard_dependencies,omitempty"`
}
type qaSnapshot struct {
 SchemaVersion int `json:"schema_version"`
 Scope string `json:"scope"`
 GeneratedUTC string `json:"generated_utc"`
 BuildVersion string `json:"build_version"`
 BuildRevision string `json:"build_revision"`
 MaxTasks int `json:"max_tasks"`
 CapturedTasks int `json:"captured_tasks"`
 Truncated bool `json:"truncated"`
 SourceStatus string `json:"source_status"`
 ExcludedCategories []string `json:"excluded_categories"`
 Tasks []qaTaskEntry `json:"tasks"`
 MaxTimelineEvents int `json:"max_timeline_events"`
 CapturedTimelineEvents int `json:"captured_timeline_events"`
 TimelineTruncated bool `json:"timeline_truncated"`
 Timeline []qaTimelineEvent `json:"timeline"`
 ExecutionSources []qaExecutionSource `json:"execution_sources"`
 ExecutionSourcesTruncated bool `json:"execution_sources_truncated"`
}
var qaExcluded=[]string{
 "credentials, cookies, OAuth and Vault material",
 "raw logs or audit event payloads",
 "prompts, conversations, model outputs and continuations",
 "tool inputs/outputs and private Project/Workspace files",
 "environment variables, device paths and model weights",
 "external Node data, screenshots and attachments",
}

// The caller MUST have already proved tenant + Project + canonical Workspace
// membership, and loaded the Tasks through its scoped SQL repository.
func makeQASnapshot(now time.Time,rows []task.Task,progress map[string]taskExecutionProgress,
 dependencies map[string]taskDependencyEvidence)qaSnapshot{
 snap:=qaSnapshot{
  SchemaVersion:qaSnapshotVersion,Scope:"canonical_project_workspace",
  GeneratedUTC:now.UTC().Format(time.RFC3339Nano),
  BuildVersion:qaIdentifier(buildinfo.Version),BuildRevision:qaIdentifier(buildinfo.Revision),
  MaxTasks:qaSnapshotTaskCap,SourceStatus:"read_only_observed_state",
  MaxTimelineEvents:qaTimelineEventCap,Timeline:make([]qaTimelineEvent,0),
  ExecutionSources:make([]qaExecutionSource,0),
  ExcludedCategories:append([]string(nil),qaExcluded...),Tasks:make([]qaTaskEntry,0),
 }
 if len(rows)>qaSnapshotTaskCap{snap.Truncated=true;rows=rows[:qaSnapshotTaskCap]}
 for _,t:=range rows {
  // No objective, completion JSON, result, reasons, user names or text from
  // uncontrolled DB columns is ever copied into a debug export.
  item:=qaTaskEntry{ID:qaIdentifier(t.ID),TaskRef:qaOpaqueRef("task",t.ID),State:qaTaskState(t.State),Revision:t.Revision,UpdatedAt:t.UpdatedAt}
  if p,ok:=progress[t.ID];ok&&p.MaxSteps>0&&p.StepsUsed>=0&&p.StepsUsed<=p.MaxSteps{
   item.Execution=&qaExecution{
    Status:qaRunStatus(p.Status),RunID:qaIdentifier(p.RunID),RunRef:qaOpaqueRef("run",p.RunID),
    StepsUsed:p.StepsUsed,MaxSteps:p.MaxSteps,
    LastStepKind:qaStepKind(p.LastStepKind),
    LastStepStatus:qaStepStatus(p.LastStepStatus),
    ReviewRequired:p.ReviewRequired||p.Status=="interrupted"||p.LastStepStatus=="unknown",
   }
  }
  if d,ok:=dependencies[t.ID];ok&&d.Total>=0&&d.Completed>=0&&d.Failed>=0&&
   d.Blocked>=0&&d.Restricted>=0&&d.Remaining>=0&&
   d.Completed+d.Failed+d.Blocked+d.Restricted<=d.Total&&d.Remaining==d.Total-d.Completed{
   copied:=d
   item.Dependencies=&copied
  }
  snap.Tasks=append(snap.Tasks,item)
 }
 snap.CapturedTasks=len(snap.Tasks)
 return snap
}

type qaBundleManifest struct {
 SchemaVersion int `json:"schema_version"`
 CreatedUTC string `json:"created_utc"`
 Origin string `json:"origin"`
 Contents map[string]string `json:"sha256"`
 ExcludedCategories []string `json:"excluded_categories"`
}
// qaBundle emits no temp files, no caller supplied paths, and only fixed
// archive entry names. Bytes are built entirely from allowlisted typed state.
func qaBundle(snapshot qaSnapshot)([]byte,error){
 snapshotJSON,err:=json.MarshalIndent(snapshot,"","  ")
 if err!=nil{return nil,err}
 if len(snapshotJSON)>64<<10{return nil,fmt.Errorf("QA snapshot exceeds safe bound")}
 const readme="OnePane RC11 QA snapshot (schema v2). This is a read-only, sanitised Task/Worker status and event chronology extract, not the full Debug Centre. Opaque refs correlate records without copying event payloads/trace IDs. Physical Node/tool/installer evidence, raw logs, secrets, code, prompts and Workspace files are excluded. Review contents locally before sharing.\n"
 files:=map[string][]byte{"snapshot.json":snapshotJSON,"README.txt":[]byte(readme)}
 hashes:=map[string]string{}
 for name,data:=range files{h:=sha256.Sum256(data);hashes[name]=hex.EncodeToString(h[:])}
 manifest:=qaBundleManifest{SchemaVersion:qaSnapshotVersion,CreatedUTC:snapshot.GeneratedUTC,
  Origin:"onepane_local_explicit_export",Contents:hashes,ExcludedCategories:append([]string(nil),qaExcluded...)}
 m,err:=json.MarshalIndent(manifest,"","  ")
 if err!=nil{return nil,err}
 var buf bytes.Buffer
 zw:=zip.NewWriter(&buf)
 for _,name:=range []string{"manifest.json","snapshot.json","README.txt"}{
  var data []byte
  if name=="manifest.json"{data=m}else{data=files[name]}
  // Enforce known names and a tight uncompressed archive budget.
  if strings.ContainsAny(name,"/\\")||len(data)>64<<10{return nil,fmt.Errorf("unsafe QA archive entry")}
  w,e:=zw.Create(name)
  if e!=nil{return nil,e}
  if _,e=io.Copy(w,bytes.NewReader(data));e!=nil{return nil,e}
 }
 if err=zw.Close();err!=nil{return nil,err}
 if buf.Len()>qaSnapshotArchiveCap{return nil,fmt.Errorf("QA bundle exceeds safe size")}
 return buf.Bytes(),nil
}
