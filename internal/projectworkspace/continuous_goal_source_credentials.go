package projectworkspace

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "database/sql"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "errors"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// This is a separately authorised, exactly one signer/Goal-scoped machine
// identity. It is not a general project.write or Node execution credential.
type GoalSourceCredential struct{
 CredentialID string `json:"credential_id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 WorkspaceID string `json:"workspace_id"`
 SignerID string `json:"signer_id"`
 SourcePrincipalID string `json:"source_principal_id"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
 RevokedAt int64 `json:"revoked_at,omitempty"`
 Status string `json:"status"`
}
type MintGoalSourceCredentialCommand struct{
 ProjectID,GoalID,SignerID,ActorPrincipalID string
 ExpiresAt int64
}
type RevokeGoalSourceCredentialCommand struct{
 ProjectID,GoalID,SignerID,CredentialID,ActorPrincipalID string
}

// The raw bearer token is returned ONCE from issuance. Do not log,
// export, list, event-source, store, or replay this one-time value.
type MintedGoalSourceCredential struct{
 Credential GoalSourceCredential `json:"credential"`
 BearerToken string `json:"bearer_token"`
}

func(s *Service)MintGoalSourceCredential(ctx context.Context,
 c MintGoalSourceCredentialCommand)(MintedGoalSourceCredential,error){
 if strings.TrimSpace(c.ProjectID)==""||strings.TrimSpace(c.GoalID)==""||
  strings.TrimSpace(c.SignerID)==""||strings.TrimSpace(c.ActorPrincipalID)==""{
  return MintedGoalSourceCredential{},ErrInvalidCommand
 }
 idv,err:=s.ids.New("cred")
 if err!=nil{return MintedGoalSourceCredential{},err}
 evtID,err:=s.ids.New("evt")
 if err!=nil{return MintedGoalSourceCredential{},err}
 secret:=make([]byte,32)
 if _,err=rand.Read(secret);err!=nil{return MintedGoalSourceCredential{},err}
 token:="op_src_"+base64.RawURLEncoding.EncodeToString(secret)
 digest:=sha256.Sum256([]byte(token))
 hash:=hex.EncodeToString(digest[:])
 var result GoalSourceCredential
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if c.ExpiresAt<=now+5*60*1000||c.ExpiresAt>now+7*24*60*60*1000{return ErrInvalidCommand}
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var goalWS,goalStatus,planRaw string
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,g.monitoring_plan_json
   FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=g.project_id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).Scan(&goalWS,&goalStatus,&planRaw)
  if err!=nil{return err}
  if goalStatus!="configured"{return ErrInvalidTransition}
  _,planHash,err:=goalReviewPlan(planRaw)
  if err!=nil{return err}
  signer,err:=scanGoalObservationSigner(tx.QueryRowContext(ctx,
   `SELECT `+goalObservationSignerColumns+` FROM project_goal_observation_signers
    WHERE id=? AND project_id=? AND goal_id=?`,c.SignerID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if signer.Status!="approved"||signer.PlanHash!=planHash||
   signer.ExpiresAt<c.ExpiresAt||signer.ProjectWorkspaceID!=goalWS{return ErrInvalidTransition}
  var sourceActive int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals p
   JOIN workspace_memberships m ON m.principal_id=p.id
    AND m.workspace_id=? AND m.status='active'
   WHERE p.id=? AND p.status='active' AND p.principal_type IN ('service','agent')`,
   project.WorkspaceID,signer.SourcePrincipalID).Scan(&sourceActive)
  if err!=nil{return ErrPrincipalIneligible}
  var approverActive int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals p
   JOIN workspace_memberships m ON m.principal_id=p.id
    AND m.workspace_id=? AND m.status='active'
   WHERE p.id=? AND p.status='active' AND p.principal_type='human'`,
   project.WorkspaceID,signer.ApprovedBy).Scan(&approverActive)
  if err!=nil{return ErrPrincipalIneligible}
  var active int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_source_credential_grants b
   JOIN api_credentials a ON a.id=b.credential_id
   WHERE b.signer_id=? AND b.revoked_by IS NULL AND b.expires_at>?
    AND a.revoked_at IS NULL AND (a.expires_at IS NULL OR a.expires_at>?)`,
   signer.ID,now,now).Scan(&active)
  if err!=nil{return err}
  if active>=2{return ErrInvalidTransition}
  wsScope,_:=json.Marshal([]string{project.WorkspaceID})
  capScope:=`["goal.source.submit"]`
  _,err=tx.ExecContext(ctx,`INSERT INTO api_credentials
   (id,principal_id,name,credential_hash,workspace_scope_json,
    capability_scope_json,created_at,expires_at)
   VALUES(?,?,?,?,?,?,?,?)`,idv,signer.SourcePrincipalID,
   "Level 5 signed source "+signer.SourceID,hash,string(wsScope),capScope,now,c.ExpiresAt)
  if err!=nil{return err}
  result=GoalSourceCredential{
   CredentialID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,
   ProjectWorkspaceID:goalWS,WorkspaceID:project.WorkspaceID,
   SignerID:signer.ID,SourcePrincipalID:signer.SourcePrincipalID,
   ApprovedBy:c.ActorPrincipalID,ApprovedAt:now,ExpiresAt:c.ExpiresAt,Status:"active",
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_source_credential_grants(
   credential_id,project_id,goal_id,project_workspace_id,workspace_id,
   signer_id,source_principal_id,approved_by,approved_at,expires_at)
   VALUES(?,?,?,?,?,?,?,?,?,?)`,
   result.CredentialID,result.ProjectID,result.GoalID,result.ProjectWorkspaceID,
   result.WorkspaceID,result.SignerID,result.SourcePrincipalID,
   result.ApprovedBy,result.ApprovedAt,result.ExpiresAt)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{
   "goal_id":result.GoalID,"signer_id":result.SignerID,
   "credential_id":idv,"expires_at":result.ExpiresAt,
   "capability":"goal.source.submit","monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&project.WorkspaceID,Type:"project.goal_source_credential_issued",
   AggregateType:"project_goal_source_credential",AggregateID:idv,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return MintedGoalSourceCredential{},err}
 return MintedGoalSourceCredential{Credential:result,BearerToken:token},nil
}

