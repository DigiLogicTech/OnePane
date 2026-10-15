package projectworkspace

import (
 "context"
 "database/sql"
 "strings"
)

// GoalRetentionInventory accounts for scoped historical records without
// reading raw observations, credentials, endpoints or Tool inputs.
// Counts are not disk-byte measurements; SQLite pages are database-wide.
type GoalRetentionInventory struct {
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 TCPAttempts int64 `json:"tcp_attempts"`
 TCPPermits int64 `json:"tcp_permits"`
 TCPRecurringGrants int64 `json:"tcp_recurring_grants"`
 TCPReachabilityAlerts int64 `json:"tcp_reachability_alerts"`
 IncidentReviews int64 `json:"incident_reviews"`
 ResponseCandidates int64 `json:"response_candidates"`
 // Count metadata only. The immutable source-signed receipts remain in SQLite
 // and may consume significant disk on long-running resource-limited hosts.
 SignedInstrumentClaims int64 `json:"signed_instrument_claims"`
 ObserverFenceEvents int64 `json:"observer_fence_events"`
 UnreconciledObserverOperations int64 `json:"unreconciled_observer_operations"`
 InFlightObserverOperations int64 `json:"in_flight_observer_operations"`
 CurrentFenceStatus string `json:"current_fence_status"`
 CurrentFenceRevision int64 `json:"current_fence_revision"`
 RetentionPolicy string `json:"retention_policy"`
 StorageAttentionReasons []string `json:"storage_attention_reasons"`
 BytesMeasured bool `json:"bytes_measured"`
 AutomaticPruningEnabled bool `json:"automatic_pruning_enabled"`
}

