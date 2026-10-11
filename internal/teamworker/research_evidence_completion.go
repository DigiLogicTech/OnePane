package teamworker

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/id"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

var errProtectedResearchCompletion = errors.New("protected Research seat completion no longer authorized")

// completeProtectedResearchTurn is the ONLY success path for model outputs
// created with frozen Research Library content. Consent, version permissions,
// message insertion and turn success occur in ONE SQLite write transaction.
// A revoked grant cannot race between the final check and durable response.
func(s *Service) completeProtectedResearchTurn(
 ctx context.Context, tenant,sessionID,turnID,seatID,researchSHA,evidenceSHA string,
 round int64, message json.RawMessage,
)(string,error){
 if s==nil||s.tx==nil||tenant==""||sessionID==""||turnID==""||
  seatID==""||researchSHA==""||evidenceSHA==""||round<1||
  len(message)==0||!json.Valid(message){
  return "",errProtectedResearchCompletion
 }
 var envelope struct{
  ResearchProvenance struct{
   SessionManifestSHA256 string `json:"session_manifest_sha256"`
   ResearchEvidenceAttached bool `json:"research_evidence_attached"`
   ResearchEvidenceManifestSHA256 string `json:"research_evidence_manifest_sha256"`
  } `json:"research_provenance"`
 }
 if json.Unmarshal(message,&envelope)!=nil||
  !envelope.ResearchProvenance.ResearchEvidenceAttached||
  envelope.ResearchProvenance.ResearchEvidenceManifestSHA256!=evidenceSHA||
  envelope.ResearchProvenance.SessionManifestSHA256!=researchSHA {
  return "",errProtectedResearchCompletion
 }
 mid,e:=id.Generator{}.New("tmsg")
 if e!=nil{return "",e}
 eventID,e:=id.Generator{}.New("evt")
 if e!=nil{return "",e}
 now:=s.clock.UnixMilli()
 err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var projectID,workspaceID,entriesJSON string
  // Bind an active, complete approval to a running exact seat and Council
  // round, active tenant/Project/Team and original approving human.
  err:=tx.QueryRowContext(ctx,`SELECT e.project_id,e.project_workspace_id,e.entries_json
   FROM research_evidence_seat_approvals approval
   JOIN research_session_evidence_metadata e ON e.session_id=approval.session_id
    AND e.workspace_id=approval.workspace_id AND e.project_id=approval.project_id
    AND e.project_workspace_id=approval.project_workspace_id
    AND e.research_manifest_sha256=approval.research_manifest_sha256
    AND e.evidence_manifest_sha256=approval.evidence_manifest_sha256
   JOIN team_sessions ss ON ss.id=approval.session_id AND ss.workspace_id=approval.workspace_id
   JOIN team_session_manifests m ON m.session_id=ss.id AND m.research_mode=1
    AND m.execution_mode='council' AND m.snapshot_sha256=approval.research_manifest_sha256
   JOIN team_turn_requests tr ON tr.session_id=ss.id AND tr.workspace_id=ss.workspace_id
    AND tr.member_id=? AND tr.id=? AND tr.round_number=?
    AND tr.status='running' AND tr.response_message_id IS NULL
   JOIN teams tm ON tm.id=ss.team_id AND tm.workspace_id=ss.workspace_id
    AND tm.status='active'
   JOIN tasks task ON task.id=ss.task_id AND task.workspace_id=ss.workspace_id
    AND task.project_id=e.project_id AND task.project_workspace_id=e.project_workspace_id
   JOIN projects p ON p.id=e.project_id AND p.workspace_id=ss.workspace_id
    AND p.status='active'
   JOIN project_workspaces pw ON pw.id=e.project_workspace_id
    AND pw.project_id=p.id AND pw.status='active'
   JOIN workspaces tenant ON tenant.id=ss.workspace_id AND tenant.status='active'
   JOIN principals human ON human.id=approval.approved_by
    AND human.principal_type='human' AND human.status='active'
   JOIN workspace_memberships membership ON membership.principal_id=human.id
    AND membership.workspace_id=ss.workspace_id AND membership.status='active'
   WHERE ss.id=? AND ss.workspace_id=? AND ss.status='deliberating'
    AND ss.round_number=? AND approval.state='approved'
    AND approval.policy='local_only' AND approval.research_manifest_sha256=?
    AND approval.evidence_manifest_sha256=?
    AND EXISTS(SELECT 1 FROM json_each(approval.seat_ids_json)
     WHERE json_each.value=?)
    AND EXISTS(SELECT 1 FROM principals worker WHERE worker.id=?
     AND worker.principal_type='system' AND worker.status='active')`,
   seatID,turnID,round,sessionID,tenant,round,researchSHA,evidenceSHA,seatID,
   WorkerPrincipal).Scan(&projectID,&workspaceID,&entriesJSON)
  if err!=nil{return errProtectedResearchCompletion}
  var entries []projectworkspace.WorkspaceEvidenceEntry
  if json.Unmarshal([]byte(entriesJSON),&entries)!=nil||
   len(entries)<1||len(entries)>researchSeatDeliveryMaxEntries{
   return errProtectedResearchCompletion
  }
  total:=int64(0)
  for _,entry:=range entries{
   if entry.AssetID==""||entry.Version<1||entry.ContentHash==""||
    !researchSeatTextAllowed(entry.MIMEType,entry.SizeBytes){
    return errProtectedResearchCompletion
   }
   total+=entry.SizeBytes
   if total>researchSeatDeliveryMaxTotalBytes{return errProtectedResearchCompletion}
   var eligible int
   err=tx.QueryRowContext(ctx,`SELECT COUNT(*)
    FROM project_library_assets a
    JOIN project_library_asset_versions v ON v.asset_id=a.id
    WHERE a.id=? AND a.project_id=? AND a.archived=0
    AND v.version=? AND v.content_hash=? AND v.size_bytes=? AND v.mime_type=?
    AND (
     EXISTS(SELECT 1 FROM workspace_library_grants grant
      WHERE grant.asset_id=a.id AND grant.project_workspace_id=?
       AND grant.enabled=1 AND json_extract(grant.permissions_json,'$.read')=1
       AND ((grant.version_policy='latest' AND a.current_version=v.version)
         OR (grant.version_policy='pinned' AND grant.pinned_version=v.version)))
     OR EXISTS(SELECT 1 FROM project_workspace_publications pub
      JOIN project_workspace_links link ON link.id=pub.link_id
      WHERE pub.asset_id=a.id AND pub.asset_version=v.version
       AND pub.content_hash=v.content_hash AND link.project_id=a.project_id
       AND link.target_workspace_id=? AND link.enabled=1
       AND (link.expires_at_ms IS NULL OR link.expires_at_ms>?)
       AND EXISTS(SELECT 1 FROM project_workspaces source
        WHERE source.id=link.source_workspace_id AND source.project_id=a.project_id
         AND source.status='active'))
    )`,
    entry.AssetID,projectID,entry.Version,entry.ContentHash,entry.SizeBytes,
    entry.MIMEType,workspaceID,workspaceID,now).Scan(&eligible)
   if err!=nil||eligible!=1{return errProtectedResearchCompletion}
  }
  result,e:=tx.ExecContext(ctx,`INSERT INTO team_messages(
   id,workspace_id,session_id,author_member_id,author_principal_id,
   message_kind,content_json,reply_to_message_id,round_number,created_at)
   VALUES(?,?,?,?,?,'agent',?,NULL,?,?)`,
   mid,tenant,sessionID,seatID,WorkerPrincipal,string(message),round,now)
  if e!=nil{return e}
  n,_:=result.RowsAffected()
  if n!=1{return errProtectedResearchCompletion}
  result,e=tx.ExecContext(ctx,`UPDATE team_turn_requests SET status='succeeded',
   response_message_id=?,updated_at=?,completed_at=?
   WHERE id=? AND session_id=? AND member_id=? AND round_number=?
    AND status='running' AND response_message_id IS NULL`,
   mid,now,now,turnID,sessionID,seatID,round)
  if e!=nil{return e}
  n,_=result.RowsAffected()
  if n!=1{return errProtectedResearchCompletion}
  // No private response, artifact identifier or document text in the event.
  payload,_:=json.Marshal(map[string]any{
   "message_id":mid,"kind":"agent","round":round,
   "research_evidence_verified":true,
  })
  if e=s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&tenant,Type:"team.message_posted",
   AggregateType:"team_session",AggregateID:sessionID,
   ActorPrincipalID:strptr(WorkerPrincipal),Payload:payload,OccurredAt:now,
  });e!=nil{return fmt.Errorf("record protected Research completion: %w",e)}
  return nil
 })
 if err!=nil{return "",err}
 return mid,nil
}
