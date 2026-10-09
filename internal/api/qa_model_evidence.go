package api

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
)

// A strictly field-allowlisted, *read-only* view of observed local Agent Check
// state. Testbed turns contain full prompts and responses; this query must
// never read or serialise those columns, placement data or free-text errors.
const qaModelSessionCap=10

type qaModelSession struct {
 SessionRef string `json:"session_ref"`
 Status string `json:"status"`
 StartedAt int64 `json:"started_at_ms"`
 EndedAt *int64 `json:"ended_at_ms,omitempty"`
 RecordedTurns int64 `json:"recorded_successful_turns"`
 RecordedToolProbes int64 `json:"recorded_tool_probe_turns"`
 FailureDetails string `json:"failure_details"`
 LastFailureStage string `json:"last_failure_stage,omitempty"`
 LastFailureCategory string `json:"last_failure_category,omitempty"`
 FailureObservedAt *int64 `json:"failure_observed_at_ms,omitempty"`
}

type qaModelEvidence struct {
 SchemaVersion int `json:"schema_version"`
 Scope string `json:"scope"`
 DeploymentRef string `json:"deployment_ref"`
 DeploymentStatus string `json:"deployment_status"`
 ResidencyState string `json:"observed_residency_state"`
 DeploymentUpdatedAt int64 `json:"deployment_updated_at_ms"`
 MaxSessions int `json:"max_sessions"`
 CapturedSessions int `json:"captured_sessions"`
 SessionsTruncated bool `json:"sessions_truncated"`
 SourceStatus string `json:"source_status"`
 EvidenceLimitations []string `json:"evidence_limitations"`
 Sessions []qaModelSession `json:"sessions"`
}

func qaDeploymentStatus(s string)string {
 switch s {
 case "discovered","qualifying","ready","degraded","draining","unavailable","disabled":
  return s
 default:return "unavailable"
 }
}
func qaResidencyStatus(s string)string {
 switch s {
 case "stopped","loading","resident","busy","draining","failed":return s
 default:return "not_observed"
 }
}
func qaSessionStatus(s string)string {
 switch s {
 case "active","completed","cancelled":return s
 default:return "unavailable"
 }
}

func qaModelFailureStage(stage string)string{
 switch stage{
 case "runtime_acquire","deployment_read","model_read","inference_dispatch",
  "response_validation","turn_store","completion_validation",
  "completion_persist","runtime_release","runtime_unload","session_abort":return stage
 default:return "unavailable"
 }
}
func qaModelFailureCategory(category string)string{
 switch category{
 case "cancelled","deadline_exceeded","not_found","invalid_response",
  "storage_failure","abort_requested","execution_failure","unknown":return category
 default:return "unknown"
 }
}

// The HTTP caller authenticates and invokes authorizeManagedDeployment with
// model.read first. The SQL query then uses only the exact authorised model
// deployment key; no global model/session inventory is ever returned.
func loadQAModelEvidence(ctx context.Context,db *sql.DB,deploymentID string)(qaModelEvidence,error){
 if db==nil||deploymentID==""{return qaModelEvidence{},errors.New("QA model evidence source unavailable")}
 out:=qaModelEvidence{
  SchemaVersion:2,Scope:"authorised_managed_model_deployment",
  DeploymentRef:qaOpaqueRef("deployment",deploymentID),
  MaxSessions:qaModelSessionCap,SourceStatus:"observed_database_state",
  EvidenceLimitations:[]string{
   "only persisted successful testbed turns are counted",
   "sessions with typed failure observations show only a known stage and broad category",
   "cancelled sessions without typed failure observations have no known machine-coded cause",
   "no CPU/GPU usage, offload measurement or unload guarantee is inferred",
   "prompts, answers, settings, logs, failure text and credentials excluded",
   "no remote Node observations are included",
  },
  Sessions:make([]qaModelSession,0),
 }
 var status string
 var residency sql.NullString
 if err:=db.QueryRowContext(ctx,`SELECT status,residency_state,updated_at
 FROM model_deployments WHERE id=?`,deploymentID).
 Scan(&status,&residency,&out.DeploymentUpdatedAt);err!=nil{return qaModelEvidence{},err}
 out.DeploymentStatus=qaDeploymentStatus(status)
 out.ResidencyState=qaResidencyStatus(residency.String)
 rows,err:=db.QueryContext(ctx,`SELECT s.id,s.status,s.started_at,s.completed_at,
 COUNT(t.id),COALESCE(SUM(CASE WHEN t.synthetic_tool_probe=1 THEN 1 ELSE 0 END),0),
 f.stage,f.category,f.observed_at
 FROM model_testbed_sessions s
 LEFT JOIN model_testbed_turns t ON t.session_id=s.id
 LEFT JOIN model_agentcheck_failure_observations f ON f.id=(
  SELECT f2.id FROM model_agentcheck_failure_observations f2
  WHERE f2.session_id=s.id AND f2.deployment_id=s.deployment_id
  ORDER BY CASE WHEN f2.stage='session_abort' THEN 1 ELSE 0 END,
   f2.observed_at DESC,f2.id DESC LIMIT 1)
 WHERE s.deployment_id=?
 GROUP BY s.id,s.status,s.started_at,s.completed_at,f.stage,f.category,f.observed_at
 ORDER BY s.started_at DESC,s.id DESC LIMIT ?`,deploymentID,qaModelSessionCap+1)
 if err!=nil{return qaModelEvidence{},fmt.Errorf("load Agent Check QA evidence: %w",err)}
 defer rows.Close()
 for rows.Next(){
  if len(out.Sessions)>=qaModelSessionCap{out.SessionsTruncated=true;break}
  var id,sessionStatus string
  var row qaModelSession
  var finished,failedAt sql.NullInt64
  var stage,category sql.NullString
  if err:=rows.Scan(&id,&sessionStatus,&row.StartedAt,&finished,&row.RecordedTurns,&row.RecordedToolProbes,&stage,&category,&failedAt);err!=nil{
   return qaModelEvidence{},err
  }
  row.SessionRef=qaOpaqueRef("testbed",id)
  row.Status=qaSessionStatus(sessionStatus)
  if finished.Valid&&finished.Int64>0{n:=finished.Int64;row.EndedAt=&n}
  if row.RecordedTurns<0||row.RecordedToolProbes<0||row.RecordedToolProbes>row.RecordedTurns{
   return qaModelEvidence{},errors.New("invalid Agent Check turn counters")
  }
  // The session table does not store a structured failure cause. Never
  // publish operator notes or infer the cause from a cancelled session.
  row.FailureDetails="not_recorded_as_structured_evidence"
  if stage.Valid&&category.Valid&&failedAt.Valid&&failedAt.Int64>0{
   row.LastFailureStage=qaModelFailureStage(stage.String)
   row.LastFailureCategory=qaModelFailureCategory(category.String)
   at:=failedAt.Int64
   row.FailureObservedAt=&at
   row.FailureDetails="structured_failure_category_only"
  }
  out.Sessions=append(out.Sessions,row)
 }
 if err:=rows.Err();err!=nil{return qaModelEvidence{},err}
 out.CapturedSessions=len(out.Sessions)
 return out,nil
}
