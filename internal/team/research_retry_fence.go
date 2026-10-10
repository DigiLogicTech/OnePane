package team

import (
 "context"
 "errors"

 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// Automatic Research retries are intentionally bounded. After two provider-
// timer retries the Council remains paused for explicit operator intervention.
const maxAutomaticResearchRetryAttempts = 2

var errResearchConsentNoLongerValid=errors.New("Research evidence consent or Project scope changed; create a new protected Council")

// No retry may convert a previously evidence-protected Council into an
// ordinary unprotected session, including after grant withdrawal, Team
// archiving, human membership revocation or a pinned manifest change.
// Called INSIDE the same write transaction as the pending turn transition.
func (s *Service) researchRetryConsentTx(ctx context.Context,tx storage.Tx,sessionID,tenant string)error{
 var existing int
 if err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM research_evidence_seat_approvals
  WHERE session_id=?`,sessionID).Scan(&existing);err!=nil{return err}
 if existing==0{return nil}
 var eligible int
 err:=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM research_evidence_seat_approvals approval
 JOIN research_session_evidence_metadata frozen ON frozen.session_id=approval.session_id
  AND frozen.workspace_id=approval.workspace_id
  AND frozen.project_id=approval.project_id
  AND frozen.project_workspace_id=approval.project_workspace_id
  AND frozen.research_manifest_sha256=approval.research_manifest_sha256
  AND frozen.evidence_manifest_sha256=approval.evidence_manifest_sha256
 JOIN team_sessions session ON session.id=approval.session_id
  AND session.workspace_id=approval.workspace_id
 JOIN team_session_manifests manifest ON manifest.session_id=session.id
  AND manifest.snapshot_sha256=approval.research_manifest_sha256
  AND manifest.research_mode=1 AND manifest.execution_mode='council'
 JOIN teams team ON team.id=session.team_id AND team.workspace_id=session.workspace_id
  AND team.status='active'
 JOIN tasks task ON task.id=session.task_id AND task.workspace_id=session.workspace_id
  AND task.project_id=approval.project_id
  AND task.project_workspace_id=approval.project_workspace_id
 JOIN projects project ON project.id=task.project_id
  AND project.workspace_id=session.workspace_id AND project.status='active'
 JOIN project_workspaces workspace ON workspace.id=task.project_workspace_id
  AND workspace.project_id=project.id AND workspace.status='active'
 JOIN workspaces tenant ON tenant.id=session.workspace_id AND tenant.status='active'
 JOIN principals human ON human.id=approval.approved_by
  AND human.principal_type='human' AND human.status='active'
 JOIN workspace_memberships membership ON membership.principal_id=human.id
  AND membership.workspace_id=session.workspace_id AND membership.status='active'
 WHERE approval.session_id=? AND approval.workspace_id=?
  AND approval.policy='local_only' AND approval.state='approved'`,
 sessionID,tenant).Scan(&eligible)
 if err!=nil{return err}
 if eligible!=1{return errResearchConsentNoLongerValid}
 return nil
}

func researchAutomaticRetryAllowed(total,eligible int)bool{
 // Every blocked/failed unresolved seat MUST have an explicit, elapsed
 // provider timer and be within the bounded automatic attempt budget.
 return total>0&&total==eligible
}
