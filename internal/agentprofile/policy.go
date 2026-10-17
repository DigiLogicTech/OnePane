package agentprofile

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/featurepolicy"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// PolicyRevision is independent of the editable Agent prompt revision.
// Only denial/inheritance is permitted. A revoked denial can be relaxed by
// a human, but no Agent policy can grant rights denied by the Project.
type PolicyRevision struct{
 ProfileID string `json:"profile_id"`
 WorkspaceID string `json:"workspace_id"`
 Revision int64 `json:"revision"`
 PolicyJSON json.RawMessage `json:"policy"`
 IssuedBy string `json:"issued_by,omitempty"`
 IssuedAt int64 `json:"issued_at"`
}
type UpdateAgentPolicyCommand struct{
 ProfileID string
 WorkspaceID string
 ExpectedRevision int64
 PolicyJSON json.RawMessage
 ActorPrincipalID string
}

func(s *Service)Policy(ctx context.Context,profileID,workspaceID string)(PolicyRevision,error){
 profileID,workspaceID=strings.TrimSpace(profileID),strings.TrimSpace(workspaceID)
 if profileID==""||workspaceID==""{return PolicyRevision{},ErrInvalid}
 var active,source string
 var owner sql.NullString
 err:=s.db.QueryRowContext(ctx,`SELECT workspace_id,status,source_kind FROM agent_profiles WHERE id=?`,profileID).Scan(&owner,&active,&source)
 if errors.Is(err,sql.ErrNoRows){return PolicyRevision{},ErrNotFound}
 if err!=nil{return PolicyRevision{},err}
 if !owner.Valid||owner.String!=workspaceID||source!="custom"||active!="active"{
  return PolicyRevision{},ErrInvalid
 }
 var p PolicyRevision
 var policyDoc string
 err=s.db.QueryRowContext(ctx,`SELECT r.profile_id,r.workspace_id,r.revision,r.policy_json,r.issued_by,r.issued_at
  FROM agent_policy_heads h JOIN agent_policy_revisions r
   ON r.profile_id=h.profile_id AND r.revision=h.revision
  WHERE h.profile_id=? AND r.workspace_id=?`,profileID,workspaceID).
  Scan(&p.ProfileID,&p.WorkspaceID,&p.Revision,&policyDoc,&p.IssuedBy,&p.IssuedAt)
 if errors.Is(err,sql.ErrNoRows){
  var count int
  if e:=s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM agent_policy_heads WHERE profile_id=?",profileID).Scan(&count);e!=nil{return PolicyRevision{},e}
  if count>0{return PolicyRevision{},fmt.Errorf("%w: dangling Agent policy head",ErrInvalid)}
  return PolicyRevision{ProfileID:profileID,WorkspaceID:workspaceID,Revision:0,
   PolicyJSON:json.RawMessage(`{"feature_policy":{}}`)},nil
 }
 if err!=nil{return PolicyRevision{},err}
 p.PolicyJSON=json.RawMessage(policyDoc)
 return p,nil
}

