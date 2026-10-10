package api

import (
 "context"
 "database/sql"
 "fmt"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// The asynchronous bridge is the durable database Task -> Attempt -> Worker
// relationship. It never pretends that a trace ID propagated to a later
// asynchronous Worker process. A trusted HTTP-created Task joins this lineage
// only by the authenticated Workspace Task pseudonym.
type qaAsyncLineage struct{
 TaskRef string `json:"task_ref"`
 AttemptRef string `json:"attempt_ref"`
 WorkerRunRef string `json:"worker_run_ref"`
 AttemptState string `json:"attempt_state"`
 WorkerState string `json:"worker_state"`
 StepsRecorded int64 `json:"steps_recorded"`
 LinkKind string `json:"link_kind"`
}
type qaProbeWitness struct{
 TaskRef string `json:"task_ref"`
 VerificationRef string `json:"verification_ref"`
 ObservationRef string `json:"observation_ref"`
 Role string `json:"role"`
 SourceTrust string `json:"source_trust"`
 ObservedAfterAttempt bool `json:"observed_after_attempt"`
 ObservedAfterOperationStart bool `json:"observed_after_operation_start"`
 IntegrityRechecked bool `json:"integrity_rechecked"`
 IndependentSource bool `json:"independent_source"`
 RecordedAssurancePass bool `json:"recorded_assurance_pass"`
 CorroboratingIntegrationProbe bool `json:"corroborating_integration_probe"`
 EvidenceClass string `json:"evidence_class"`
}

func qaAttemptState(s string)string{
 switch s{
 case "created","queued","running","waiting","succeeded","failed","cancelled","interrupted":return s
 default:return "unavailable"
 }
}
func loadQAAsyncLineage(ctx context.Context,db *sql.DB,tenant,projectID,workspaceID string,visible []task.Task)([]qaAsyncLineage,bool,error){
 out:=make([]qaAsyncLineage,0)
 if db==nil{return out,false,nil}
 prefix,args,ok:=qaDeepScope(tenant,projectID,workspaceID,visible)
 if !ok{return out,false,nil}
 query:=prefix+`SELECT r.task_id,a.id,r.id,a.status,r.status,r.step_count
 FROM agent_worker_runs r
 JOIN scoped t ON t.id=r.task_id
 JOIN task_attempts a ON a.id=r.attempt_id AND a.task_id=r.task_id
 WHERE r.workspace_id=?
 ORDER BY r.started_at DESC,r.id DESC LIMIT ?`
 args=append(args,tenant,qaDeepEvidenceCap+1)
 bounded,cancel:=context.WithTimeout(ctx,2*time.Second);defer cancel()
 rows,err:=db.QueryContext(bounded,query,args...)
 if err!=nil{return nil,false,fmt.Errorf("scoped Worker lineage unavailable: %w",err)}
 defer rows.Close()
 for rows.Next(){
  var taskID,attemptID,runID,attemptState,runState string
  var steps int64
  if err:=rows.Scan(&taskID,&attemptID,&runID,&attemptState,&runState,&steps);err!=nil{return nil,false,err}
  if len(out)>=qaDeepEvidenceCap{return out,true,nil}
  if steps<0{return nil,false,fmt.Errorf("invalid Worker lineage step counter")}
  if steps>256{steps=256}
  out=append(out,qaAsyncLineage{
   TaskRef:qaOpaqueRef("task",taskID),AttemptRef:qaOpaqueRef("attempt",attemptID),
   WorkerRunRef:qaOpaqueRef("run",runID),
   AttemptState:qaAttemptState(attemptState),WorkerState:qaRunStatus(runState),
   StepsRecorded:steps,LinkKind:"persisted_task_attempt_worker_foreign_keys",
  })
 }
 if err:=rows.Err();err!=nil{return nil,false,err}
 return out,false,nil
}

func qaWitnessTrust(s string)string{
 switch s{case "trusted_control","trusted_procedure","authoritative_data","user_instruction",
  "untrusted_content","unverified_derived","verified_derived":return s}
 return "unavailable"
}

// Read the observation IDs that the *persisted assurance evaluation* itself
// selected in its result JSON; independently re-check observation integrity
// from source rows. This does not run an external probe, or verify the actual
// external-world effect. Rows without a matching Worker run cannot claim an
// independent source. No observation JSON, probe ID, principal or hash leaks.
func loadQAProbeWitnesses(ctx context.Context,db *sql.DB,tenant,projectID,workspaceID string,visible []task.Task)([]qaProbeWitness,bool,error){
 out:=make([]qaProbeWitness,0)
 if db==nil{return out,false,nil}
 prefix,args,ok:=qaDeepScope(tenant,projectID,workspaceID,visible)
 if !ok{return out,false,nil}
 query:=prefix+`SELECT v.task_id,v.id,ob.id,
  json_extract(e.value,'$.role') role,ob.trust,ob.observed_at,
  ob.source_principal_id,r.worker_principal_id,
  v.status,a.status,v.required_level,v.achieved_level,a.achieved_level,a.evidence_hash,
  COALESCE((SELECT MAX(ta.started_at) FROM task_attempts ta WHERE ta.task_id=v.task_id),0),
  COALESCE((SELECT MAX(ev.occurred_at) FROM events ev
   WHERE ev.aggregate_type='operation' AND ev.aggregate_id=v.operation_id
   AND ev.event_type='operation.executing' AND ev.workspace_id=v.workspace_id),0)
 FROM verifications v JOIN scoped t ON t.id=v.task_id
 JOIN assurance_runs a ON a.verification_id=v.id AND a.workspace_id=v.workspace_id
  AND a.task_id=v.task_id
  AND (v.operation_id IS NULL AND a.operation_id IS NULL OR v.operation_id=a.operation_id)
 JOIN json_each(CASE WHEN json_valid(a.result_json) THEN a.result_json ELSE '{}' END,'$.observations') e
 JOIN observations ob ON ob.id=json_extract(e.value,'$.observation_id')
  AND ob.workspace_id=v.workspace_id
 LEFT JOIN agent_worker_runs r ON r.id=a.worker_run_id
  AND r.workspace_id=v.workspace_id AND r.task_id=v.task_id
 WHERE v.workspace_id=?
  AND json_extract(e.value,'$.status')='pass'
  AND json_extract(e.value,'$.role') IN ('direct','integration')
 ORDER BY v.started_at DESC,ob.observed_at DESC,ob.id LIMIT ?`
 args=append(args,tenant,qaDeepEvidenceCap+1)
 bounded,cancel:=context.WithTimeout(ctx,3*time.Second);defer cancel()
 rows,err:=db.QueryContext(bounded,query,args...)
 if err!=nil{return nil,false,fmt.Errorf("scoped assurance observation links unavailable: %w",err)}
 type observationRow struct{
  taskID,verificationID,observationID,role,trust,verificationStatus,assuranceStatus,required string
  observedAt,minAttempt,minOperation int64
  source,worker,achieved,assuranceAchieved,digest sql.NullString
 }
 collected:=make([]observationRow,0,qaDeepEvidenceCap)
 truncated:=false
 for rows.Next(){
  var x observationRow
  if err:=rows.Scan(&x.taskID,&x.verificationID,&x.observationID,&x.role,&x.trust,&x.observedAt,
   &x.source,&x.worker,&x.verificationStatus,&x.assuranceStatus,&x.required,&x.achieved,
   &x.assuranceAchieved,&x.digest,&x.minAttempt,&x.minOperation);err!=nil{
   rows.Close();return nil,false,err
  }
  if len(collected)>=qaDeepEvidenceCap{truncated=true;break}
  collected=append(collected,x)
 }
 if err:=rows.Err();err!=nil{rows.Close();return nil,false,err}
 if err:=rows.Close();err!=nil{return nil,false,err}
 // Close the result set before querying individual observation integrity.
 // This works with SQLite connection pools capped to a single connection.
 verifier:=observation.NewService(db,nil,nil)
 for _,x:=range collected{
  intact:=verifier.VerifyIntegrity(bounded,x.observationID)==nil
  afterAttempt:=x.minAttempt>0&&x.observedAt>=x.minAttempt
  afterOperation:=x.minOperation==0||x.observedAt>=x.minOperation
  independent:=x.worker.Valid&&x.worker.String!=""&&x.source.Valid&&x.source.String!=""&&x.source.String!=x.worker.String
  achievedLevel:="";if x.achieved.Valid{achievedLevel=x.achieved.String}
  assuranceLevel:="";if x.assuranceAchieved.Valid{assuranceLevel=x.assuranceAchieved.String}
  recordedPass:=x.verificationStatus=="pass"&&x.assuranceStatus=="passed"&&qaDigestRecorded(x.digest)&&
   qaLevelRank(x.required)>=1&&qaLevelRank(achievedLevel)>=qaLevelRank(x.required)&&
   qaLevelRank(assuranceLevel)>=qaLevelRank(x.required)
  corroborates:=x.role=="integration"&&recordedPass&&intact&&afterAttempt&&afterOperation&&independent
  out=append(out,qaProbeWitness{
   TaskRef:qaOpaqueRef("task",x.taskID),
   VerificationRef:qaOpaqueRef("verification",x.verificationID),
   ObservationRef:qaOpaqueRef("observation",x.observationID),
   Role:x.role,SourceTrust:qaWitnessTrust(x.trust),
   ObservedAfterAttempt:afterAttempt,ObservedAfterOperationStart:afterOperation,
   IntegrityRechecked:intact,IndependentSource:independent,
   RecordedAssurancePass:recordedPass,CorroboratingIntegrationProbe:corroborates,
   EvidenceClass:"recorded_probe_observation_not_external_effect_attestation",
  })
 }
 return out,truncated,nil
}
