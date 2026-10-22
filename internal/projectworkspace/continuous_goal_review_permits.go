package projectworkspace

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// GoalEvidenceReviewPermit allows only periodic inspection of previously
// human-attested observation RECEIPT METADATA. Its presence cannot execute a
// probe, read raw sensor values, start a model, or activate goal monitoring.
type GoalEvidenceReviewPermit struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 SourceID string `json:"source_id"`
 PlanHash string `json:"plan_hash"`
 EffectClass string `json:"effect_class"`
 Status string `json:"status"`
 Revision int64 `json:"revision"`
 ApprovedBy string `json:"approved_by"`
 RevokedBy *string `json:"revoked_by,omitempty"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
 RevokedAt int64 `json:"revoked_at"`
}

type ApproveGoalEvidenceReviewCommand struct {
 ProjectID string
 GoalID string
 SourceID string
 ExpectedGoalRevision int64
 ExpiresAt int64
 ActorPrincipalID string
}
type RevokeGoalEvidenceReviewCommand struct {
 ProjectID string
 GoalID string
 PermitID string
 ExpectedGoalRevision int64
 ExpectedPermitRevision int64
 ActorPrincipalID string
}

const reviewPermitColumns = `id,project_id,goal_id,project_workspace_id,source_id,plan_hash,effect_class,status,revision,approved_by,revoked_by,approved_at,expires_at,revoked_at`

func scanReviewPermit(row scanner)(GoalEvidenceReviewPermit,error){
 var p GoalEvidenceReviewPermit
 var revoked sql.NullString
 err:=row.Scan(&p.ID,&p.ProjectID,&p.GoalID,&p.ProjectWorkspaceID,&p.SourceID,
  &p.PlanHash,&p.EffectClass,&p.Status,&p.Revision,&p.ApprovedBy,&revoked,
  &p.ApprovedAt,&p.ExpiresAt,&p.RevokedAt)
 if revoked.Valid{p.RevokedBy=&revoked.String}
 return p,err
}

func goalReviewPlan(raw string)(goalMonitorPlan,string,error){
 canonical,err:=canonicalGoalPlan(json.RawMessage(raw))
 if err!=nil{return goalMonitorPlan{},"",fmt.Errorf("%w: invalid goal source plan",ErrInvalidCommand)}
 var plan goalMonitorPlan
 if err=json.Unmarshal(canonical,&plan);err!=nil{return goalMonitorPlan{},"",err}
 hash:=sha256.Sum256(canonical)
 return plan,"sha256:"+hex.EncodeToString(hash[:]),nil
}

func humanGoalReviewer(ctx context.Context,tx storage.Tx,principal string)error{
 var kind string
 err:=tx.QueryRowContext(ctx,`SELECT principal_type FROM principals
  WHERE id=? AND status='active'`,principal).Scan(&kind)
 if err!=nil{return fmt.Errorf("%w: active human reviewer required",ErrPrincipalIneligible)}
 if kind!="human"{return fmt.Errorf("%w: human reviewer required",ErrPrincipalIneligible)}
 return nil
}

// ApproveGoalEvidenceReview grants a bounded receipt-metadata review, never
// sensor or host permission. Both membership and Project/Workspace ownership
// are rechecked inside the same serialized SQLite transaction as the permit.
func(s *Service)ApproveGoalEvidenceReview(ctx context.Context,c ApproveGoalEvidenceReviewCommand)(GoalEvidenceReviewPermit,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.SourceID=strings.TrimSpace(c.SourceID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 now:=s.clock.UnixMilli()
 if c.ProjectID==""||c.GoalID==""||!goalSourceID.MatchString(c.SourceID)||
   c.ActorPrincipalID==""||c.ExpectedGoalRevision<1||
   c.ExpiresAt<=now+60*1000||c.ExpiresAt>now+30*24*60*60*1000{
  return GoalEvidenceReviewPermit{},ErrInvalidCommand
 }
 permitID,err:=s.ids.New("grev")
 if err!=nil{return GoalEvidenceReviewPermit{},err}
 evtID,err:=s.ids.New("evt")
 if err!=nil{return GoalEvidenceReviewPermit{},err}
 var permit GoalEvidenceReviewPermit
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return fmt.Errorf("%w: active Level 5 Project required",ErrInvalidCommand)
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var wsID,status,planRaw string
  var revision int64
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,g.monitoring_plan_json,g.revision
    FROM project_continuous_goals g JOIN project_workspaces w
      ON w.id=g.project_workspace_id AND w.project_id=g.project_id AND w.status='active'
    WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).Scan(&wsID,&status,&planRaw,&revision)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status!="configured"{return ErrInvalidTransition}
  plan,hash,err:=goalReviewPlan(planRaw)
  if err!=nil{return err}
  allowed:=false
  for _,source:=range plan.SourceIDs{if source==c.SourceID{allowed=true;break}}
  if !allowed{return fmt.Errorf("%w: source absent from approved goal plan",ErrInvalidCommand)}
  var existing int
  if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_evidence_review_permits
      WHERE goal_id=? AND source_id=? AND status='approved'`,c.GoalID,c.SourceID).Scan(&existing);err!=nil{return err}
  // Even an expired permit requires human revocation, so historical
  // authorisation cannot be silently replaced or revived.
  if existing!=0{return ErrInvalidTransition}
  permit=GoalEvidenceReviewPermit{
   ID:permitID,ProjectID:c.ProjectID,GoalID:c.GoalID,
   ProjectWorkspaceID:wsID,SourceID:c.SourceID,PlanHash:hash,
   EffectClass:"existing_receipt_metadata",Status:"approved",Revision:1,
   ApprovedBy:c.ActorPrincipalID,ApprovedAt:now,ExpiresAt:c.ExpiresAt,
  }
  if _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_evidence_review_permits(
    id,project_id,goal_id,project_workspace_id,source_id,plan_hash,
    effect_class,status,revision,approved_by,approved_at,expires_at)
    VALUES(?,?,?,?,?,?,'existing_receipt_metadata','approved',1,?,?,?)`,
    permit.ID,permit.ProjectID,permit.GoalID,permit.ProjectWorkspaceID,
    permit.SourceID,permit.PlanHash,permit.ApprovedBy,permit.ApprovedAt,permit.ExpiresAt);err!=nil{return err}
  result,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
    SET revision=revision+1,updated_at=?
    WHERE id=? AND project_id=? AND revision=? AND status='configured'`,
    now,c.GoalID,c.ProjectID,revision)
  if err!=nil{return err}
  n,err:=result.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"source_id":c.SourceID,"permit_id":permit.ID,
   "effect_class":permit.EffectClass,"expires_at":permit.ExpiresAt,
   "goal_revision":revision+1,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_evidence_review_approved",
   AggregateType:"project_continuous_goal",AggregateID:c.GoalID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalEvidenceReviewPermit{},err}
 return permit,nil
}

