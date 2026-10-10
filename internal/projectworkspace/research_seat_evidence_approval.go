package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "mime"
 "sort"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// ResearchSeatEvidenceApproval records separate human consent for named
// frozen Council seats. This record is NOT a content-read capability:
// no worker/dispatch code consumes it, and only local models are in scope.
type ResearchSeatEvidenceApproval struct {
 SessionID string `json:"session_id"`
 ProjectID string `json:"project_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 ResearchManifestSHA256 string `json:"research_manifest_sha256"`
 EvidenceManifestSHA256 string `json:"evidence_manifest_sha256"`
 SeatIDs []string `json:"seat_ids"`
 Policy string `json:"policy"`
 State string `json:"state"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
 RevokedBy *string `json:"revoked_by,omitempty"`
 RevokedAt *int64 `json:"revoked_at,omitempty"`
 ModelVisible bool `json:"model_visible"`
 DispatchEnabled bool `json:"dispatch_enabled"`
}

func canonicalResearchSeats(seats []string)([]string,error){
 if len(seats)<1||len(seats)>8{return nil,ErrInvalidCommand}
 ids:=append([]string(nil),seats...)
 sort.Strings(ids)
 for i,id:=range ids{
  if id==""||len(id)>128||strings.TrimSpace(id)!=id||
   (i>0&&id==ids[i-1]){return nil,ErrInvalidCommand}
 }
 return ids,nil
}
func researchTextSelectionBounded(entries []WorkspaceEvidenceEntry)bool{
 if len(entries)<1||len(entries)>8{return false}
 var total int64
 for _,e:=range entries{
  kind,_,err:=mime.ParseMediaType(strings.TrimSpace(e.MIMEType))
  if err!=nil{return false}
  kind=strings.ToLower(kind)
  textual:=strings.HasPrefix(kind,"text/")
  switch kind{
  case "application/json","application/xml","application/javascript",
   "application/x-yaml","application/yaml","application/toml":
   textual=true
  }
  if !textual||e.SizeBytes<0||e.SizeBytes>64<<10{return false}
  total+=e.SizeBytes
  if total>256<<10{return false}
 }
 return true
}

func frozenResearchSeatsEligible(snapshot []byte,seats []string)bool{
 var manifest struct{
  ResearchMode bool `json:"research_mode"`
  ExecutionMode string `json:"execution_mode"`
  Research struct{ChairMode string `json:"chair_mode"`} `json:"research"`
  Members []struct{
   ID string `json:"id"`
   MemberKind string `json:"member_kind"`
   Status string `json:"status"`
   Config json.RawMessage `json:"config"`
  } `json:"members"`
 }
 if json.Unmarshal(snapshot,&manifest)!=nil||!manifest.ResearchMode||
  manifest.ExecutionMode!="council"||manifest.Research.ChairMode=="manual"{
  return false
 }
 // Council answers are visible to peer seats in later critique rounds.
 // Sharing must therefore cover every active participant, not only the
 // recipients of the first-pass direct evidence packet.
 selected:=map[string]bool{}
 for _,id:=range seats{selected[id]=true}
 count:=0
 for _,member:=range manifest.Members{
  if member.Status!="active"{continue}
  if member.MemberKind!="agent"||!selected[member.ID]{return false}
  var cfg struct{
   CouncilChairOnly bool `json:"council_chair_only"`
   ManualWeb struct{Enabled bool `json:"enabled"`} `json:"manual_web"`
  }
  if len(member.Config)>0&&json.Unmarshal(member.Config,&cfg)!=nil{return false}
  if cfg.CouncilChairOnly||cfg.ManualWeb.Enabled{return false}
  count++
 }
 return count>0&&count==len(selected)&&len(selected)==len(seats)
}