// UpdatePolicy uses optimistic concurrency on the policy HEAD, not the
// prompt revision. Security-sensitive widening requires an active human;
// Agents and automations can only tighten current permissions. Every
// revision is immutable and auditable.
func(s *Service)UpdatePolicy(ctx context.Context,cmd UpdateAgentPolicyCommand)(PolicyRevision,error){
 cmd.ProfileID,cmd.WorkspaceID,cmd.ActorPrincipalID=
  strings.TrimSpace(cmd.ProfileID),strings.TrimSpace(cmd.WorkspaceID),strings.TrimSpace(cmd.ActorPrincipalID)
 if cmd.ProfileID==""||cmd.WorkspaceID==""||cmd.ActorPrincipalID==""||cmd.ExpectedRevision<0{
  return PolicyRevision{},ErrInvalid
 }
 normalized,err:=featurepolicy.CanonicalRestrictiveAgentPolicy(cmd.PolicyJSON)
 if err!=nil{return PolicyRevision{},fmt.Errorf("%w: %v",ErrInvalid,err)}
 now:=s.clock.UnixMilli()
 eventID,err:=s.ids.New("evt")
 if err!=nil{return PolicyRevision{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var owner sql.NullString
  var status,source string
  err:=tx.QueryRowContext(ctx,`SELECT workspace_id,status,source_kind FROM agent_profiles WHERE id=?`,cmd.ProfileID).Scan(&owner,&status,&source)
  if err!=nil{return err}
  if !owner.Valid||owner.String!=cmd.WorkspaceID||status!="active"||source!="custom"{
   return ErrInvalid
  }
  var principalType string
  err=tx.QueryRowContext(ctx,`SELECT p.principal_type FROM principals p
   JOIN workspace_memberships w ON w.principal_id=p.id
   WHERE p.id=? AND p.status='active' AND w.workspace_id=? AND w.status='active'`,
   cmd.ActorPrincipalID,cmd.WorkspaceID).Scan(&principalType)
  if err!=nil{return fmt.Errorf("%w: issuing principal lacks active membership",ErrInvalid)}
  var current int64
  var oldDoc string
  err=tx.QueryRowContext(ctx,`SELECT r.revision,r.policy_json FROM agent_policy_heads h
   JOIN agent_policy_revisions r ON r.profile_id=h.profile_id AND r.revision=h.revision
   WHERE h.profile_id=?`,cmd.ProfileID).Scan(&current,&oldDoc)
  if errors.Is(err,sql.ErrNoRows){
   var headCount int
   if e:=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM agent_policy_heads WHERE profile_id=?",cmd.ProfileID).Scan(&headCount);e!=nil{return e}
   if headCount!=0{return ErrInvalid}
   current=0
   oldDoc=`{"feature_policy":{}}`
  }else if err!=nil{return err}
  if current!=cmd.ExpectedRevision{return ErrRevisionConflict}
  broadens,err:=featurepolicy.AgentRestrictionBroadens([]byte(oldDoc),normalized)
  if err!=nil{return fmt.Errorf("%w: compare Agent policy revisions: %v",ErrInvalid,err)}
  if broadens&&principalType!="human"{
   return fmt.Errorf("%w: only a human can remove an Agent denial",ErrInvalid)
  }
  next:=current+1
  _,err=tx.ExecContext(ctx,`INSERT INTO agent_policy_revisions
   (profile_id,revision,workspace_id,policy_json,issued_by,issued_at)
   VALUES(?,?,?,?,?,?)`,
   cmd.ProfileID,next,cmd.WorkspaceID,string(normalized),cmd.ActorPrincipalID,now)
  if err!=nil{return err}
  var result sql.Result
  if current==0{
   result,err=tx.ExecContext(ctx,"INSERT INTO agent_policy_heads(profile_id,revision) VALUES(?,?)",cmd.ProfileID,next)
  }else{
   result,err=tx.ExecContext(ctx,`UPDATE agent_policy_heads SET revision=?
    WHERE profile_id=? AND revision=?`,next,cmd.ProfileID,current)
  }
  if err!=nil{return err}
  count,err:=result.RowsAffected()
  if err!=nil{return err}
  if count!=1{return ErrRevisionConflict}
  payload,_:=json.Marshal(map[string]any{
   "profile_id":cmd.ProfileID,"revision":next,"previous_revision":current,
   "policy_expanded":broadens,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&cmd.WorkspaceID,
   Type:"agent_policy.revised",AggregateType:"agent_profile_policy",
   AggregateID:cmd.ProfileID,ActorPrincipalID:&cmd.ActorPrincipalID,
   Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return PolicyRevision{},err}
 return s.Policy(ctx,cmd.ProfileID,cmd.WorkspaceID)
}


// PolicyHistory is intentionally bounded and Workspace-scoped. It returns
// only immutable reviewed denial documents and metadata, never Agent
// prompt contents, credentials, model-generated transcripts or Tool inputs.
func(s *Service)PolicyHistory(ctx context.Context,profileID,workspaceID string,limit int)([]PolicyRevision,error){
 profileID,workspaceID=strings.TrimSpace(profileID),strings.TrimSpace(workspaceID)
 if profileID==""||workspaceID==""{return nil,ErrInvalid}
 if limit<1||limit>50{limit=20}
 // Policy() confirms active custom Agent ownership. Do not infer tenancy
 // from a policy revision ID or accept a caller-supplied alternate Workspace.
 if _,err:=s.Policy(ctx,profileID,workspaceID);err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT profile_id,workspace_id,revision,policy_json,issued_by,issued_at
  FROM agent_policy_revisions WHERE profile_id=? AND workspace_id=?
  ORDER BY revision DESC LIMIT ?`,profileID,workspaceID,limit)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]PolicyRevision{}
 for rows.Next(){
  var item PolicyRevision
  var raw string
  if err:=rows.Scan(&item.ProfileID,&item.WorkspaceID,&item.Revision,&raw,
   &item.IssuedBy,&item.IssuedAt);err!=nil{return nil,err}
  item.PolicyJSON=json.RawMessage(raw)
  out=append(out,item)
 }
 return out,rows.Err()
}