// RevokeGoalEvidenceReview invalidates the permit and advances the goal's
// revision atomically. A future dispatcher must re-evaluate current scope
// at the last controllable boundary, never trust an old permit object.
func(s *Service)RevokeGoalEvidenceReview(ctx context.Context,c RevokeGoalEvidenceReviewCommand)(GoalEvidenceReviewPermit,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.PermitID=strings.TrimSpace(c.PermitID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.PermitID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedPermitRevision<1{
  return GoalEvidenceReviewPermit{},ErrInvalidCommand
 }
 evtID,err:=s.ids.New("evt");if err!=nil{return GoalEvidenceReviewPermit{},err}
 now:=s.clock.UnixMilli()
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID);if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var currentRevision int64
  var status string
  err=tx.QueryRowContext(ctx,`SELECT g.status,g.revision FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=g.project_id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).Scan(&status,&currentRevision)
  if err!=nil{return err}
  if currentRevision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status=="retired"{return ErrInvalidTransition}
  p,err:=scanReviewPermit(tx.QueryRowContext(ctx,`SELECT `+reviewPermitColumns+
    ` FROM project_goal_evidence_review_permits WHERE id=? AND goal_id=? AND project_id=?`,
    c.PermitID,c.GoalID,c.ProjectID))
  if err!=nil{return err}
  if p.Status!="approved"{return ErrInvalidTransition}
  if p.Revision!=c.ExpectedPermitRevision{return ErrRevisionConflict}
  result,err:=tx.ExecContext(ctx,`UPDATE project_goal_evidence_review_permits
    SET status='revoked',revision=revision+1,revoked_by=?,revoked_at=?
    WHERE id=? AND goal_id=? AND revision=? AND status='approved'`,
    c.ActorPrincipalID,now,p.ID,c.GoalID,p.Revision)
  if err!=nil{return err}
  n,err:=result.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  result,err=tx.ExecContext(ctx,`UPDATE project_continuous_goals
    SET revision=revision+1,updated_at=? WHERE id=? AND project_id=? AND revision=?`,
    now,c.GoalID,c.ProjectID,currentRevision)
  if err!=nil{return err}
  n,err=result.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"permit_id":p.ID,"source_id":p.SourceID,
   "effect_class":"existing_receipt_metadata","goal_revision":currentRevision+1,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_evidence_review_revoked",
   AggregateType:"project_continuous_goal",AggregateID:c.GoalID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalEvidenceReviewPermit{},err}
 return scanReviewPermit(s.db.QueryRowContext(ctx,`SELECT `+reviewPermitColumns+
  ` FROM project_goal_evidence_review_permits WHERE id=?`,c.PermitID))
}

// ListGoalEvidenceReviews returns permit metadata only after revalidating
// canonical goal ownership and active tenant/workspace state.
func(s *Service)ListGoalEvidenceReviews(ctx context.Context,projectID,goalID string)([]GoalEvidenceReviewPermit,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""{return nil,ErrInvalidCommand}
 var ok int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=? LIMIT 1`,goalID,projectID).Scan(&ok)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+reviewPermitColumns+
  ` FROM project_goal_evidence_review_permits
    WHERE goal_id=? AND project_id=? ORDER BY approved_at DESC,id DESC LIMIT 64`,goalID,projectID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=make([]GoalEvidenceReviewPermit,0)
 for rows.Next(){
  p,err:=scanReviewPermit(rows)
  if err!=nil{return nil,err}
  out=append(out,p)
 }
 return out,rows.Err()
}

