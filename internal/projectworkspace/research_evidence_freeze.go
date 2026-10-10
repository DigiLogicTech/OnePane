package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// FreezeResearchEvidence atomically freezes a human-reviewed metadata
// selection before any independent Council seat or round begins. The record
// is neither model-visible context nor an artifact read capability.
func(s *Service) FreezeResearchEvidence(
 ctx context.Context,sessionID,actor string,selections []WorkspaceEvidenceSelection,
)(ResearchEvidenceSelection,error){
 if s==nil||s.db==nil||s.tx==nil||sessionID==""||actor==""{
  return ResearchEvidenceSelection{},ErrInvalidCommand
 }
 var projectID,workspaceID,manifestSHA string
 err:=s.db.QueryRowContext(ctx,`SELECT t.project_id,t.project_workspace_id,
  m.snapshot_sha256 FROM team_sessions ss
 JOIN tasks t ON t.id=ss.task_id
 JOIN team_session_manifests m ON m.session_id=ss.id
 WHERE ss.id=? AND m.research_mode=1 AND m.execution_mode='council'
 AND t.project_id IS NOT NULL AND t.project_workspace_id IS NOT NULL`,
 sessionID).Scan(&projectID,&workspaceID,&manifestSHA)
 if err!=nil{return ResearchEvidenceSelection{},ErrCrossWorkspace}
 // Reject an ineligible actor *before* probing Library permissions,
 // so an agent cannot use the approval endpoint for metadata discovery.
 var humanCount int
 err=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM team_sessions ss
  JOIN principals principal ON principal.id=? AND principal.status='active'
   AND principal.principal_type='human'
  JOIN workspace_memberships membership ON membership.workspace_id=ss.workspace_id
   AND membership.principal_id=principal.id AND membership.status='active'
  WHERE ss.id=?`,actor,sessionID).Scan(&humanCount)
 if err!=nil||humanCount!=1{return ResearchEvidenceSelection{},ErrCrossWorkspace}
 packet,err:=s.BuildWorkspaceEvidencePacket(ctx,projectID,workspaceID,selections)
 if err!=nil{return ResearchEvidenceSelection{},err}
 entriesJSON,err:=json.Marshal(packet.Entries)
 if err!=nil{return ResearchEvidenceSelection{},err}
 selectionJSON,err:=json.Marshal(selections)
 if err!=nil{return ResearchEvidenceSelection{},err}
 now:=s.clock.UnixMilli()
 eventID,err:=s.ids.New("evt")
 if err!=nil{return ResearchEvidenceSelection{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  tenant,e:=s.validateResearchEvidenceTx(ctx,tx,sessionID,actor,
   projectID,workspaceID,manifestSHA,packet.Entries)
  if e!=nil{return e}
  result,e:=tx.ExecContext(ctx,`INSERT INTO research_session_evidence_metadata(
   session_id,workspace_id,project_id,project_workspace_id,
   research_manifest_sha256,evidence_manifest_sha256,
   entries_json,selection_json,approved_by,approved_at)
  VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(session_id) DO NOTHING`,
  sessionID,tenant,projectID,workspaceID,manifestSHA,
  packet.ManifestSHA256,string(entriesJSON),string(selectionJSON),actor,now)
  if e!=nil{return e}
  added,_:=result.RowsAffected()
  if added==0{
   var digest,researchSHA,approvedBy string
   e=tx.QueryRowContext(ctx,`SELECT evidence_manifest_sha256,
    research_manifest_sha256,approved_by FROM research_session_evidence_metadata
    WHERE session_id=?`,sessionID).Scan(&digest,&researchSHA,&approvedBy)
   if e!=nil||digest!=packet.ManifestSHA256||
    researchSHA!=manifestSHA||approvedBy!=actor{
    return ErrInvalidCommand
   }
   return nil
  }
  payload,_:=json.Marshal(map[string]any{
   "session_id":sessionID,"project_id":projectID,
   "project_workspace_id":workspaceID,"research_manifest_sha256":manifestSHA,
   "evidence_manifest_sha256":packet.ManifestSHA256,
   "entry_count":len(packet.Entries),"model_visible":false,
  })
  return s.events.Append(ctx,tx,event.Event{ID:eventID,
   WorkspaceID:&tenant,Type:"research.evidence_metadata_frozen",
   AggregateType:"team_session",AggregateID:sessionID,
   ActorPrincipalID:&actor,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return ResearchEvidenceSelection{},err}
 return s.ResearchEvidence(ctx,sessionID,actor)
}

// ResearchEvidence never returns an old frozen receipt as a transferable
// capability. Active human and Project membership are necessary, and all
// exact Library selections are reauthorised before any metadata is released.
func(s *Service) ResearchEvidence(ctx context.Context,sessionID,actor string)(
 ResearchEvidenceSelection,error){
 if s==nil||s.db==nil||sessionID==""||actor==""{
  return ResearchEvidenceSelection{},ErrInvalidCommand
 }
 var out ResearchEvidenceSelection
 var selectionsJSON,storedSHA string
 err:=s.db.QueryRowContext(ctx,`SELECT e.project_id,e.project_workspace_id,
  e.research_manifest_sha256,e.evidence_manifest_sha256,e.selection_json,
  e.approved_by,e.approved_at
 FROM research_session_evidence_metadata e
 JOIN team_sessions ss ON ss.id=e.session_id
 JOIN team_session_manifests m ON m.session_id=ss.id
  AND m.research_mode=1 AND m.snapshot_sha256=e.research_manifest_sha256
 JOIN tasks t ON t.id=ss.task_id AND t.project_id=e.project_id
  AND t.project_workspace_id=e.project_workspace_id
 JOIN principals actor ON actor.id=? AND actor.principal_type='human'
  AND actor.status='active'
 JOIN workspace_memberships member ON member.workspace_id=ss.workspace_id
  AND member.principal_id=actor.id AND member.status='active'
 WHERE e.session_id=? AND e.workspace_id=ss.workspace_id`,
 actor,sessionID).Scan(&out.ProjectID,&out.ProjectWorkspaceID,
 &out.ResearchManifestSHA256,&storedSHA,&selectionsJSON,
 &out.ApprovedBy,&out.ApprovedAt)
 if errors.Is(err,sql.ErrNoRows){return ResearchEvidenceSelection{},ErrCrossWorkspace}
 if err!=nil{return ResearchEvidenceSelection{},err}
 var selections []WorkspaceEvidenceSelection
 if json.Unmarshal([]byte(selectionsJSON),&selections)!=nil{
  return ResearchEvidenceSelection{},ErrCrossWorkspace
 }
 packet,err:=s.BuildWorkspaceEvidencePacket(ctx,out.ProjectID,out.ProjectWorkspaceID,selections)
 if err!=nil||packet.ManifestSHA256!=storedSHA{
  return ResearchEvidenceSelection{},ErrCrossWorkspace
 }
 out.SessionID=sessionID;out.Evidence=packet
 return out,nil
}
