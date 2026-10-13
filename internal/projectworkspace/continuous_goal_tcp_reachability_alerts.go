package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// TCPReachabilityAlert is a bounded, evidence-backed notice that an exact
// TCP handshake deviated from the baseline which existed at admission.
// It is NOT an incident diagnosis, verified device health or an authority
// to change host/network configuration.
type TCPReachabilityAlert struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 SourceID string `json:"source_id"`
 BaselineID string `json:"baseline_id"`
 BaselineRevision int64 `json:"baseline_revision"`
 AttemptID string `json:"attempt_id"`
 ExpectedTCP string `json:"expected_tcp"`
 ObservedTCP string `json:"observed_tcp"`
 Classification string `json:"classification"`
 Status string `json:"status"`
 Revision int64 `json:"revision"`
 DetectedBy string `json:"detected_by"`
 DetectedAt int64 `json:"detected_at"`
 AcknowledgedBy *string `json:"acknowledged_by,omitempty"`
 AcknowledgedAt int64 `json:"acknowledged_at"`
 DeviceHealth string `json:"device_health"`
 // A later approved TCP attempt recorded the same baseline expectation.
 // This is historical socket evidence, NEVER verified device health,
 // current reachability or confirmation that an incident was resolved.
 ReturnToBaselineEvidenceState string `json:"return_to_baseline_evidence_state"`
 ReturnToBaselineAttemptID string `json:"return_to_baseline_attempt_id,omitempty"`
 ReturnToBaselineAt int64 `json:"return_to_baseline_at,omitempty"`
}
type AcknowledgeTCPReachabilityAlertCommand struct {
 ProjectID string
 GoalID string
 AlertID string
 ExpectedRevision int64
 ActorPrincipalID string
}
const tcpReachabilityAlertColumns=`id,project_id,goal_id,project_workspace_id,source_id,
 baseline_id,baseline_revision,attempt_id,expected_tcp,observed_tcp,
 classification,status,revision,detected_by,detected_at,
 acknowledged_by,acknowledged_at`

func scanTCPReachabilityAlert(r scanner)(TCPReachabilityAlert,error){
 var a TCPReachabilityAlert
 var actor sql.NullString
 err:=r.Scan(&a.ID,&a.ProjectID,&a.GoalID,&a.ProjectWorkspaceID,
  &a.SourceID,&a.BaselineID,&a.BaselineRevision,&a.AttemptID,
  &a.ExpectedTCP,&a.ObservedTCP,&a.Classification,&a.Status,
  &a.Revision,&a.DetectedBy,&a.DetectedAt,&actor,&a.AcknowledgedAt)
 if actor.Valid{a.AcknowledgedBy=&actor.String}
 a.DeviceHealth="unverified"
 return a,err
}

// Called ONLY from the writer transaction which finalises the immutable
// TCP attempt. A baseline revised after the socket was claimed cannot
// retroactively generate a drift notice against a different expectation.
func(s *Service)recordTCPReachabilityAlertTx(ctx context.Context,tx storage.Tx,
 attempt GoalTCPProbeAttempt,status string,now int64,tenantWorkspaceID string)error{
 if status!="connected"&&status!="not_connected"{return nil}
 if attempt.BaselineIDAtClaim==""||attempt.BaselineRevisionAtClaim<1{return nil}
 var expected string
 err:=tx.QueryRowContext(ctx,`SELECT baseline.expected_tcp
  FROM project_goal_tcp_baseline_heads head
  JOIN project_goal_tcp_baseline_revisions baseline ON baseline.id=head.baseline_id
  JOIN project_goal_tcp_probe_permits permit ON permit.id=baseline.permit_id
   AND permit.project_id=baseline.project_id AND permit.goal_id=baseline.goal_id
   AND permit.status='approved' AND permit.expires_at>?
   AND permit.plan_generation>0
   AND permit.plan_hash=baseline.plan_hash
  JOIN project_continuous_goals goal ON goal.id=baseline.goal_id
   AND goal.project_id=baseline.project_id AND goal.status='configured'
   AND goal.project_workspace_id=baseline.project_workspace_id
   AND goal.plan_generation=permit.plan_generation
  WHERE head.goal_id=? AND head.source_id=?
   AND head.baseline_id=? AND head.revision=?
   AND baseline.project_id=? AND baseline.project_workspace_id=?
   AND baseline.permit_id=?`,
  now,attempt.GoalID,attempt.SourceID,attempt.BaselineIDAtClaim,
  attempt.BaselineRevisionAtClaim,attempt.ProjectID,
  attempt.ProjectWorkspaceID,attempt.PermitID).Scan(&expected)
 if errors.Is(err,sql.ErrNoRows){return nil}
 if err!=nil{return err}
 if expected==status{return nil}
 classification:="unexpected_unreachable"
 if status=="connected"{classification="unexpected_reachable"}
 alertID,err:=s.ids.New("talert");if err!=nil{return err}
 changed,err:=tx.ExecContext(ctx,`INSERT OR IGNORE INTO project_goal_tcp_reachability_alerts(
    id,project_id,goal_id,project_workspace_id,source_id,
    baseline_id,baseline_revision,attempt_id,expected_tcp,observed_tcp,
    classification,status,revision,detected_by,detected_at)
    VALUES(?,?,?,?,?,?,?,?,?,?,?,'open',1,?,?)`,
  alertID,attempt.ProjectID,attempt.GoalID,attempt.ProjectWorkspaceID,
  attempt.SourceID,attempt.BaselineIDAtClaim,attempt.BaselineRevisionAtClaim,
  attempt.ID,expected,status,classification,attempt.RequestedBy,now)
 if err!=nil{return err}
 n,err:=changed.RowsAffected();if err!=nil{return err}
 if n==0{return nil}
 evtID,err:=s.ids.New("evt");if err!=nil{return err}
 payload,_:=json.Marshal(map[string]any{
  "alert_id":alertID,"goal_id":attempt.GoalID,"source_id":attempt.SourceID,
  "baseline_id":attempt.BaselineIDAtClaim,
  "baseline_revision":attempt.BaselineRevisionAtClaim,
  "attempt_id":attempt.ID,"classification":classification,
  "device_health":"unverified","remediation_executed":false,
 })
 return s.events.Append(ctx,tx,event.Event{
  ID:evtID,WorkspaceID:&tenantWorkspaceID,
  Type:"project.goal_tcp_reachability_notice",
  AggregateType:"project_goal_tcp_reachability_alert",
  AggregateID:alertID,ActorPrincipalID:&attempt.RequestedBy,
  Payload:payload,OccurredAt:now,
 })
}

