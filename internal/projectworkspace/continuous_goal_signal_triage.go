package projectworkspace

import (
 "context"
 "database/sql"
 "errors"
 "strings"
)

// GoalSignalTriage is a read-only operator diagnostic. It never claims
// independent external device identity, verified security, incident
// confidence, active monitoring or authority to execute a response.
type GoalSignalTriage struct {
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 GoalRevision int64 `json:"goal_revision"`
 GoalStatus string `json:"goal_status"`
 GoalHealth string `json:"goal_health"`
 AssessedAt int64 `json:"assessed_at"`
 SignalFreshnessMinutes int64 `json:"signal_freshness_minutes"`
 State string `json:"state"`
 RecentNegativeSources int `json:"recent_negative_sources"`
 RecentTCPNegativeSources int `json:"recent_tcp_negative_sources"`
 RecentNodeNegativeSources int `json:"recent_node_negative_sources"`
 Sources []GoalSignalTriageSource `json:"sources"`
 MonitoringActive bool `json:"monitoring_active"`
 DeviceIdentityVerified bool `json:"device_identity_verified"`
 RemediationAuthorized bool `json:"remediation_authorized"`
 CrossClassTargetCorrelation string `json:"cross_class_target_correlation"`
 Limitations []string `json:"limitations"`
}
type GoalSignalTriageSource struct {
 SourceID string `json:"source_id"`
 TCPState string `json:"tcp_state"`
 TCPObservedAt int64 `json:"tcp_observed_at,omitempty"`
 TCPAttemptID string `json:"tcp_attempt_id,omitempty"`
 TCPDeclaredTargetID string `json:"tcp_declared_target_id,omitempty"`
 TCPIdentityClass string `json:"tcp_identity_class,omitempty"`
 TLSCheckState string `json:"tls_check_state"`
 TLSCheckID string `json:"tls_check_id,omitempty"`
 TLSPinID string `json:"tls_pin_id,omitempty"`
 TLSIdentityClass string `json:"tls_identity_class,omitempty"`
 TLSCertificatePossessionMatched bool `json:"tls_certificate_possession_matched"`
 InstrumentProofState string `json:"instrument_proof_state"`
 InstrumentProofID string `json:"instrument_proof_id,omitempty"`
 InstrumentKeyID string `json:"instrument_key_id,omitempty"`
 InstrumentProofClass string `json:"instrument_proof_class,omitempty"`
 InstrumentKeyPossessionProven bool `json:"instrument_key_possession_proven"`
 NodeState string `json:"node_state"`
 NodeObservedAt int64 `json:"node_observed_at,omitempty"`
 NodeReceiptID string `json:"node_receipt_id,omitempty"`
 NodeIdentityClass string `json:"node_identity_class,omitempty"`
 PeerNodeID string `json:"peer_node_id,omitempty"`
 NegativeIndicator bool `json:"negative_indicator"`
}