func(s *Service)ListGoalSourceCredentials(ctx context.Context,
 projectID,goalID,signerID string)([]GoalSourceCredential,error){
 if projectID==""||goalID==""||signerID==""{return nil,ErrInvalidCommand}
 var exists int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_goal_observation_signers k
  JOIN projects p ON p.id=k.project_id AND p.status='active'
  JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
  JOIN project_continuous_goals g ON g.id=k.goal_id AND g.project_id=p.id
  JOIN project_workspaces w ON w.id=k.project_workspace_id AND w.project_id=p.id AND w.status='active'
  WHERE k.id=? AND k.goal_id=? AND k.project_id=?`,signerID,goalID,projectID).Scan(&exists)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT b.credential_id,b.project_id,b.goal_id,b.project_workspace_id,
  b.workspace_id,b.signer_id,b.source_principal_id,b.approved_by,b.approved_at,
  b.expires_at,
  CASE WHEN b.revoked_at>COALESCE(a.revoked_at,0)
    THEN b.revoked_at ELSE COALESCE(a.revoked_at,0) END,
  CASE
   WHEN b.revoked_by IS NOT NULL OR a.revoked_at IS NOT NULL THEN 'revoked'
   WHEN b.expires_at<=? OR a.expires_at IS NULL OR a.expires_at<=? THEN 'expired'
   WHEN k.status!='approved' OR k.expires_at<=? THEN 'signer_inactive'
   WHEN p.status!='active' OR w.status!='active' THEN 'scope_inactive'
   WHEN EXISTS(SELECT 1 FROM principals pr
     LEFT JOIN workspace_memberships m ON m.workspace_id=b.workspace_id AND m.principal_id=pr.id
     WHERE pr.id=b.source_principal_id
      AND (pr.status!='active' OR pr.principal_type NOT IN ('service','agent')
       OR m.status IS NULL OR m.status!='active')) THEN 'scope_inactive'
   ELSE 'active'
  END
  FROM project_goal_source_credential_grants b
  JOIN api_credentials a ON a.id=b.credential_id
  JOIN project_goal_observation_signers k ON k.id=b.signer_id
  JOIN projects p ON p.id=b.project_id
  JOIN project_workspaces w ON w.id=b.project_workspace_id
  WHERE b.project_id=? AND b.goal_id=? AND b.signer_id=?
  ORDER BY b.approved_at DESC,b.credential_id DESC LIMIT 100`,
  s.clock.UnixMilli(),s.clock.UnixMilli(),s.clock.UnixMilli(),projectID,goalID,signerID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalSourceCredential{}
 for rows.Next(){
  var item GoalSourceCredential
  if err=rows.Scan(&item.CredentialID,&item.ProjectID,&item.GoalID,&item.ProjectWorkspaceID,
   &item.WorkspaceID,&item.SignerID,&item.SourcePrincipalID,&item.ApprovedBy,
   &item.ApprovedAt,&item.ExpiresAt,&item.RevokedAt,&item.Status);err!=nil{return nil,err}
  out=append(out,item)
 }
 return out,rows.Err()
}

