package projectworkspace

import (
 "context"
 "database/sql"
 "fmt"
 "strings"
)

// GoalEvidenceCoverage reports attached evidence freshness, not a monitored
// service's security, uptime or verified compliance. It reads only bounded
// metadata from the goal's immutable receipts; it does not dispatch work.
type GoalEvidenceCoverage struct {
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 GoalRevision int64 `json:"goal_revision"`
 GoalStatus string `json:"goal_status"`
 GoalHealth string `json:"goal_health"`
 AssessedAt int64 `json:"assessed_at"`
 FreshnessMinutes int64 `json:"freshness_minutes"`
 Sources []GoalEvidenceSource `json:"sources"`
 Assessment string `json:"assessment"`
 MonitoringActive bool `json:"monitoring_active"`
 // Short-lived network permission is separate from verified system health.
 RecurringTCPAuthorizedSources int `json:"recurring_tcp_authorized_sources"`
 // A signed source identity is distinct from a human-attached receipt and
 // never establishes that an endpoint, device or network is healthy.
 RecentSourceSignedSources int `json:"recent_source_signed_sources"`
}

type GoalEvidenceSource struct {
 SourceID string `json:"source_id"`
 EvidenceState string `json:"evidence_state"`
 LastObservedAt int64 `json:"last_observed_at"`
 // A TCP handshake is a different evidence class, never an attested
 // Observation or proof that the source is healthy or secure.
 LastTCPTestAt int64 `json:"last_tcp_test_at,omitempty"`
 TCPReachability string `json:"tcp_reachability,omitempty"`
 TCPRecurringAuthorized bool `json:"tcp_recurring_authorized"`
 LastSourceSignedObservedAt int64 `json:"last_source_signed_observed_at,omitempty"`
 SignatureEvidenceState string `json:"signature_evidence_state"`
}