// GoalSignalTriage only cross-references existing approved, exact-Goal,
// immutable metadata. A TCP handshake checks one human-consented socket;
// peer_connectivity is a self-report by one pinned-mTLS Node. Their
// subjects are NOT necessarily the same device and must never be
// described as corroborated device health or a confirmed incident.
func(s *Service)GoalSignalTriage(ctx context.Context,
 projectID,goalID string)(GoalSignalTriage,error){
 projectID=strings.TrimSpace(projectID)
 goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return GoalSignalTriage{},ErrInvalidCommand}
 coverage,err:=s.GoalEvidenceCoverage(ctx,projectID,goalID)
 if err!=nil{return GoalSignalTriage{},err}
 // The Goal plan is a security input. An old TCP permit from a
 // different plan revision may not inherit authority solely because its
 // human-readable source ID still exists in the new plan.
 var planRaw string
 err=s.db.QueryRowContext(ctx,`SELECT monitoring_plan_json FROM
  project_continuous_goals WHERE id=? AND project_id=?`,
  goalID,projectID).Scan(&planRaw)
 if err!=nil{return GoalSignalTriage{},err}
 _,planHash,err:=goalReviewPlan(planRaw)
 if err!=nil{return GoalSignalTriage{},ErrInvalidCommand}
 now:=s.clock.UnixMilli()
 // Conservative bounded freshness even if a Goal is configured to
 // run weekly. Older telemetry must not trigger a contemporary alert.
 const freshnessMS int64=15*60*1000
 out:=GoalSignalTriage{
  ProjectID:projectID,GoalID:goalID,GoalRevision:coverage.GoalRevision,
  GoalStatus:coverage.GoalStatus,GoalHealth:coverage.GoalHealth,
  AssessedAt:now,SignalFreshnessMinutes:15,
  State:"no_recent_measurements",
  CrossClassTargetCorrelation:"not_established",
  Sources:make([]GoalSignalTriageSource,0,len(coverage.Sources)),
  Limitations:[]string{
   "tcp_connect_tests_reachability_not_device_security",
   "node_peer_connectivity_is_self_report_not_physical_device_attestation",
   "pinned_tls_certificate_proves_key_possession_not_physical_device_integrity",
   "instrument_signed_challenge_proves_key_possession_not_physical_device_or_measurement_accuracy",
   "different_sources_may_not_measure_same_target",
   "read_only_no_automated_monitoring_or_remediation",
  },
 }
 // A paused or retired Goal must not be represented as an active
 // current-signal evaluation even if it retains historical receipts.
 if coverage.GoalStatus!="configured"{
  out.State="goal_not_configured"
  return out,nil
 }
 seenTCP,seenNode:=false,false
 for _,source:=range coverage.Sources{
  item:=GoalSignalTriageSource{
   SourceID:source.SourceID,TCPState:"no_current_approved_measurement",
   TLSCheckState:"no_current_approved_check",
   InstrumentProofState:"no_current_authenticated_proof",
   NodeState:"no_current_approved_measurement",
  }
  // Bound each query to ONE last completed TCP result for this exact Goal
  // source. No target IP, port, raw receipt, socket or secret is returned.
  var tcpStatus string
  var tcpID,tcpPermitID string
  var tcpAt,tcpClaimedAt int64
  err=s.db.QueryRowContext(ctx,`SELECT a.id,a.permit_id,a.status,a.claimed_at,a.completed_at
   FROM project_goal_tcp_probe_attempts a
   JOIN project_goal_tcp_probe_permits p ON p.id=a.permit_id
     AND p.goal_id=a.goal_id AND p.project_id=a.project_id
     AND p.source_id=a.source_id AND p.status='approved'
     AND p.expires_at>? AND p.plan_hash=?
     AND a.permit_revision=p.revision
     AND a.target_ipv4=p.target_ipv4
     AND a.target_port=p.target_port
     AND a.timeout_ms=p.timeout_ms
     AND a.claimed_at>=p.approved_at
   JOIN project_continuous_goals g ON g.id=a.goal_id
    AND g.project_id=a.project_id AND g.status='configured'
    AND g.project_workspace_id=a.project_workspace_id
    AND g.project_workspace_id=p.project_workspace_id
   JOIN projects pr ON pr.id=g.project_id AND pr.status='active'
   JOIN workspaces tenant ON tenant.id=pr.workspace_id
    AND tenant.status='active'
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=pr.id AND w.status='active'
   JOIN principals human ON human.id=p.approved_by
    AND human.status='active' AND human.principal_type='human'
   JOIN workspace_memberships member ON member.principal_id=human.id
    AND member.workspace_id=tenant.id AND member.status='active'
   WHERE a.project_id=? AND a.goal_id=? AND a.source_id=?
    AND a.claimed_at BETWEEN ? AND ?
   ORDER BY a.claimed_at DESC,a.id DESC LIMIT 1`,
   now,planHash,projectID,goalID,source.SourceID,now-freshnessMS,now).
   Scan(&tcpID,&tcpPermitID,&tcpStatus,&tcpClaimedAt,&tcpAt)
  if err!=nil&&!errors.Is(err,sql.ErrNoRows){return GoalSignalTriage{},err}
  if err==nil{
   item.TCPAttemptID=tcpID
   switch {
   case tcpStatus=="claimed"||tcpStatus=="cancelled":
    // A newer interrupted probe invalidates any older impression of
    // current reachability. It is not a negative TCP measurement.
    item.TCPState="unknown_after_interruption"
   case tcpAt<tcpClaimedAt||tcpAt>now||tcpAt==0:
    item.TCPState="unknown_after_interruption"
   case tcpStatus=="connected"||tcpStatus=="not_connected"||tcpStatus=="timed_out":
    item.TCPState=tcpStatus;item.TCPObservedAt=tcpAt;seenTCP=true
    if tcpStatus=="not_connected"||tcpStatus=="timed_out"{
     item.NegativeIndicator=true;out.RecentTCPNegativeSources++
    }
   default:
    item.TCPState="unknown_after_interruption"
   }
   // Identity is an OPERATOR-DECLARED endpoint association, not a
   // physical-device attestation. A pre-binding probe must never inherit
   // the identity retroactively even if its IP and port happen to match.
   if item.TCPObservedAt>0{
    var targetID,targetClass string
    bindErr:=s.db.QueryRowContext(ctx,`SELECT declared.id,declared.identity_class
     FROM project_goal_tcp_declared_target_bindings declared
     JOIN project_goal_tcp_probe_permits p ON p.id=declared.permit_id
      AND p.project_id=declared.project_id AND p.goal_id=declared.goal_id
      AND p.source_id=declared.source_id AND p.status='approved'
      AND p.expires_at>?
     JOIN projects project ON project.id=declared.project_id
      AND project.status='active'
     JOIN principals reviewer ON reviewer.id=declared.approved_by
      AND reviewer.status='active' AND reviewer.principal_type='human'
     JOIN workspace_memberships membership
      ON membership.workspace_id=project.workspace_id
      AND membership.principal_id=reviewer.id AND membership.status='active'
     WHERE declared.project_id=? AND declared.goal_id=?
      AND declared.source_id=? AND declared.permit_id=?
      AND declared.status='approved' AND declared.expires_at>?
      AND declared.approved_at<?
      AND declared.plan_hash=? AND declared.plan_hash=p.plan_hash
      AND declared.permit_revision=p.revision
      AND declared.project_workspace_id=p.project_workspace_id
      AND declared.target_ipv4=p.target_ipv4 AND declared.target_port=p.target_port
     ORDER BY declared.approved_at DESC LIMIT 1`,
     now,projectID,goalID,source.SourceID,tcpPermitID,now,
     tcpClaimedAt,planHash).Scan(&targetID,&targetClass)
    if bindErr!=nil&&!errors.Is(bindErr,sql.ErrNoRows){
     return GoalSignalTriage{},bindErr
    }
    if bindErr==nil{
     item.TCPDeclaredTargetID=targetID
     item.TCPIdentityClass=targetClass
    }
   }
  }
  // Separately approved certificate possession is not evidence of a
  // physical device's identity and never casts an incident "vote". Use
  // exactly the current PIN/target/permit/Goal plan and all live reviewers.
  // The most recent check includes a claimed/incomplete attempt so an
  // earlier matched handshake cannot appear current during a newer probe.
  var tlsCheckID,tlsPinID,tlsStatus string
  var tlsClaimedAt,tlsCompletedAt int64
  err=s.db.QueryRowContext(ctx,`SELECT attempt.id,attempt.pin_id,
   attempt.status,attempt.claimed_at,attempt.completed_at
   FROM project_goal_tls_endpoint_checks attempt
   JOIN project_goal_tls_endpoint_pins pin ON pin.id=attempt.pin_id
    AND pin.project_id=attempt.project_id AND pin.goal_id=attempt.goal_id
    AND pin.status='approved' AND pin.expires_at>?
   JOIN project_goal_tcp_declared_target_bindings declared
    ON declared.id=pin.declared_target_id AND declared.project_id=pin.project_id
     AND declared.goal_id=pin.goal_id AND declared.permit_id=pin.permit_id
     AND declared.status='approved' AND declared.expires_at>?
     AND declared.permit_revision=pin.permit_revision
     AND declared.plan_hash=pin.plan_hash
   JOIN project_goal_tcp_probe_permits permit ON permit.id=pin.permit_id
    AND permit.project_id=pin.project_id AND permit.goal_id=pin.goal_id
    AND permit.status='approved' AND permit.expires_at>?
    AND permit.revision=pin.permit_revision AND permit.plan_hash=?
    AND permit.source_id=declared.source_id
    AND permit.target_ipv4=declared.target_ipv4
    AND permit.target_port=declared.target_port
    AND permit.project_workspace_id=declared.project_workspace_id
   JOIN projects p ON p.id=pin.project_id AND p.status='active'
   JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
   JOIN project_continuous_goals goal ON goal.id=pin.goal_id
    AND goal.project_id=p.id AND goal.status='configured'
    AND goal.project_workspace_id=pin.project_workspace_id
   JOIN project_workspaces workspace ON workspace.id=goal.project_workspace_id
    AND workspace.project_id=p.id AND workspace.status='active'
   JOIN principals pin_review ON pin_review.id=pin.approved_by
    AND pin_review.status='active' AND pin_review.principal_type='human'
   JOIN workspace_memberships pin_member ON pin_member.principal_id=pin_review.id
    AND pin_member.workspace_id=p.workspace_id AND pin_member.status='active'
   JOIN principals target_review ON target_review.id=declared.approved_by
    AND target_review.status='active' AND target_review.principal_type='human'
   JOIN workspace_memberships target_member ON target_member.principal_id=target_review.id
    AND target_member.workspace_id=p.workspace_id AND target_member.status='active'
   JOIN principals permit_review ON permit_review.id=permit.approved_by
    AND permit_review.status='active' AND permit_review.principal_type='human'
   JOIN workspace_memberships permit_member ON permit_member.principal_id=permit_review.id
    AND permit_member.workspace_id=p.workspace_id AND permit_member.status='active'
   WHERE attempt.project_id=? AND attempt.goal_id=?
    AND declared.source_id=? AND attempt.claimed_at>=pin.approved_at
    AND attempt.claimed_at BETWEEN ? AND ?
   ORDER BY attempt.claimed_at DESC,attempt.id DESC LIMIT 1`,
   now,now,now,planHash,projectID,goalID,source.SourceID,
   now-freshnessMS,now).
   Scan(&tlsCheckID,&tlsPinID,&tlsStatus,&tlsClaimedAt,&tlsCompletedAt)
  if err!=nil&&!errors.Is(err,sql.ErrNoRows){return GoalSignalTriage{},err}
  if err==nil{
   item.TLSCheckID=tlsCheckID;item.TLSPinID=tlsPinID
   item.TLSIdentityClass="operator_pinned_tls_certificate"
   if tlsStatus=="claimed"||tlsCompletedAt<tlsClaimedAt||tlsCompletedAt>now{
    item.TLSCheckState="unknown_after_interruption"
   }else{
    item.TLSCheckState=tlsStatus
    item.TLSCertificatePossessionMatched=(tlsStatus=="matched")
   }
  }
  // A signed offline instrument challenge is cryptographic possession
  // of a separately human-enrolled KEY. It is not a measurement from a
  // specific device and adds NO vote to incident classification. All
  // original human grants must still be live and target/plan-bound.
  var instrumentProofID,instrumentKeyID,instrumentProofClass string
  err=s.db.QueryRowContext(ctx,`SELECT proof.id,proof.instrument_key_id,proof.proof_class
   FROM project_goal_instrument_proofs proof
   JOIN project_goal_instrument_keys ik ON ik.id=proof.instrument_key_id
    AND ik.project_id=proof.project_id AND ik.goal_id=proof.goal_id
    AND ik.status='approved' AND ik.expires_at>?
   JOIN project_goal_tls_endpoint_pins pin ON pin.id=ik.tls_pin_id
    AND pin.project_id=ik.project_id AND pin.goal_id=ik.goal_id
    AND pin.status='approved' AND pin.expires_at>?
    AND pin.declared_target_id=ik.declared_target_id
    AND pin.plan_hash=ik.plan_hash
   JOIN project_goal_tcp_declared_target_bindings target
    ON target.id=ik.declared_target_id AND target.project_id=ik.project_id
    AND target.goal_id=ik.goal_id AND target.status='approved'
    AND target.expires_at>? AND target.permit_id=pin.permit_id
    AND target.plan_hash=ik.plan_hash
    AND target.permit_revision=pin.permit_revision
   JOIN project_goal_tcp_probe_permits permit
    ON permit.id=target.permit_id AND permit.project_id=ik.project_id
    AND permit.goal_id=ik.goal_id AND permit.status='approved'
    AND permit.expires_at>? AND permit.revision=target.permit_revision
    AND permit.plan_hash=? AND permit.target_ipv4=target.target_ipv4
    AND permit.target_port=target.target_port
    AND permit.source_id=target.source_id
   JOIN project_continuous_goals goal ON goal.id=ik.goal_id
    AND goal.project_id=ik.project_id AND goal.status='configured'
    AND goal.project_workspace_id=ik.project_workspace_id
    AND goal.project_workspace_id=target.project_workspace_id
    AND goal.project_workspace_id=pin.project_workspace_id
   JOIN projects p ON p.id=ik.project_id AND p.status='active'
   JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
   JOIN project_workspaces pw ON pw.id=goal.project_workspace_id
    AND pw.project_id=p.id AND pw.status='active'
   JOIN principals ki ON ki.id=ik.approved_by
    AND ki.principal_type='human' AND ki.status='active'
   JOIN workspace_memberships km ON km.principal_id=ki.id
    AND km.workspace_id=p.workspace_id AND km.status='active'
   JOIN principals pi ON pi.id=pin.approved_by
    AND pi.principal_type='human' AND pi.status='active'
   JOIN workspace_memberships pm ON pm.principal_id=pi.id
    AND pm.workspace_id=p.workspace_id AND pm.status='active'
   JOIN principals ti ON ti.id=target.approved_by
    AND ti.principal_type='human' AND ti.status='active'
   JOIN workspace_memberships tm ON tm.principal_id=ti.id
    AND tm.workspace_id=p.workspace_id AND tm.status='active'
   JOIN principals ri ON ri.id=permit.approved_by
    AND ri.principal_type='human' AND ri.status='active'
   JOIN workspace_memberships rm ON rm.principal_id=ri.id
    AND rm.workspace_id=p.workspace_id AND rm.status='active'
   WHERE proof.project_id=? AND proof.goal_id=?
    AND target.source_id=? AND proof.accepted_at BETWEEN ? AND ?
   ORDER BY proof.accepted_at DESC,proof.id DESC LIMIT 1`,
   now,now,now,now,planHash,
   projectID,goalID,source.SourceID,now-freshnessMS,now).
   Scan(&instrumentProofID,&instrumentKeyID,&instrumentProofClass)
  if err!=nil&&!errors.Is(err,sql.ErrNoRows){return GoalSignalTriage{},err}
  if err==nil{
   item.InstrumentProofID=instrumentProofID
   item.InstrumentKeyID=instrumentKeyID
   item.InstrumentProofClass=instrumentProofClass
   item.InstrumentProofState="recent_signed_challenge"
   item.InstrumentKeyPossessionProven=true
  }
  // Only report a live, correctly pinned TLS source with a human
  // approver whose membership remains active. Historical receipts
  // remain in audit history, but revocation prevents current triage.
  var nodeID,receiptID,nodeOutcome string
  var nodeAt int64
  err=s.db.QueryRowContext(ctx,`SELECT r.id,r.outcome,r.observed_at,r.peer_node_id
   FROM project_goal_node_measurement_receipts r
   JOIN project_goal_node_measurement_grants grant
    ON grant.id=r.grant_id AND grant.goal_id=r.goal_id
     AND grant.project_id=r.project_id AND grant.source_id=r.source_id
     AND grant.project_workspace_id=r.project_workspace_id
     AND grant.peer_node_id=r.peer_node_id
     AND grant.status='approved' AND grant.expires_at>?
     AND r.observed_at>=grant.approved_at
   JOIN project_continuous_goals g ON g.id=r.goal_id
    AND g.project_id=r.project_id AND g.status='configured'
    AND g.project_workspace_id=r.project_workspace_id
   JOIN projects p ON p.id=g.project_id AND p.status='active'
   JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=p.id AND w.status='active'
   JOIN principals approver ON approver.id=grant.approved_by
    AND approver.principal_type='human' AND approver.status='active'
   JOIN workspace_memberships member ON member.principal_id=approver.id
    AND member.workspace_id=p.workspace_id AND member.status='active'
   JOIN harness_nodes n ON n.id=r.peer_node_id AND n.local=0
    AND n.trust_state='paired'
   JOIN node_pairings pairing ON pairing.peer_node_id=n.id
    AND pairing.status='paired' AND pairing.local_confirmed=1
    AND pairing.peer_confirmed=1
    AND pairing.peer_tls_fingerprint=grant.peer_tls_fingerprint
   WHERE r.project_id=? AND r.goal_id=? AND r.source_id=?
    AND r.observed_at BETWEEN ? AND ?
   ORDER BY r.observed_at DESC,r.id DESC LIMIT 1`,
   now,projectID,goalID,source.SourceID,now-freshnessMS,now).
   Scan(&receiptID,&nodeOutcome,&nodeAt,&nodeID)
  if err!=nil&&!errors.Is(err,sql.ErrNoRows){return GoalSignalTriage{},err}
  if err==nil{
   item.NodeState=nodeOutcome;item.NodeReceiptID=receiptID
   item.NodeObservedAt=nodeAt;item.PeerNodeID=nodeID
   item.NodeIdentityClass="paired_node_transport_self_report";seenNode=true
   if nodeOutcome=="unreachable"{
    item.NegativeIndicator=true;out.RecentNodeNegativeSources++
   }
  }
  if item.NegativeIndicator{out.RecentNegativeSources++}
  out.Sources=append(out.Sources,item)
 }
 // Distinct source IDs prevent the same registered source from
 // manufacturing independent votes. These remain investigatory only.
 out.State=classifyGoalSignalTriage(out.RecentNegativeSources,
  out.RecentTCPNegativeSources,out.RecentNodeNegativeSources,
  seenTCP,seenNode)
 return out,nil
}

// Pure, narrow classifier: never infer a shared target, device trust or
// remediation permission from merely co-occurring connection reports.
func classifyGoalSignalTriage(negativeSources,tcpNeg,nodeNeg int,
 seenTCP,seenNode bool)string{
 switch{
 case negativeSources>=2&&tcpNeg>0&&nodeNeg>0:
  return "multiple_adverse_indicators_investigate"
 case negativeSources>0:
  return "adverse_indicator_needs_review"
 case seenTCP&&seenNode:
  return "mixed_classes_no_adverse_indicator"
 case seenTCP||seenNode:
  return "single_measurement_class"
 default:
  return "no_recent_measurements"
 }
}