func(s *Service)RevokeGoalSourceCredential(ctx context.Context,
 c RevokeGoalSourceCredentialCommand)(GoalSourceCredential,error){
 if c.ProjectID==""||c.GoalID==""||c.SignerID==""||
  c.CredentialID==""||c.ActorPrincipalID==""{return GoalSourceCredential{},ErrInvalidCommand}
 evtID,err:=s.ids.New("evt");if err!=nil{return GoalSourceCredential{},err}
 var out GoalSourceCredential
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var revokedBy sql.NullString
  var revokedAt int64
  err=tx.QueryRowContext(ctx,`SELECT b.project_workspace_id,b.workspace_id,b.source_principal_id,
   b.approved_by,b.approved_at,b.expires_at,b.revoked_by,b.revoked_at
   FROM project_goal_source_credential_grants b
   JOIN project_continuous_goals g ON g.id=b.goal_id
   JOIN project_workspaces w ON w.id=b.project_workspace_id AND w.project_id=b.project_id
   WHERE b.credential_id=? AND b.signer_id=? AND b.goal_id=? AND b.project_id=?
    AND b.workspace_id=?`,c.CredentialID,c.SignerID,c.GoalID,c.ProjectID,p.WorkspaceID).
   Scan(&out.ProjectWorkspaceID,&out.WorkspaceID,&out.SourcePrincipalID,
    &out.ApprovedBy,&out.ApprovedAt,&out.ExpiresAt,&revokedBy,&revokedAt)
  if err!=nil{return err}
  if revokedBy.Valid{return ErrInvalidTransition}
  out.ProjectID=c.ProjectID;out.GoalID=c.GoalID;out.SignerID=c.SignerID;out.CredentialID=c.CredentialID
  now:=s.clock.UnixMilli()
  var changed sql.Result
  changed,err=tx.ExecContext(ctx,`UPDATE project_goal_source_credential_grants
   SET revoked_by=?,revoked_at=? WHERE credential_id=? AND revoked_by IS NULL`,
   c.ActorPrincipalID,now,c.CredentialID)
  if err!=nil{return err}
  n,err:=changed.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  changed,err=tx.ExecContext(ctx,`UPDATE api_credentials SET revoked_at=?
   WHERE id=? AND revoked_at IS NULL`,now,c.CredentialID)
  if err!=nil{return err}
  n,err=changed.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  out.RevokedAt=now
  out.Status="revoked"
  payload,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"signer_id":c.SignerID,
   "credential_id":c.CredentialID,"revoked":true,"monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&p.WorkspaceID,Type:"project.goal_source_credential_revoked",
   AggregateType:"project_goal_source_credential",AggregateID:c.CredentialID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return GoalSourceCredential{},err}
 return out,nil
}

// Checked inside each source writer transaction, not just API authentication:
// token expiry, revocation and exact signer scope remain fail-closed during
// concurrent revocation. Internal non-HTTP callers may omit credentialID.
// Both DB preflight reads and serialized writer transactions use this
// narrow query contract. The latter is always authoritative for admission.
type goalSourceCredentialQuery interface{
 QueryRowContext(context.Context,string,...any)*sql.Row
}
func checkGoalSourceCredentialTx(ctx context.Context,tx goalSourceCredentialQuery,
 credentialID,projectID,goalID,signerID,workspaceID,principalID string,now int64)error{
 if credentialID==""{return nil}
 var found int
 err:=tx.QueryRowContext(ctx,`SELECT 1 FROM project_goal_source_credential_grants b
  JOIN api_credentials a ON a.id=b.credential_id AND a.principal_id=b.source_principal_id
  JOIN project_goal_observation_signers k ON k.id=b.signer_id AND k.goal_id=b.goal_id
  WHERE b.credential_id=? AND b.project_id=? AND b.goal_id=?
   AND b.signer_id=? AND b.workspace_id=? AND b.source_principal_id=?
   AND b.revoked_by IS NULL AND b.expires_at>? AND a.revoked_at IS NULL
   AND a.expires_at>? AND k.status='approved' AND k.expires_at>?
   AND a.workspace_scope_json=? AND a.capability_scope_json='["goal.source.submit"]'`,
   credentialID,projectID,goalID,signerID,workspaceID,principalID,
   now,now,now,string(mustJSONSourceWorkspace(workspaceID))).Scan(&found)
 if errors.Is(err,sql.ErrNoRows){return ErrPrincipalIneligible}
 return err
}
func mustJSONSourceWorkspace(id string)[]byte{
 raw,_:=json.Marshal([]string{id});return raw
}
