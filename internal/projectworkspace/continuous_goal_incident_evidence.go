package projectworkspace

import (
 "context"
 "database/sql"
 "errors"
 "strings"
)

// GoalIncidentEvidence provides a bounded historical review of an existing
// human-owned incident case. "Shared endpoint" means a past OPERATOR-declared
// IP:port association, not a proven device identity or independent sensor.
// No observation, Task, LLM, socket or remediation is initiated by this read.
type GoalIncidentEvidence struct {
 Review GoalIncidentReview `json:"review"`
 Alert TCPReachabilityAlert `json:"alert"`
 SignedEndpointClaims []GoalIncidentSignedEndpointClaim `json:"signed_endpoint_claims"`
 EndpointAssociation string `json:"endpoint_association"`
 DeviceIdentityVerified bool `json:"device_identity_verified"`
 IndependentVantageVerified bool `json:"independent_vantage_verified"`
 IncidentConfirmed bool `json:"incident_confirmed"`
 RemediationAuthorized bool `json:"remediation_authorized"`
 Limitations []string `json:"limitations"`
}

// No raw IP/port, signature, certificate, challenge or sensor payload.
type GoalIncidentSignedEndpointClaim struct {
 ClaimID string `json:"claim_id"`
 InstrumentKeyID string `json:"instrument_key_id"`
 Outcome string `json:"outcome"`
 AcceptedAt int64 `json:"accepted_at"`
 ProofClass string `json:"proof_class"`
}