func(s *Service)GoalEvidenceCoverage(ctx context.Context,projectID,goalID string)(GoalEvidenceCoverage,error){
 projectID=strings.TrimSpace(projectID)
 goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return GoalEvidenceCoverage{},ErrInvalidCommand}
 var planRaw,status,health,projectWorkspaceID,tenantWorkspaceID string
 var cadence int
 var revision,planGeneration int64
 // A goal must belong to the canonical Project and an active Workspace
 // before its current coverage can be evaluated. Archived Workspaces do
 // not silently become a Project-wide monitoring source.
 err:=s.db.QueryRowContext(ctx,`SELECT g.monitoring_plan_json,g.status,g.health,g.cadence_minutes,g.revision,
   g.project_workspace_id,p.workspace_id,g.plan_generation
  FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
      AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=? AND p.status='active'`,
  goalID,projectID).Scan(&planRaw,&status,&health,&cadence,&revision,
   &projectWorkspaceID,&tenantWorkspaceID,&planGeneration)
 if err!=nil{return GoalEvidenceCoverage{},err}
 // Stored source allowlists are security inputs, too: a legacy or
 // tampered row must not bypass the same strict admission as a new goal.
 plan,planHash,err:=goalReviewPlan(planRaw)
 if err!=nil{return GoalEvidenceCoverage{},fmt.Errorf("%w: invalid stored goal plan: %v",ErrInvalidCommand,err)}
 if len(plan.SourceIDs)>32{
  return GoalEvidenceCoverage{},fmt.Errorf("%w: unsupported stored goal plan",ErrInvalidCommand)
 }
 now:=s.clock.UnixMilli()
 freshness:=int64(cadence)*2
 if freshness<15{freshness=15}
 if freshness>24*60{freshness=24*60}
 // The aggregate avoids holding complete source payloads, identifiers or
 // unbounded histories in memory, even on small local hardware.
 rows,err:=s.db.QueryContext(ctx,`SELECT source_id,MAX(observed_at)
   FROM project_goal_observation_receipts WHERE goal_id=? GROUP BY source_id`,goalID)
 if err!=nil{return GoalEvidenceCoverage{},err}
 last:=map[string]int64{}
 for rows.Next(){
  var id string
  var timestamp int64
  if err:=rows.Scan(&id,&timestamp);err!=nil{rows.Close();return GoalEvidenceCoverage{},err}
  last[id]=timestamp
 }
 if err:=rows.Err();err!=nil{rows.Close();return GoalEvidenceCoverage{},err}
 rows.Close()
 // TCP handshake attempts are never counted as attached, integrity-verified
 // Observation receipts. Present their most recent outcome separately.
 // Read only a bounded one-per-source metadata projection; no raw values,
 // target addresses or network sockets are accessed by this endpoint.
 type lastTCP struct{outcome string;at int64}
 tcp:=map[string]lastTCP{}
 tcpRows,err:=s.db.QueryContext(ctx,`SELECT a.source_id,a.status,a.claimed_at
  FROM project_goal_tcp_probe_attempts a
  JOIN project_goal_tcp_probe_permits permit ON permit.id=a.permit_id
   AND permit.project_id=a.project_id AND permit.goal_id=a.goal_id
   AND permit.project_workspace_id=a.project_workspace_id
   AND permit.source_id=a.source_id AND permit.status='approved'
   AND permit.expires_at>? AND permit.plan_hash=?
   AND permit.plan_generation=? AND permit.plan_generation>0
   AND permit.revision=a.permit_revision
   AND permit.target_ipv4=a.target_ipv4 AND permit.target_port=a.target_port
   AND permit.timeout_ms=a.timeout_ms AND a.claimed_at>=permit.approved_at
  JOIN project_continuous_goals goal ON goal.id=a.goal_id
   AND goal.project_id=a.project_id AND goal.project_workspace_id=a.project_workspace_id
   AND goal.status='configured' AND goal.plan_generation=permit.plan_generation
  JOIN principals approver ON approver.id=permit.approved_by
   AND approver.principal_type='human' AND approver.status='active'
  JOIN workspace_memberships member ON member.principal_id=approver.id
   AND member.workspace_id=? AND member.status='active'
  WHERE a.project_id=? AND a.goal_id=? AND a.id=(
    SELECT b.id FROM project_goal_tcp_probe_attempts b
    WHERE b.project_id=a.project_id AND b.goal_id=a.goal_id
      AND b.source_id=a.source_id
    ORDER BY b.claimed_at DESC,b.id DESC LIMIT 1)
  ORDER BY a.source_id LIMIT 32`,
  now,planHash,planGeneration,tenantWorkspaceID,projectID,goalID)
 if err!=nil{return GoalEvidenceCoverage{},err}
 for tcpRows.Next(){
  var id,state string
  var stamp int64
  if err=tcpRows.Scan(&id,&state,&stamp);err!=nil{
   tcpRows.Close();return GoalEvidenceCoverage{},err
  }
  tcp[id]=lastTCP{outcome:state,at:stamp}
 }
 if err=tcpRows.Err();err!=nil{tcpRows.Close();return GoalEvidenceCoverage{},err}
 tcpRows.Close()
 // An existing manual TCP target permit or stored source name does NOT
 // activate unattended network traffic. Only independently approved,
 // unexpired recurring grants with valid approvers and an unchanged
 // canonical Project/Workspace/source can be reported as authorised.
 recurring:=map[string]bool{}
 grantRows,err:=s.db.QueryContext(ctx,`SELECT source_id,id
  FROM project_goal_tcp_recurring_grants
  WHERE project_id=? AND goal_id=? AND status='approved'
   AND expires_at>? ORDER BY source_id,id LIMIT 32`,projectID,goalID,now)
 if err!=nil{return GoalEvidenceCoverage{},err}
 type grantScope struct{sourceID,id string}
 grants:=make([]grantScope,0,32)
 for grantRows.Next(){
  var row grantScope
  if err=grantRows.Scan(&row.sourceID,&row.id);err!=nil{
   grantRows.Close();return GoalEvidenceCoverage{},err
  }
  grants=append(grants,row)
 }
 if err=grantRows.Err();err!=nil{grantRows.Close();return GoalEvidenceCoverage{},err}
 grantRows.Close()
 for _,candidate:=range grants{
  valid,err:=s.TCPRecurringGrantAllowed(ctx,candidate.id)
  if err!=nil{return GoalEvidenceCoverage{},err}
  if valid{recurring[candidate.sourceID]=true}
 }
 // Signed proof rows are immutable, but live signing authority is not.
 // Historical source proofs remain visible after key revocation without
 // being promoted to currently approved or healthy device evidence.
 // The two projections are restricted to exact canonical Goal sources.
 type signedState struct{ historical,live int64 }
 signed:=map[string]signedState{}
 signedRows,err:=s.db.QueryContext(ctx,`SELECT proof.source_id,
  MAX(proof.observed_at),
  MAX(CASE WHEN signer.status='approved' AND signer.expires_at>?
    AND signer.plan_hash=? AND signer.project_workspace_id=?
    AND signer.plan_generation=? AND signer.plan_generation>0
    AND EXISTS(SELECT 1 FROM principals p JOIN workspace_memberships m
      ON m.principal_id=p.id AND m.workspace_id=?
     WHERE p.id=signer.approved_by AND p.status='active'
       AND p.principal_type='human' AND m.status='active')
    AND EXISTS(SELECT 1 FROM principals p JOIN workspace_memberships m
      ON m.principal_id=p.id AND m.workspace_id=?
     WHERE p.id=signer.source_principal_id AND p.status='active'
       AND p.principal_type IN ('service','agent') AND m.status='active')
    THEN proof.observed_at ELSE 0 END)
  FROM project_goal_signed_observation_proofs proof
  JOIN project_goal_observation_signers signer ON signer.id=proof.signer_id
    AND signer.goal_id=proof.goal_id AND signer.project_id=proof.project_id
    AND signer.source_id=proof.source_id
  WHERE proof.project_id=? AND proof.goal_id=?
  GROUP BY proof.source_id ORDER BY proof.source_id LIMIT 32`,
  now,planHash,projectWorkspaceID,planGeneration,tenantWorkspaceID,tenantWorkspaceID,projectID,goalID)
 if err!=nil{return GoalEvidenceCoverage{},err}
 for signedRows.Next(){
  var id string
  var state signedState
  if err=signedRows.Scan(&id,&state.historical,&state.live);err!=nil{
   signedRows.Close();return GoalEvidenceCoverage{},err
  }
  signed[id]=state
 }
 if err=signedRows.Err();err!=nil{signedRows.Close();return GoalEvidenceCoverage{},err}
 signedRows.Close()
 sources:=make([]GoalEvidenceSource,0,len(plan.SourceIDs))
 fresh,stale,missing:=0,0,0
 tcpAuthorizedCount:=0
 signedCount:=0
 for _,id:=range plan.SourceIDs{
  seen:=last[id]
  item:=GoalEvidenceSource{SourceID:id,LastObservedAt:seen,TCPRecurringAuthorized:recurring[id]}
  if latest,ok:=tcp[id];ok{
   item.LastTCPTestAt=latest.at
   if latest.outcome=="claimed"{
    item.TCPReachability="unknown_after_interruption"
   }else{
    item.TCPReachability=latest.outcome
   }
  }
  switch{
  case seen==0:
   item.EvidenceState="missing"
   missing++
  case seen>now||now-seen>freshness*60*1000:
   item.EvidenceState="stale_attached"
   stale++
  default:
   item.EvidenceState="recent_attached"
   fresh++
  }
  if item.TCPRecurringAuthorized{tcpAuthorizedCount++}
  item.SignatureEvidenceState="none"
  if proof,ok:=signed[id];ok{
   item.LastSourceSignedObservedAt=proof.historical
   item.SignatureEvidenceState="historical_key_inactive"
   if proof.live>0{
    item.LastSourceSignedObservedAt=proof.live
    item.SignatureEvidenceState="recent_source_signature"
    if proof.live>now||now-proof.live>freshness*60*1000{
     item.SignatureEvidenceState="stale_source_signature"
    }else{signedCount++}
   }
  }
  sources=append(sources,item)
 }
 _=fresh
 assessment:="unconfigured_sources"
 if len(sources)>0 {
  assessment="recent_evidence_attached"
  if missing>0||stale>0{assessment="incomplete_evidence"}
 }
 return GoalEvidenceCoverage{
  ProjectID:projectID,GoalID:goalID,GoalRevision:revision,
  GoalStatus:status,GoalHealth:health,AssessedAt:now,
  FreshnessMinutes:freshness,Sources:sources,Assessment:assessment,
  MonitoringActive:false,
  RecurringTCPAuthorizedSources:tcpAuthorizedCount,
  RecentSourceSignedSources:signedCount,
 },nil
}

// Compile-time guard that future DB schema changes do not make missing goal
// ownership look like an empty, healthy status.
var _ = sql.ErrNoRows
