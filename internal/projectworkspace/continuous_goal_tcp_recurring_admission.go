package projectworkspace

import (
 "context"
 "database/sql"
 "errors"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/storage"
)

const GoalTCPRecurringWorkerPrincipal="system:tcp-recurring-observer"

// authorizeRecurringTCPClaimTx MUST be called inside the same SQLite writer
// transaction that inserts an immutable TCP attempt claim. It rejects all
// mismatches between this independent unattended-grant and the manual exact
// target permission from which it was explicitly derived.
func(s *Service)authorizeRecurringTCPClaimTx(ctx context.Context,tx storage.Tx,
 grantID,projectID,goalID,workspaceID string,manual GoalTCPProbePermit,
 planHash string,cadence,now int64)error{
 grant,err:=scanRecurringGrant(tx.QueryRowContext(ctx,`SELECT `+recurringGrantColumns+
  ` FROM project_goal_tcp_recurring_grants
  WHERE id=? AND project_id=? AND goal_id=?`,grantID,projectID,goalID))
 if errors.Is(err,sql.ErrNoRows){return ErrInvalidTransition}
 if err!=nil{return err}
 if grant.Status!="approved"||grant.ExpiresAt<=now||
  grant.ManualPermitID!=manual.ID||
  grant.ManualPermitRevision!=manual.Revision||
  grant.ProjectWorkspaceID!=workspaceID||grant.PlanHash!=planHash||
  grant.SourceID!=manual.SourceID||
  grant.TargetIPv4!=manual.TargetIPv4||
  grant.TargetPort!=manual.TargetPort||
  grant.TimeoutMS!=manual.TimeoutMS||
  grant.CadenceMinutes!=cadence||
  cadence<15||cadence>10080||
  grant.EffectClass!="recurring_tcp_connect"{
  return ErrInvalidTransition
 }
 for _,principal:=range []string{grant.ApprovedBy,manual.ApprovedBy}{
  var active int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals p
    JOIN projects project ON project.id=?
    JOIN workspace_memberships member ON member.workspace_id=project.workspace_id
     AND member.principal_id=p.id AND member.status='active'
    WHERE p.id=? AND p.principal_type='human' AND p.status='active'`,
    projectID,principal).Scan(&active)
  if errors.Is(err,sql.ErrNoRows){return ErrPrincipalIneligible}
  if err!=nil{return err}
 }
 var principalType string
 err=tx.QueryRowContext(ctx,`SELECT principal_type FROM principals WHERE
  id=? AND status='active'`,GoalTCPRecurringWorkerPrincipal).Scan(&principalType)
 if err!=nil{return err}
 if principalType!="system"{
  return fmt.Errorf("%w: recurring TCP worker must be a system principal",ErrPrincipalIneligible)
 }
 return nil
}

// tcpRecurringClaimStillAuthorised rechecks source, tenant, both human
// approvals, grant status/expiry and exact TCP scope at the last reasonable
// admission boundary before DialContext. This is not an active health proof.
func(s *Service)tcpRecurringClaimStillAuthorised(ctx context.Context,
 a GoalTCPProbeAttempt,grantID string)(bool,error){
 if a.RequestedBy!=GoalTCPRecurringWorkerPrincipal||a.RecurringGrantID==nil||
  *a.RecurringGrantID!=grantID{return false,nil}
 valid,err:=s.TCPRecurringGrantAllowed(ctx,grantID)
 if err!=nil||!valid{return false,err}
 grant,err:=scanRecurringGrant(s.db.QueryRowContext(ctx,`SELECT `+recurringGrantColumns+
  ` FROM project_goal_tcp_recurring_grants WHERE id=? AND goal_id=?
   AND project_id=?`,grantID,a.GoalID,a.ProjectID))
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 if grant.ManualPermitID!=a.PermitID||
  grant.ManualPermitRevision!=a.PermitRevision||
  grant.ProjectWorkspaceID!=a.ProjectWorkspaceID||
  grant.SourceID!=a.SourceID||
  grant.TargetIPv4!=a.TargetIPv4||
  grant.TargetPort!=a.TargetPort||
  grant.TimeoutMS!=a.TimeoutMS{
  return false,nil
 }
 var revision int64
 var status string
 err=s.db.QueryRowContext(ctx,`SELECT revision,status FROM project_continuous_goals
  WHERE id=? AND project_id=?`,a.GoalID,a.ProjectID).Scan(&revision,&status)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 return revision==a.GoalRevision&&status=="configured",nil
}