func(s *Service)ListTCPReachabilityAlerts(ctx context.Context,projectID,goalID string)([]TCPReachabilityAlert,error){
 projectID=strings.TrimSpace(projectID);goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var owned int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&owned)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+tcpReachabilityAlertColumns+
  ` FROM project_goal_tcp_reachability_alerts
  WHERE project_id=? AND goal_id=?
  ORDER BY detected_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]TCPReachabilityAlert{}
 for rows.Next(){
  a,err:=scanTCPReachabilityAlert(rows)
  if err!=nil{return nil,err}
  a.ReturnToBaselineEvidenceState="not_observed"
  // Historical evidence is correlated only with the exact original
  // permit, its approved target, and the immutable baseline version.
  // This deliberately does NOT read current network status, generate
  // a remediation, or change the alert's open/acknowledged status.
  var returnID string
  var returnAt int64
  recoveryErr:=s.db.QueryRowContext(ctx,`SELECT later.id,later.completed_at
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
    AND later.completed_at>=later.claimed_at
    AND later.completed_at<=?
   ORDER BY later.claimed_at ASC,later.id ASC LIMIT 1`,
   a.AttemptID,a.ProjectID,a.GoalID,a.ProjectWorkspaceID,a.SourceID,
   a.ProjectID,a.GoalID,a.ProjectWorkspaceID,a.SourceID,
   a.BaselineID,a.BaselineRevision,a.ExpectedTCP,a.DetectedAt,
   s.clock.UnixMilli()).Scan(&returnID,&returnAt)
  if recoveryErr==nil{
   a.ReturnToBaselineEvidenceState="historical_tcp_return_observed"
   a.ReturnToBaselineAttemptID=returnID
   a.ReturnToBaselineAt=returnAt
  }else if !errors.Is(recoveryErr,sql.ErrNoRows){
   return nil,recoveryErr
  }
  out=append(out,a)
 }
 return out,rows.Err()
}

func(s *Service)AcknowledgeTCPReachabilityAlert(ctx context.Context,
 c AcknowledgeTCPReachabilityAlertCommand)(TCPReachabilityAlert,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID);c.GoalID=strings.TrimSpace(c.GoalID)
 c.AlertID=strings.TrimSpace(c.AlertID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.AlertID==""||
  c.ActorPrincipalID==""||c.ExpectedRevision<1{
  return TCPReachabilityAlert{},ErrInvalidCommand
 }
 evtID,err:=s.ids.New("evt");if err!=nil{return TCPReachabilityAlert{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var validGoal int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
     AND w.project_id=g.project_id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).Scan(&validGoal)
  if err!=nil{return err}
  a,err:=scanTCPReachabilityAlert(tx.QueryRowContext(ctx,`SELECT `+
   tcpReachabilityAlertColumns+` FROM project_goal_tcp_reachability_alerts
    WHERE id=? AND project_id=? AND goal_id=?`,c.AlertID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if a.Revision!=c.ExpectedRevision{return ErrRevisionConflict}
  if a.Status!="open"{return ErrInvalidTransition}
  now:=s.clock.UnixMilli()
  if now<a.DetectedAt{now=a.DetectedAt}
  changed,err:=tx.ExecContext(ctx,`UPDATE project_goal_tcp_reachability_alerts
   SET status='acknowledged',revision=revision+1,
    acknowledged_by=?,acknowledged_at=?
   WHERE id=? AND revision=? AND status='open'`,
   c.ActorPrincipalID,now,a.ID,a.Revision)
  if err!=nil{return err}
  n,err:=changed.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  payload,_:=json.Marshal(map[string]any{
   "alert_id":a.ID,"goal_id":c.GoalID,"source_id":a.SourceID,
   "device_health":"unverified","acknowledgement_is_not_remediation":true,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_tcp_reachability_notice_acknowledged",
   AggregateType:"project_goal_tcp_reachability_alert",
   AggregateID:a.ID,ActorPrincipalID:&c.ActorPrincipalID,
   Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return TCPReachabilityAlert{},err}
 return scanTCPReachabilityAlert(s.db.QueryRowContext(ctx,`SELECT `+
  tcpReachabilityAlertColumns+` FROM project_goal_tcp_reachability_alerts
  WHERE id=?`,c.AlertID))
}