// ApproveResearchSeatEvidence requires a second, explicit human action after
// the metadata freeze and before the first Research turn. An immutable local-
// only seat list is recorded with revocation support. All actual delivery
// remains disabled pending independently reviewed policy enforcement.
func(s *Service) ApproveResearchSeatEvidence(
 ctx context.Context,sessionID,actor string,seats []string,
)(ResearchSeatEvidenceApproval,error){
 if s==nil||s.db==nil||s.tx==nil||sessionID==""||actor==""{
  return ResearchSeatEvidenceApproval{},ErrInvalidCommand
 }
 ids,err:=canonicalResearchSeats(seats)
 if err!=nil{return ResearchSeatEvidenceApproval{},err}
 frozen,err:=s.ResearchEvidence(ctx,sessionID,actor)
 if err!=nil{return ResearchSeatEvidenceApproval{},err}
 if !researchTextSelectionBounded(frozen.Evidence.Entries){return ResearchSeatEvidenceApproval{},ErrInvalidCommand}
 // The frozen metadata and current grant checks are independently repeated
 // INSIDE the write transaction, protecting a grant revoked after preflight.
 canonical,_:=json.Marshal(ids)
 now:=s.clock.UnixMilli()
 evtID,err:=s.ids.New("evt")
 if err!=nil{return ResearchSeatEvidenceApproval{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var tenant,manifestSHA,evidenceSHA string
  var snapshot,entriesRaw string
  err:=tx.QueryRowContext(ctx,`SELECT ss.workspace_id,m.snapshot_sha256,
   m.snapshot_json,e.evidence_manifest_sha256,e.entries_json
  FROM team_sessions ss
  JOIN teams tm ON tm.id=ss.team_id AND tm.workspace_id=ss.workspace_id
   AND tm.status='active'
  JOIN team_session_manifests m ON m.session_id=ss.id
   AND m.research_mode=1 AND m.execution_mode='council'
  JOIN tasks t ON t.id=ss.task_id AND t.workspace_id=ss.workspace_id
   AND t.project_id=? AND t.project_workspace_id=?
  JOIN research_session_evidence_metadata e ON e.session_id=ss.id
   AND e.workspace_id=ss.workspace_id AND e.project_id=t.project_id
   AND e.project_workspace_id=t.project_workspace_id
   AND e.research_manifest_sha256=m.snapshot_sha256
  JOIN projects p ON p.id=t.project_id AND p.workspace_id=ss.workspace_id
   AND p.status='active'
  JOIN project_workspaces w ON w.id=t.project_workspace_id
   AND w.project_id=t.project_id AND w.status='active'
  JOIN principals principal ON principal.id=? AND principal.status='active'
   AND principal.principal_type='human'
  JOIN workspace_memberships membership ON membership.workspace_id=ss.workspace_id
   AND membership.principal_id=principal.id AND membership.status='active'
  WHERE ss.id=? AND ss.status='deliberating' AND ss.round_number=0
   AND NOT EXISTS(SELECT 1 FROM team_turn_requests tr WHERE tr.session_id=ss.id)`,
   frozen.ProjectID,frozen.ProjectWorkspaceID,actor,sessionID).
   Scan(&tenant,&manifestSHA,&snapshot,&evidenceSHA,&entriesRaw)
  if err!=nil{return ErrCrossWorkspace}
  if manifestSHA!=frozen.ResearchManifestSHA256||
   evidenceSHA!=frozen.Evidence.ManifestSHA256||
   !frozenResearchSeatsEligible([]byte(snapshot),ids){
   return ErrCrossWorkspace
  }
  var entries []WorkspaceEvidenceEntry
  if json.Unmarshal([]byte(entriesRaw),&entries)!=nil||
   !researchTextSelectionBounded(entries)||
   len(entries)!=len(frozen.Evidence.Entries){
   return ErrCrossWorkspace
  }
  for i:=range entries{
   if entries[i]!=frozen.Evidence.Entries[i]{return ErrCrossWorkspace}
  }
  if _,err=s.validateResearchEvidenceTx(ctx,tx,sessionID,actor,
   frozen.ProjectID,frozen.ProjectWorkspaceID,manifestSHA,entries);err!=nil{
   return err
  }
  result,err:=tx.ExecContext(ctx,`INSERT INTO research_evidence_seat_approvals(
   session_id,workspace_id,project_id,project_workspace_id,
   research_manifest_sha256,evidence_manifest_sha256,
   seat_ids_json,policy,state,approved_by,approved_at)
   VALUES(?,?,?,?,?,?,?,'local_only','approved',?,?)
   ON CONFLICT(session_id) DO NOTHING`,
   sessionID,tenant,frozen.ProjectID,frozen.ProjectWorkspaceID,
   manifestSHA,evidenceSHA,string(canonical),actor,now)
  if err!=nil{return err}
  inserted,_:=result.RowsAffected()
  if inserted==0{
   var priorJSON,priorActor,priorState,priorSHA,priorEvidence string
   err=tx.QueryRowContext(ctx,`SELECT seat_ids_json,approved_by,state,
    research_manifest_sha256,evidence_manifest_sha256
    FROM research_evidence_seat_approvals WHERE session_id=?`,sessionID).
    Scan(&priorJSON,&priorActor,&priorState,&priorSHA,&priorEvidence)
   if err!=nil||priorJSON!=string(canonical)||priorActor!=actor||
    priorState!="approved"||priorSHA!=manifestSHA||
    priorEvidence!=evidenceSHA{return ErrInvalidCommand}
   return nil
  }
  payload,_:=json.Marshal(map[string]any{
   "session_id":sessionID,"seat_count":len(ids),"policy":"local_only",
   "research_manifest_sha256":manifestSHA,
   "evidence_manifest_sha256":evidenceSHA,
   "dispatch_enabled":false,
  })
  return s.events.Append(ctx,tx,event.Event{ID:evtID,WorkspaceID:&tenant,
   Type:"research.seat_evidence_intent_approved",
   AggregateType:"team_session",AggregateID:sessionID,
   ActorPrincipalID:&actor,Payload:payload,OccurredAt:now})
 })
 if err!=nil{return ResearchSeatEvidenceApproval{},err}
 return s.ResearchSeatEvidenceApproval(ctx,sessionID,actor)
}