// GoalEvidenceReviewAllowed resolves CURRENT permit, goal, Project, Workspace,
// original source-plan hash and expiry. It is not by itself an active sensor
// authorisation; only pre-attested metadata is in scope.
func(s *Service)GoalEvidenceReviewAllowed(ctx context.Context,projectID,goalID,sourceID string)(bool,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""||
  !goalSourceID.MatchString(sourceID){return false,ErrInvalidCommand}
 project,err:=s.repo.Project(ctx,projectID)
 if err!=nil{return false,err}
 if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{return false,nil}
 var status,planRaw,workspaceID string
 err=s.db.QueryRowContext(ctx,`SELECT g.status,g.monitoring_plan_json,g.project_workspace_id
  FROM project_continuous_goals g
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=g.project_id AND w.status='active'
  JOIN workspaces t ON t.id=? AND t.status='active'
  WHERE g.id=? AND g.project_id=?`,project.WorkspaceID,goalID,projectID).
  Scan(&status,&planRaw,&workspaceID)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 if status!="configured"&&status!="active"{return false,nil}
 plan,hash,err:=goalReviewPlan(planRaw)
 if err!=nil{return false,err}
 inPlan:=false
 for _,id:=range plan.SourceIDs{if id==sourceID{inPlan=true;break}}
 if !inPlan{return false,nil}
 var present int
 err=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_goal_evidence_review_permits
  WHERE project_id=? AND goal_id=? AND project_workspace_id=?
   AND source_id=? AND plan_hash=? AND effect_class='existing_receipt_metadata'
   AND status='approved' AND expires_at>? LIMIT 1`,
  projectID,goalID,workspaceID,sourceID,hash,s.clock.UnixMilli()).Scan(&present)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 return err==nil,err
}