func(s *Service)GoalIncidentEvidence(ctx context.Context,projectID,goalID,reviewID string)(GoalIncidentEvidence,error){
 projectID=strings.TrimSpace(projectID)
 goalID=strings.TrimSpace(goalID)
 reviewID=strings.TrimSpace(reviewID)
 if projectID==""||goalID==""||reviewID==""{return GoalIncidentEvidence{},ErrInvalidCommand}
 var active int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&active)
 if err!=nil{return GoalIncidentEvidence{},err}
 review,err:=scanGoalIncidentReview(s.db.QueryRowContext(ctx,`SELECT `+
  goalIncidentReviewColumns+` FROM project_goal_incident_reviews
  WHERE id=? AND project_id=? AND goal_id=?`,reviewID,projectID,goalID))
 if err!=nil{return GoalIncidentEvidence{},err}
 alert,err:=scanTCPReachabilityAlert(s.db.QueryRowContext(ctx,`SELECT `+
  tcpReachabilityAlertColumns+` FROM project_goal_tcp_reachability_alerts
  WHERE id=? AND project_id=? AND goal_id=? AND project_workspace_id=?
    AND source_id=?`,review.TCPAlertID,projectID,goalID,
   review.ProjectWorkspaceID,review.SourceID))
 if err!=nil{return GoalIncidentEvidence{},err}
 out:=GoalIncidentEvidence{
  Review:review,Alert:alert,
  SignedEndpointClaims:[]GoalIncidentSignedEndpointClaim{},
  EndpointAssociation:"not_established",
  Limitations:[]string{
   "tcp_connectivity_is_not_device_security",
   "signed_claim_is_instrument_self_report_without_vantage_attestation",
   "operator_declared_endpoint_is_not_physical_device_identity",
   "historical_evidence_is_not_current_health_or_remediation_authority",
   "paired_node_connectivity_not_associated_with_tcp_target",
  },
 }
 // Historical return-to-baseline means only that another permitted TCP
 // socket test later matched the originally approved baseline. The case
 // and alert remain unchanged, regardless of return status.
 var recoveredID string
 var recoveredAt int64
 err=s.db.QueryRowContext(ctx,`SELECT later.id,later.completed_at
  FROM project_goal_tcp_probe_attempts later
  JOIN project_goal_tcp_probe_attempts original
   ON original.id=? AND original.project_id=? AND original.goal_id=?
   AND original.project_workspace_id=? AND original.source_id=?
  WHERE later.project_id=? AND later.goal_id=?
   AND later.project_workspace_id=? AND later.source_id=?
   AND later.permit_id=original.permit_id
   AND later.permit_revision=original.permit_revision
   AND later.target_ipv4=original.target_ipv4
   AND later.target_port=original.target_port
   AND later.timeout_ms=original.timeout_ms
   AND later.baseline_id_at_claim=?
   AND later.baseline_revision_at_claim=?
   AND later.status=?
   AND later.claimed_at>?
   AND later.completed_at>=later.claimed_at AND later.completed_at<=?
  ORDER BY later.claimed_at ASC,later.id ASC LIMIT 1`,
  alert.AttemptID,alert.ProjectID,alert.GoalID,alert.ProjectWorkspaceID,alert.SourceID,
  alert.ProjectID,alert.GoalID,alert.ProjectWorkspaceID,alert.SourceID,
  alert.BaselineID,alert.BaselineRevision,alert.ExpectedTCP,alert.DetectedAt,
  s.clock.UnixMilli()).Scan(&recoveredID,&recoveredAt)
 if err==nil{
  out.Alert.ReturnToBaselineEvidenceState="historical_tcp_return_observed"
  out.Alert.ReturnToBaselineAttemptID=recoveredID
  out.Alert.ReturnToBaselineAt=recoveredAt
 }else if errors.Is(err,sql.ErrNoRows){
  out.Alert.ReturnToBaselineEvidenceState="not_observed"
 }else{return GoalIncidentEvidence{},err}

 // Historical association requires the TCP target declaration to have been
 // explicitly approved BEFORE the original socket attempt. An expired or
 // revoked declaration cannot be silently reapplied to later attempts.
 var declaredID string
 err=s.db.QueryRowContext(ctx,`SELECT declared.id
  FROM project_goal_tcp_probe_attempts original
  JOIN project_goal_tcp_probe_permits permit ON permit.id=original.permit_id
   AND permit.project_id=original.project_id AND permit.goal_id=original.goal_id
   AND permit.project_workspace_id=original.project_workspace_id
   AND permit.source_id=original.source_id
   AND permit.revision=original.permit_revision
   AND permit.target_ipv4=original.target_ipv4
   AND permit.target_port=original.target_port
  JOIN project_goal_tcp_declared_target_bindings declared
   ON declared.permit_id=permit.id AND declared.project_id=original.project_id
   AND declared.goal_id=original.goal_id
   AND declared.project_workspace_id=original.project_workspace_id
   AND declared.source_id=original.source_id
   AND declared.target_ipv4=original.target_ipv4
   AND declared.target_port=original.target_port
   AND declared.permit_revision=original.permit_revision
   AND declared.plan_hash=permit.plan_hash
  WHERE original.id=? AND original.project_id=? AND original.goal_id=?
   AND original.project_workspace_id=? AND original.source_id=?
   AND permit.approved_at<=original.claimed_at
   AND (permit.revoked_at=0 OR permit.revoked_at>original.claimed_at)
   AND declared.approved_at<original.claimed_at
   AND declared.expires_at>original.claimed_at
   AND (declared.revoked_at=0 OR declared.revoked_at>original.claimed_at)
  ORDER BY declared.approved_at DESC,declared.id DESC LIMIT 1`,
  alert.AttemptID,projectID,goalID,review.ProjectWorkspaceID,review.SourceID).
  Scan(&declaredID)
 if errors.Is(err,sql.ErrNoRows){return out,nil}
 if err!=nil{return GoalIncidentEvidence{},err}
 // A signed instrument claim refers to the SAME historical operator-
 // declared endpoint only when its instrument/key, TLS pin, challenge,
 // source and exact human TCP permit all belong to that declaration.
 // Time proximity does not attest physical-device identity or accuracy.
 const windowMillis int64=15*60*1000
 from:=alert.DetectedAt-windowMillis
 if from<0{from=0}
 rows,err:=s.db.QueryContext(ctx,`SELECT m.id,m.instrument_key_id,m.outcome,
   m.accepted_at,m.proof_class
   FROM project_goal_instrument_measurement_claims m
   JOIN project_goal_instrument_keys key ON key.id=m.instrument_key_id
    AND key.project_id=m.project_id AND key.goal_id=m.goal_id
    AND key.declared_target_id=m.declared_target_id
    AND key.tls_pin_id=m.tls_pin_id
   JOIN project_goal_tls_endpoint_pins pin ON pin.id=m.tls_pin_id
    AND pin.project_id=m.project_id AND pin.goal_id=m.goal_id
    AND pin.declared_target_id=m.declared_target_id
   JOIN project_goal_instrument_challenges challenge ON challenge.id=m.challenge_id
    AND challenge.instrument_key_id=key.id
    AND challenge.project_id=m.project_id AND challenge.goal_id=m.goal_id
    AND challenge.status='accepted'
   JOIN project_goal_tcp_declared_target_bindings d ON d.id=m.declared_target_id
    AND d.project_id=m.project_id AND d.goal_id=m.goal_id
    AND d.project_workspace_id=key.project_workspace_id
    AND d.source_id=m.source_id AND d.permit_id=pin.permit_id
    AND d.permit_revision=pin.permit_revision AND d.plan_hash=pin.plan_hash
   WHERE m.project_id=? AND m.goal_id=? AND m.declared_target_id=?
    AND m.source_id=? AND m.accepted_at BETWEEN ? AND ?
    AND m.accepted_at>=challenge.issued_at
    AND m.accepted_at<=?
   ORDER BY m.accepted_at DESC,m.id DESC LIMIT 8`,
   projectID,goalID,declaredID,review.SourceID,from,
   alert.DetectedAt+windowMillis,s.clock.UnixMilli())
 if err!=nil{return GoalIncidentEvidence{},err}
 defer rows.Close()
 for rows.Next(){
  var claim GoalIncidentSignedEndpointClaim
  if err=rows.Scan(&claim.ClaimID,&claim.InstrumentKeyID,&claim.Outcome,
   &claim.AcceptedAt,&claim.ProofClass);err!=nil{return GoalIncidentEvidence{},err}
  out.SignedEndpointClaims=append(out.SignedEndpointClaims,claim)
 }
 if err=rows.Err();err!=nil{return GoalIncidentEvidence{},err}
 if len(out.SignedEndpointClaims)>0{
  out.EndpointAssociation="historical_operator_declared_same_endpoint"
 }
 return out,nil
}
