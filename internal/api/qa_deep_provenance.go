package api

import (
 "context"
 "database/sql"
 "fmt"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// All links are persisted, equality-checked Task/Attempt/Workspace associations
// under the canonical Workspace QA access gate. Not raw agent/model text or
// timing-based guesses. Source rows are re-authorised by SQL on each read.
const qaDeepEvidenceCap=24

type qaWorkerToolEvidence struct {
 TaskRef string `json:"task_ref"`
 RunRef string `json:"run_ref"`
 TargetRef string `json:"target_ref"`
 ObservationRef string `json:"observation_ref,omitempty"`
 Source string `json:"source"`
 JournalStatus string `json:"journal_status"`
 TargetStatus string `json:"target_status"`
 ObservationTrust string `json:"observation_trust,omitempty"`
 LinkKind string `json:"link_kind"`
}

type qaAssuranceEvidence struct {
 TaskRef string `json:"task_ref"`
 VerificationRef string `json:"verification_ref"`
 WorkerRunRef string `json:"worker_run_ref,omitempty"`
 OperationRef string `json:"operation_ref,omitempty"`
 VerificationStatus string `json:"verification_status"`
 AssuranceStatus string `json:"assurance_status"`
 RequiredLevel string `json:"required_level"`
 AchievedLevel string `json:"achieved_level"`
 OperationState string `json:"operation_state,omitempty"`
 EvidenceDigestRecorded bool `json:"evidence_digest_recorded"`
 DistinctVerifierRecorded bool `json:"distinct_verifier_recorded"`
 RecordedAssurancePass bool `json:"recorded_assurance_pass"`
}

func qaDeepScope(tenant,projectID,workspaceID string,visible []task.Task)(string,[]any,bool){
 if tenant==""||projectID==""||workspaceID==""||len(visible)==0{return "",nil,false}
 ids:=make([]string,0,qaSnapshotTaskCap)
 seen:=map[string]bool{}
 for _,t:=range visible{
  if t.ID==""||t.WorkspaceID!=tenant||t.ProjectID==nil||*t.ProjectID!=projectID||
   t.ProjectWorkspaceID==nil||*t.ProjectWorkspaceID!=workspaceID||seen[t.ID]{continue}
  seen[t.ID]=true;ids=append(ids,t.ID)
  if len(ids)>=qaSnapshotTaskCap{break}
 }
 if len(ids)==0{return "",nil,false}
 query:=`WITH scoped AS(SELECT id FROM tasks WHERE workspace_id=? AND project_id=?
  AND project_workspace_id=? AND id IN (`+strings.TrimSuffix(strings.Repeat("?,",len(ids)),",")+`)) `
 args:=make([]any,0,len(ids)+4)
 args=append(args,tenant,projectID,workspaceID)
 for _,v:=range ids{args=append(args,v)}
 return query,args,true
}

func qaSafeWorkerState(s string)string{
 switch s{
 case "started","waiting","succeeded","failed","unknown","interrupted","blocked":return s
 default:return "unavailable"
 }
}
func qaSafeInvocationState(s string)string{
 switch s{
 case "created","authorized","running","succeeded","failed","timed_out","cancelled","interrupted":return s
 default:return "unavailable"
 }
}
func qaSafeOperationState(s string)string{
 switch s{
 case "proposed","authorized","prepared","executing","observing","verified",
 "committed","denied","failed","aborted","unknown_outcome",
 "blocked_unknown_outcome","compensating","compensated","compensation_failed":return s
 default:return "unavailable"
 }
}
func qaSafeAssuranceState(s string)string{
 switch s{
 case "queued","running","waiting_evidence","waiting_human","passed","failed","inconclusive","interrupted":return s
 case "":return "not_recorded"
 default:return "unavailable"
 }
}
func qaSafeVerificationState(s string)string{
 switch s{case "pending","pass","fail","inconclusive","stale":return s}
 return "unavailable"
}
func qaSafeLevel(s string)string{
 switch s{case "V0","V1","V2","V3","V4","V5":return s}
 return "unavailable"
}
func qaLevelRank(s string)int{
 switch s{case "V0":return 0;case "V1":return 1;case "V2":return 2;case "V3":return 3;case "V4":return 4;case "V5":return 5}
 return -1
}
func qaDigestRecorded(s sql.NullString)bool{
 if !s.Valid||len(s.String)!=71||!strings.HasPrefix(s.String,"sha256:"){return false}
 for _,c:=range s.String[7:]{if !((c>='0'&&c<='9')||(c>='a'&&c<='f')){return false}}
 return true
}

// A trusted Worker->Tool link requires the Worker journal result_ref to
// refer to a persisted observation whose subject precisely names a Tool
// invocation on the *same Task, same Attempt and same tenant*. Mutations use
// an independent operation journal -> persisted operation match.
func loadQAWorkerToolEvidence(ctx context.Context,db *sql.DB,tenant,projectID,workspaceID string,visible []task.Task)([]qaWorkerToolEvidence,bool,error){
 out:=make([]qaWorkerToolEvidence,0)
 if db==nil{return out,false,nil}
 prefix,args,ok:=qaDeepScope(tenant,projectID,workspaceID,visible)
 if !ok{return out,false,nil}
 query:=prefix+`SELECT source,task_id,run_id,target_id,observation_id,journal_status,target_status,trust FROM(
 SELECT 'tool' source,r.task_id,r.id run_id,i.id target_id,ob.id observation_id,
  st.status journal_status,i.status target_status,ob.trust trust,st.started_at occurred
 FROM agent_worker_steps st
 JOIN agent_worker_runs r ON r.id=st.run_id
 JOIN scoped t ON t.id=r.task_id
 JOIN observations ob ON ob.id=st.result_ref AND ob.workspace_id=r.workspace_id
   AND ob.observation_type='agent_tool_result'
 JOIN tool_invocations i ON ob.subject_ref='tool_invocation:'||i.id
   AND i.workspace_id=r.workspace_id AND i.task_id=r.task_id AND i.attempt_id=r.attempt_id
 WHERE r.workspace_id=? AND st.step_kind='tool' AND st.status='succeeded'
 UNION ALL
 SELECT 'operation',r.task_id,r.id,o.id,NULL,st.status,o.state,NULL,st.started_at
 FROM agent_worker_steps st
 JOIN agent_worker_runs r ON r.id=st.run_id
 JOIN scoped t ON t.id=r.task_id
 JOIN operations o ON o.id=st.result_ref AND o.workspace_id=r.workspace_id
  AND o.task_id=r.task_id AND o.attempt_id=r.attempt_id
 WHERE r.workspace_id=? AND st.step_kind='operation' AND st.status IN('succeeded','unknown')
 ) ORDER BY occurred DESC,run_id,target_id LIMIT ?`
 args=append(args,tenant,tenant,qaDeepEvidenceCap+1)
 bounded,cancel:=context.WithTimeout(ctx,2*time.Second);defer cancel()
 rows,err:=db.QueryContext(bounded,query,args...)
 if err!=nil{return nil,false,fmt.Errorf("load worker operation provenance: %w",err)}
 defer rows.Close()
 for rows.Next(){
  var kind,taskID,runID,targetID,journal,targetStatus string
  var observationID,trust sql.NullString
  if err:=rows.Scan(&kind,&taskID,&runID,&targetID,&observationID,&journal,&targetStatus,&trust);err!=nil{return nil,false,err}
  if len(out)>=qaDeepEvidenceCap{return out,true,nil}
  if kind!="tool"&&kind!="operation"{return nil,false,fmt.Errorf("unrecognised worker provenance category")}
  targetState:="unavailable";obsTrust:=""
  if kind=="tool"{
   targetState=qaSafeInvocationState(targetStatus)
   if trust.Valid{
    switch trust.String{case "unverified_derived","verified_derived","trusted_control":obsTrust=trust.String;default:obsTrust="unavailable"}
   }
  }else{targetState=qaSafeOperationState(targetStatus)}
  entry:=qaWorkerToolEvidence{TaskRef:qaOpaqueRef("task",taskID),
   RunRef:qaOpaqueRef("run",runID),TargetRef:qaOpaqueRef(kind,targetID),
   Source:kind,JournalStatus:qaSafeWorkerState(journal),
   TargetStatus:targetState,ObservationTrust:obsTrust,
   LinkKind:"persisted_same_task_attempt_reference"}
  if kind=="tool"&&observationID.Valid{entry.ObservationRef=qaOpaqueRef("observation",observationID.String)}
  out=append(out,entry)
 }
 if err:=rows.Err();err!=nil{return nil,false,err}
 return out,false,nil
}

// A recorded assurance pass is NOT a claim that the external world changed.
// It reports only a consistent persisted V1-V5 assurance row, recorded
// evidence digest, matching verification level and a different verifier from
// the linked Worker run. Missing or inconsistent links remain unverified.
func loadQAAssuranceEvidence(ctx context.Context,db *sql.DB,tenant,projectID,workspaceID string,visible []task.Task)([]qaAssuranceEvidence,bool,error){
 out:=make([]qaAssuranceEvidence,0)
 if db==nil{return out,false,nil}
 prefix,args,ok:=qaDeepScope(tenant,projectID,workspaceID,visible)
 if !ok{return out,false,nil}
 query:=prefix+`SELECT v.task_id,v.id,v.status,v.required_level,v.achieved_level,
 v.operation_id,v.verified_by,a.status,a.evidence_hash,
 a.worker_run_id,r.id,r.worker_principal_id,o.id,o.state,a.achieved_level
 FROM verifications v JOIN scoped t ON t.id=v.task_id
 LEFT JOIN assurance_runs a ON a.verification_id=v.id AND a.workspace_id=v.workspace_id
   AND a.task_id=v.task_id
   AND (v.operation_id IS NULL AND a.operation_id IS NULL OR a.operation_id=v.operation_id)
 LEFT JOIN agent_worker_runs r ON r.id=a.worker_run_id AND r.workspace_id=v.workspace_id
   AND r.task_id=v.task_id
 LEFT JOIN operations o ON o.id=v.operation_id AND o.workspace_id=v.workspace_id
   AND o.task_id=v.task_id
 WHERE v.workspace_id=?
 ORDER BY v.started_at DESC,v.id DESC LIMIT ?`
 args=append(args,tenant,qaDeepEvidenceCap+1)
 bounded,cancel:=context.WithTimeout(ctx,2*time.Second);defer cancel()
 rows,err:=db.QueryContext(bounded,query,args...)
 if err!=nil{return nil,false,fmt.Errorf("load scoped assurance evidence: %w",err)}
 defer rows.Close()
 for rows.Next(){
  var taskID,verificationID,status,required string
  var achieved,opID,verifiedBy,assuranceStatus,digest,declaredWorkerID,workerID,workerPrincipal,joinedOpID,operationState,assuranceAchieved sql.NullString
  if err:=rows.Scan(&taskID,&verificationID,&status,&required,&achieved,&opID,&verifiedBy,&assuranceStatus,&digest,
   &declaredWorkerID,&workerID,&workerPrincipal,&joinedOpID,&operationState,&assuranceAchieved);err!=nil{return nil,false,err}
  if len(out)>=qaDeepEvidenceCap{return out,true,nil}
  entry:=qaAssuranceEvidence{TaskRef:qaOpaqueRef("task",taskID),
   VerificationRef:qaOpaqueRef("verification",verificationID),
   VerificationStatus:qaSafeVerificationState(status),
   RequiredLevel:qaSafeLevel(required),AchievedLevel:"unavailable",
   AssuranceStatus:"not_recorded"}
  if achieved.Valid{entry.AchievedLevel=qaSafeLevel(achieved.String)}
  if assuranceStatus.Valid{entry.AssuranceStatus=qaSafeAssuranceState(assuranceStatus.String)}
  if joinedOpID.Valid&&opID.Valid&&opID.String==joinedOpID.String{
   entry.OperationRef=qaOpaqueRef("operation",joinedOpID.String)
   if operationState.Valid{entry.OperationState=qaSafeOperationState(operationState.String)}
  }
  if declaredWorkerID.Valid&&workerID.Valid&&declaredWorkerID.String==workerID.String{
   entry.WorkerRunRef=qaOpaqueRef("run",workerID.String)
   entry.DistinctVerifierRecorded=verifiedBy.Valid&&verifiedBy.String!=""&&
    workerPrincipal.Valid&&workerPrincipal.String!=""&&verifiedBy.String!=workerPrincipal.String
  }
  entry.EvidenceDigestRecorded=qaDigestRecorded(digest)
  entry.RecordedAssurancePass=entry.AssuranceStatus=="passed"&&entry.VerificationStatus=="pass"&&
   entry.EvidenceDigestRecorded&&entry.DistinctVerifierRecorded&&
   qaLevelRank(entry.RequiredLevel)>=1&&qaLevelRank(entry.AchievedLevel)>=qaLevelRank(entry.RequiredLevel)&&
   assuranceAchieved.Valid&&qaLevelRank(assuranceAchieved.String)>=qaLevelRank(entry.RequiredLevel)
  out=append(out,entry)
 }
 if err:=rows.Err();err!=nil{return nil,false,err}
 return out,false,nil
}
