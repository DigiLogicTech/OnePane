package api

import (
 "context"
 "database/sql"
 "fmt"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

// qaResearchEvidenceState contains only verified *existence* of an evidence
// approval. Names, content, hash values, selected Library IDs, principal
// identities, snippets and Council outputs never enter a QA snapshot.
type qaResearchEvidenceState struct {
 TaskRef string `json:"task_ref"`
 SessionRef string `json:"session_ref"`
 ApprovalState string `json:"approval_state"`
 VersionCount int `json:"version_count"`
 ResearchManifestRecorded bool `json:"research_manifest_recorded"`
 EvidenceManifestRecorded bool `json:"evidence_manifest_recorded"`
 ApprovedAtMS int64 `json:"approved_at_ms"`
 RoundStarted bool `json:"round_started"`
}

func loadQAResearchEvidence(
 ctx context.Context,db *sql.DB,tenant,projectID,workspaceID string,
 visible []task.Task,
)([]qaResearchEvidenceState,bool,error){
 result:=[]qaResearchEvidenceState{}
 if db==nil{return result,false,nil}
 prefix,args,ok:=qaDeepScope(tenant,projectID,workspaceID,visible)
 if !ok{return result,false,nil}
 query:=prefix+`SELECT t.id,ss.id,
  CASE WHEN json_array_length(e.entries_json) BETWEEN 1 AND 16
   THEN json_array_length(e.entries_json) ELSE 0 END,
  e.research_manifest_sha256,e.evidence_manifest_sha256,
  e.approved_at,ss.round_number
 FROM scoped selection
 JOIN tasks t ON t.id=selection.id AND t.workspace_id=?
   AND t.project_id=? AND t.project_workspace_id=?
 JOIN team_sessions ss ON ss.task_id=t.id AND ss.workspace_id=t.workspace_id
 JOIN team_session_manifests m ON m.session_id=ss.id AND m.research_mode=1
 JOIN research_session_evidence_metadata e ON e.session_id=ss.id
   AND e.workspace_id=t.workspace_id AND e.project_id=t.project_id
   AND e.project_workspace_id=t.project_workspace_id
   AND e.research_manifest_sha256=m.snapshot_sha256
 ORDER BY e.approved_at DESC,ss.id LIMIT ?`
 args=append(args,tenant,projectID,workspaceID,qaDeepEvidenceCap+1)
 bounded,cancel:=context.WithTimeout(ctx,2*time.Second)
 defer cancel()
 rows,err:=db.QueryContext(bounded,query,args...)
 if err!=nil{return nil,false,fmt.Errorf("scoped Research approval evidence unavailable: %w",err)}
 defer rows.Close()
 for rows.Next(){
  var id,session,researchSHA,evidenceSHA string
  var count int
  var approved,round int64
  if err=rows.Scan(&id,&session,&count,&researchSHA,&evidenceSHA,
   &approved,&round);err!=nil{return nil,false,err}
  if len(result)>=qaDeepEvidenceCap{return result,true,nil}
  researchRecorded:=len(researchSHA)==64
  evidenceRecorded:=len(evidenceSHA)==71 && evidenceSHA[:7]=="sha256:"
  state:="metadata_approval_only"
  if count==0||!researchRecorded||!evidenceRecorded{
   state="invalid_recorded_provenance"
  }
  result=append(result,qaResearchEvidenceState{
   TaskRef:qaIdentifier(id),SessionRef:qaIdentifier(session),
   ApprovalState:state,VersionCount:count,
   ResearchManifestRecorded:researchRecorded,
   EvidenceManifestRecorded:evidenceRecorded,
   ApprovedAtMS:approved,RoundStarted:round>0,
  })
 }
 if err=rows.Err();err!=nil{return nil,false,err}
 return result,false,nil
}