// ResearchSeatEvidenceApproval is a human-only receipt. The consent record
// alone is never an inference capability and never authorizes blob access.
func(s *Service) ResearchSeatEvidenceApproval(
 ctx context.Context,sessionID,actor string,
)(ResearchSeatEvidenceApproval,error){
 if s==nil||s.db==nil||sessionID==""||actor==""{
  return ResearchSeatEvidenceApproval{},ErrInvalidCommand
 }
 out:=ResearchSeatEvidenceApproval{ModelVisible:false,DispatchEnabled:false}
 var raw string
 err:=s.db.QueryRowContext(ctx,`SELECT e.project_id,e.project_workspace_id,
  e.research_manifest_sha256,e.evidence_manifest_sha256,e.seat_ids_json,
  e.policy,e.state,e.approved_by,e.approved_at,e.revoked_by,e.revoked_at
 FROM research_evidence_seat_approvals e
 JOIN team_sessions ss ON ss.id=e.session_id AND ss.workspace_id=e.workspace_id
 JOIN teams tm ON tm.id=ss.team_id AND tm.workspace_id=ss.workspace_id
  AND tm.status='active'
 JOIN projects p ON p.id=e.project_id AND p.workspace_id=ss.workspace_id
  AND p.status='active'
 JOIN project_workspaces w ON w.id=e.project_workspace_id
  AND w.project_id=p.id AND w.status='active'
 JOIN workspaces tenant ON tenant.id=ss.workspace_id AND tenant.status='active'
 JOIN principals principal ON principal.id=? AND principal.status='active'
  AND principal.principal_type='human'
 JOIN workspace_memberships member ON member.workspace_id=ss.workspace_id
  AND member.principal_id=principal.id AND member.status='active'
 WHERE e.session_id=?`,actor,sessionID).
 Scan(&out.ProjectID,&out.ProjectWorkspaceID,
  &out.ResearchManifestSHA256,&out.EvidenceManifestSHA256,&raw,
  &out.Policy,&out.State,&out.ApprovedBy,&out.ApprovedAt,
  &out.RevokedBy,&out.RevokedAt)
 if errors.Is(err,sql.ErrNoRows){return ResearchSeatEvidenceApproval{},ErrCrossWorkspace}
 if err!=nil{return ResearchSeatEvidenceApproval{},err}
 if json.Unmarshal([]byte(raw),&out.SeatIDs)!=nil{return ResearchSeatEvidenceApproval{},ErrCrossWorkspace}
 out.SessionID=sessionID
 return out,nil
}

// Revocation succeeds even when prior Library grants were removed: an
// operator must always be able to terminate existing delivery intent.
// Revoked approvals cannot silently become approved again.
func(s *Service) RevokeResearchSeatEvidence(
 ctx context.Context,sessionID,actor string,
)(ResearchSeatEvidenceApproval,error){
 if s==nil||s.db==nil||s.tx==nil||sessionID==""||actor==""{
  return ResearchSeatEvidenceApproval{},ErrInvalidCommand
 }
 now:=s.clock.UnixMilli()
 evtID,err:=s.ids.New("evt")
 if err!=nil{return ResearchSeatEvidenceApproval{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var tenant string
  err:=tx.QueryRowContext(ctx,`SELECT ss.workspace_id
   FROM research_evidence_seat_approvals e
   JOIN team_sessions ss ON ss.id=e.session_id
    AND ss.workspace_id=e.workspace_id
   JOIN principals principal ON principal.id=? AND principal.status='active'
    AND principal.principal_type='human'
   JOIN workspace_memberships member ON member.workspace_id=ss.workspace_id
    AND member.principal_id=principal.id AND member.status='active'
   WHERE e.session_id=? AND e.state='approved'`,actor,sessionID).Scan(&tenant)
  if err!=nil{return ErrCrossWorkspace}
  result,err:=tx.ExecContext(ctx,`UPDATE research_evidence_seat_approvals
   SET state='revoked',revoked_by=?,revoked_at=?
   WHERE session_id=? AND state='approved'`,actor,now,sessionID)
  if err!=nil{return err}
  n,_:=result.RowsAffected()
  if n!=1{return ErrInvalidCommand}
  payload,_:=json.Marshal(map[string]any{
   "session_id":sessionID,"policy":"local_only","dispatch_enabled":false,
  })
  return s.events.Append(ctx,tx,event.Event{ID:evtID,WorkspaceID:&tenant,
   Type:"research.seat_evidence_intent_revoked",
   AggregateType:"team_session",AggregateID:sessionID,
   ActorPrincipalID:&actor,Payload:payload,OccurredAt:now})
 })
 if err!=nil{return ResearchSeatEvidenceApproval{},err}
 // A revoked consent must remain revocable even if its Team or Project was
 // archived, and archived data must not be redisclosed by calling GET.
 // The response is an intentionally minimal, content-free revocation receipt.
 return ResearchSeatEvidenceApproval{
  SessionID:sessionID,Policy:"local_only",State:"revoked",
  RevokedBy:&actor,RevokedAt:&now,
  ModelVisible:false,DispatchEnabled:false,
 },nil
}