func(s *Service)GoalRetentionInventory(ctx context.Context,projectID,goalID string)(GoalRetentionInventory,error){
 projectID=strings.TrimSpace(projectID);goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return GoalRetentionInventory{},ErrInvalidCommand}
 // All reads use one SQLite read snapshot, not a potentially inconsistent
 // series of concurrent commits. No full history is copied into Go RAM.
 tx,err:=s.db.BeginTx(ctx,&sql.TxOptions{ReadOnly:true})
 if err!=nil{return GoalRetentionInventory{},err}
 defer tx.Rollback()
 out:=GoalRetentionInventory{ProjectID:projectID,GoalID:goalID,
  RetentionPolicy:"append_only_operator_review",
  StorageAttentionReasons:[]string{},BytesMeasured:false,
  AutomaticPruningEnabled:false,CurrentFenceStatus:"running"}
 err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id
  FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&out.ProjectWorkspaceID)
 if err!=nil{return GoalRetentionInventory{},err}
 // Constant query count and fixed table names prevent adversarial filters,
 // unbounded materialisation and access to another Project's records.
 tally:=[]struct{
  query string
  target *int64
 }{
  {`SELECT COUNT(*) FROM project_goal_tcp_probe_attempts
    WHERE project_id=? AND goal_id=? AND project_workspace_id=?`,&out.TCPAttempts},
  {`SELECT COUNT(*) FROM project_goal_tcp_probe_permits
    WHERE project_id=? AND goal_id=? AND project_workspace_id=?`,&out.TCPPermits},
  {`SELECT COUNT(*) FROM project_goal_tcp_recurring_grants
    WHERE project_id=? AND goal_id=? AND project_workspace_id=?`,&out.TCPRecurringGrants},
  {`SELECT COUNT(*) FROM project_goal_tcp_reachability_alerts
    WHERE project_id=? AND goal_id=? AND project_workspace_id=?`,&out.TCPReachabilityAlerts},
  {`SELECT COUNT(*) FROM project_goal_incident_reviews
    WHERE project_id=? AND goal_id=? AND project_workspace_id=?`,&out.IncidentReviews},
  {`SELECT COUNT(*) FROM project_goal_response_candidates
    WHERE project_id=? AND goal_id=? AND project_workspace_id=?`,&out.ResponseCandidates},
  {`SELECT COUNT(*) FROM project_goal_instrument_measurement_claims m
    JOIN project_goal_instrument_keys k ON k.id=m.instrument_key_id
     AND k.project_id=m.project_id AND k.goal_id=m.goal_id
    WHERE m.project_id=? AND m.goal_id=?
     AND k.project_workspace_id=?`,&out.SignedInstrumentClaims},
 }
 for _,v:=range tally{
  if err=tx.QueryRowContext(ctx,v.query,
   projectID,goalID,out.ProjectWorkspaceID).Scan(v.target);err!=nil{
   return GoalRetentionInventory{},err
  }
 }
 // Event sequence and payloads remain immutable; the event store is not
 // vacuumed, compacted or modified to satisfy this advisory endpoint.
 err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM events
  WHERE aggregate_type='project_goal_tcp_observation_fence'
   AND aggregate_id=?`,goalID).Scan(&out.ObserverFenceEvents)
 if err!=nil{return GoalRetentionInventory{},err}
 err=tx.QueryRowContext(ctx,`SELECT status,revision FROM
   project_goal_tcp_observation_fences
   WHERE project_id=? AND goal_id=? AND project_workspace_id=?`,
  projectID,goalID,out.ProjectWorkspaceID).
  Scan(&out.CurrentFenceStatus,&out.CurrentFenceRevision)
 if err!=nil && err!=sql.ErrNoRows{return GoalRetentionInventory{},err}
 // Unknown external outcomes are NOT inferred from mere connectivity.
 // Count durable uncertain/failed-compensation states separately from
 // routine transient phases that may still be executing successfully.
 // Startup recovery can later quarantine a transient crashed phase.
 resourceRef:="goal-tcp-observer://"+projectID+"/"+goalID
 err=tx.QueryRowContext(ctx,`SELECT
    COALESCE(SUM(CASE WHEN op.state IN (
      'unknown_outcome','blocked_unknown_outcome','compensation_failed')
      THEN 1 ELSE 0 END),0),
    COALESCE(SUM(CASE WHEN op.state IN (
      'executing','observing','verified','compensating')
      THEN 1 ELSE 0 END),0)
  FROM operations op
  JOIN tasks task ON task.id=op.task_id
   AND task.workspace_id=op.workspace_id
   AND task.project_id=? AND task.project_workspace_id=?
  WHERE op.workspace_id=(SELECT workspace_id FROM projects WHERE id=?)
   AND op.resource_ref=?
   AND op.tool_id IN ('project.goal.tcp_observer.pause',
    'project.goal.tcp_observer.resume')
   AND op.adapter_id='onepane.goal.tcp_observer.control'
   AND op.state IN ('executing','observing','verified',
    'unknown_outcome','blocked_unknown_outcome','compensating',
    'compensation_failed')`,
   projectID,out.ProjectWorkspaceID,projectID,resourceRef).
   Scan(&out.UnreconciledObserverOperations,&out.InFlightObserverOperations)
 if err!=nil{return GoalRetentionInventory{},err}
 if out.UnreconciledObserverOperations>0{
  out.StorageAttentionReasons=append(out.StorageAttentionReasons,
   "observer_mutation_needs_reconciliation")
 }
 if out.ObserverFenceEvents>=1024{
  out.StorageAttentionReasons=append(out.StorageAttentionReasons,"observer_audit_growth")
 }
 if out.TCPAttempts>=10000{
  out.StorageAttentionReasons=append(out.StorageAttentionReasons,"tcp_attempt_history_growth")
 }
 if out.IncidentReviews>=1000||out.ResponseCandidates>=1000{
  out.StorageAttentionReasons=append(out.StorageAttentionReasons,"incident_review_history_growth")
 }
 if out.SignedInstrumentClaims>=1024{
  out.StorageAttentionReasons=append(out.StorageAttentionReasons,
   "signed_instrument_claim_history_growth")
 }
 if err=tx.Commit();err!=nil{return GoalRetentionInventory{},err}
 return out,nil
}
