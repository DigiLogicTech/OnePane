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
 Limitations []string `json:"limitations"`
}
type GoalSignalTriageSource struct {
 SourceID string `json:"source_id"`
 TCPState string `json:"tcp_state"`
 TCPObservedAt int64 `json:"tcp_observed_at,omitempty"`
 TCPAttemptID string `json:"tcp_attempt_id,omitempty"`
 NodeState string `json:"node_state"`
 NodeObservedAt int64 `json:"node_observed_at,omitempty"`
 NodeReceiptID string `json:"node_receipt_id,omitempty"`
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
 now:=s.clock.UnixMilli()
 // Conservative bounded freshness even if a Goal is configured to
 // run weekly. Older telemetry must not trigger a contemporary alert.
 const freshnessMS int64=15*60*1000
 out:=GoalSignalTriage{
  ProjectID:projectID,GoalID:goalID,GoalRevision:coverage.GoalRevision,
  AssessedAt:now,SignalFreshnessMinutes:15,
  State:"no_recent_measurements",
  Sources:make([]GoalSignalTriageSource,0,len(coverage.Sources)),
  Limitations:[]string{
   "tcp_connect_tests_reachability_not_device_security",
   "node_peer_connectivity_is_self_report_not_physical_device_attestation",
   "different_sources_may_not_measure_same_target",
   "read_only_no_automated_monitoring_or_remediation",
  },
 }
 seenTCP,seenNode:=false,false
 for _,source:=range coverage.Sources{
  item:=GoalSignalTriageSource{
   SourceID:source.SourceID,TCPState:"no_current_approved_measurement",
   NodeState:"no_current_approved_measurement",
  }
  // Bound each query to ONE last completed TCP result for this exact Goal
  // source. No target IP, port, raw receipt, socket or secret is returned.
  var tcpStatus string
  var tcpID string
  var tcpAt int64
  err=s.db.QueryRowContext(ctx,`SELECT a.id,a.status,a.completed_at
   FROM project_goal_tcp_probe_attempts a
   JOIN project_goal_tcp_probe_permits p ON p.id=a.permit_id
     AND p.goal_id=a.goal_id AND p.project_id=a.project_id
     AND p.source_id=a.source_id AND p.status='approved'
     AND p.expires_at>?
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
    AND a.status IN ('connected','not_connected','timed_out')
    AND a.completed_at BETWEEN ? AND ?
   ORDER BY a.completed_at DESC,a.id DESC LIMIT 1`,
   now,projectID,goalID,source.SourceID,now-freshnessMS,now).
   Scan(&tcpID,&tcpStatus,&tcpAt)
  if err!=nil&&!errors.Is(err,sql.ErrNoRows){return GoalSignalTriage{},err}
  if err==nil{
   item.TCPState=tcpStatus;item.TCPAttemptID=tcpID
   item.TCPObservedAt=tcpAt;seenTCP=true
   if tcpStatus=="not_connected"||tcpStatus=="timed_out"{
    item.NegativeIndicator=true;out.RecentTCPNegativeSources++
   }
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
   item.NodeObservedAt=nodeAt;item.PeerNodeID=nodeID;seenNode=true
   if nodeOutcome=="unreachable"{
    item.NegativeIndicator=true;out.RecentNodeNegativeSources++
   }
  }
  if item.NegativeIndicator{out.RecentNegativeSources++}
  out.Sources=append(out.Sources,item)
 }
 // Distinct source IDs prevent the same registered source from
 // manufacturing independent votes. These remain investigatory only.
 switch {
 case out.RecentNegativeSources>=2&&seenNode&&seenTCP:
  out.State="multiple_adverse_indicators_investigate"
 case out.RecentNegativeSources>0:
  out.State="adverse_indicator_needs_review"
 case seenNode&&seenTCP:
  out.State="mixed_classes_no_adverse_indicator"
 case seenNode||seenTCP:
  out.State="single_measurement_class"
 }
 return out,nil
}
