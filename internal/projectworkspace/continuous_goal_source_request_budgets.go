package projectworkspace

import (
 "context"
 "database/sql"
 "errors"

 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// Unlike successful proof/reading limits, this cap includes malformed and
// rejected source HTTP requests. A single machine bearer credential cannot
// consume unbounded CPU/SQLite verification work.
var ErrSourceRequestRateLimited=errors.New("source request attempt rate limit reached")

// ReserveGoalSourceRequest is invoked AFTER bearer authentication and
// Workspace capability check, but BEFORE JSON decoding or Observation reads.
// Both machine source routes share 60 attempts per fixed 60-second window.
// Attempts remain consumed even on malformed input or failed signatures.
func(s *Service)ReserveGoalSourceRequest(ctx context.Context,
 projectID,goalID,credentialID,principalID string)error{
 if projectID==""||goalID==""||credentialID==""||principalID==""{
  return ErrPrincipalIneligible
 }
 return s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  var tenant,signerID string
  err:=tx.QueryRowContext(ctx,`SELECT p.workspace_id,b.signer_id
   FROM project_goal_source_credential_grants b
   JOIN projects p ON p.id=b.project_id AND p.status='active'
   JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
   JOIN project_goal_observation_signers k ON k.id=b.signer_id
     AND k.goal_id=b.goal_id AND k.project_id=b.project_id
   JOIN project_continuous_goals g ON g.id=b.goal_id AND g.project_id=p.id
     AND g.status='configured'
   JOIN project_workspaces pw ON pw.id=b.project_workspace_id
     AND pw.project_id=p.id AND pw.status='active'
   JOIN api_credentials a ON a.id=b.credential_id
    AND a.principal_id=b.source_principal_id
   JOIN principals source ON source.id=b.source_principal_id
   JOIN workspace_memberships m ON m.principal_id=source.id
    AND m.workspace_id=p.workspace_id AND m.status='active'
   WHERE b.project_id=? AND b.goal_id=? AND b.credential_id=?
    AND b.source_principal_id=? AND b.workspace_id=p.workspace_id
    AND b.revoked_by IS NULL AND b.expires_at>?
    AND k.status='approved' AND k.expires_at>?
    AND a.revoked_at IS NULL AND a.expires_at>?
    AND a.workspace_scope_json=json_array(p.workspace_id)
    AND a.capability_scope_json='["goal.source.submit"]'
    AND source.status='active' AND source.principal_type IN ('agent','service')`,
   projectID,goalID,credentialID,principalID,now,now,now).
   Scan(&tenant,&signerID)
  if errors.Is(err,sql.ErrNoRows){return ErrPrincipalIneligible}
  if err!=nil{return err}
  // Authorised exact source/Goal binding is rechecked by the final writer
  // transaction; this first reservation prevents amplification on bad bodies.
  var start int64
  var count int
  err=tx.QueryRowContext(ctx,`SELECT window_started_at,attempts
   FROM project_goal_source_request_budgets WHERE credential_id=?`,
   credentialID).Scan(&start,&count)
  if errors.Is(err,sql.ErrNoRows){
   _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_source_request_budgets
    (credential_id,window_started_at,attempts,updated_at) VALUES(?,?,1,?)`,
    credentialID,now,now)
   return err
  }
  if err!=nil{return err}
  if start>now{return ErrSourceRequestRateLimited} // No rollback reset.
  if now-start>=60*1000{
   _,err=tx.ExecContext(ctx,`UPDATE project_goal_source_request_budgets
    SET window_started_at=?,attempts=1,updated_at=? WHERE credential_id=?`,
    now,now,credentialID)
   return err
  }
  if count>=60{return ErrSourceRequestRateLimited}
  _,err=tx.ExecContext(ctx,`UPDATE project_goal_source_request_budgets
   SET attempts=attempts+1,updated_at=? WHERE credential_id=? AND attempts<60`,
   now,credentialID)
  return err
 })
}
