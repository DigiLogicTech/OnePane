package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

const GoalEvidenceReviewWorkerPrincipal = "system:goal-evidence-reviewer"

// EnsureGoalEvidenceReviewWorkerPrincipal creates a non-human, named audit
// identity. This principal cannot create grants; the only autonomous operation
// implemented for it is a metadata-only review of already-attested receipts.
func(s *Service)EnsureGoalEvidenceReviewWorkerPrincipal(ctx context.Context)error{
 now:=s.clock.UnixMilli()
 eventID,err:=s.ids.New("evt");if err!=nil{return err}
 return s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var kind,status string
  err:=tx.QueryRowContext(ctx,`SELECT principal_type,status FROM principals WHERE id=?`,
   GoalEvidenceReviewWorkerPrincipal).Scan(&kind,&status)
  if err==nil{
   if kind!="system"||status!="active"{
    return fmt.Errorf("%w: review worker identity has invalid state",ErrPrincipalIneligible)
   }
   return nil
  }
  if !errors.Is(err,sql.ErrNoRows){return err}
  _,err=tx.ExecContext(ctx,`INSERT INTO principals(
    id,principal_type,display_name,status,revision,created_at,updated_at)
    VALUES(?,'system','OnePane evidence-only reviewer','active',1,?,?)`,
    GoalEvidenceReviewWorkerPrincipal,now,now)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{
   "principal_id":GoalEvidenceReviewWorkerPrincipal,
   "purpose":"previously_attested_receipt_metadata_review",
   "no_probe_execution":true,
  })
  actor:=GoalEvidenceReviewWorkerPrincipal
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,Type:"system_principal.registered",AggregateType:"principal",
   AggregateID:actor,ActorPrincipalID:&actor,Payload:payload,OccurredAt:now,
  })
 })
}

type dueGoalReview struct {
 projectID string
 goalID string
 permitID string
 goalRevision int64
 permitRevision int64
}

// TickDueGoalEvidenceReviews advances a small, bounded sample of source
// permits every cycle. It never dispatches a Task, adapter, probe or model.
// Completed cadence windows persist in SQLite; repeats and process restarts
// cannot silently reexecute the same review window. Permission is checked
// again in ReviewGoalEvidence's serialised admission transaction.
func(s *Service)TickDueGoalEvidenceReviews(ctx context.Context,limit int)([]GoalEvidenceReviewRun,error){
 if limit<1||limit>32{return nil,ErrInvalidCommand}
 now:=s.clock.UnixMilli()
 // No user permit means no review. Candidates are selected by the
 // *current* Project/Workspace/tenant status and explicit effect class.
 // Skip windows already durably reviewed; no missed-window backfill or
 // catch-up storm occurs after a system restart.
 rows,err:=s.db.QueryContext(ctx,`SELECT p.project_id,p.goal_id,p.id,g.revision,p.revision
  FROM project_goal_evidence_review_permits p
  JOIN project_continuous_goals g ON g.id=p.goal_id AND g.project_id=p.project_id
   AND g.project_workspace_id=p.project_workspace_id
   AND g.status IN ('configured','active')
  JOIN projects pr ON pr.id=p.project_id AND pr.status='active'
  JOIN workspaces t ON t.id=pr.workspace_id AND t.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=pr.id AND w.status='active'
  WHERE p.status='approved' AND p.effect_class='existing_receipt_metadata'
   AND p.expires_at>? AND g.cadence_minutes BETWEEN 5 AND 10080
   AND NOT EXISTS (
     SELECT 1 FROM project_goal_evidence_review_runs r
      WHERE r.permit_id=p.id
       AND r.cadence_window=CAST(?/(g.cadence_minutes*60000) AS INTEGER))
  ORDER BY COALESCE((SELECT MAX(r.reviewed_at)
    FROM project_goal_evidence_review_runs r WHERE r.permit_id=p.id),0),
    p.approved_at,p.id LIMIT 256`,now,now)
 if err!=nil{return nil,err}
 candidates:=make([]dueGoalReview,0,64)
 for rows.Next(){
  var c dueGoalReview
  if err=rows.Scan(&c.projectID,&c.goalID,&c.permitID,&c.goalRevision,&c.permitRevision);err!=nil{
   rows.Close();return nil,err
  }
  candidates=append(candidates,c)
 }
 err=rows.Err()
 rows.Close()
 if err!=nil{return nil,err}
 results:=make([]GoalEvidenceReviewRun,0,limit)
 for _,c:=range candidates{
  if err=ctx.Err();err!=nil{return results,err}
  if len(results)>=limit{break}
  // This service identity cannot grant itself new capabilities; a revoked,
  // expired, foreign or malformed human-approved permit is refused.
  result,e:=s.ReviewGoalEvidence(ctx,ReviewGoalEvidenceCommand{
   ProjectID:c.projectID,GoalID:c.goalID,PermitID:c.permitID,
   ExpectedGoalRevision:c.goalRevision,ExpectedPermitRevision:c.permitRevision,
   ActorPrincipalID:GoalEvidenceReviewWorkerPrincipal,
  })
  if e!=nil{
   if ctx.Err()!=nil{return results,ctx.Err()}
   // Isolate an invalid source permit instead of blocking unrelated work.
   // Runtime diagnostics can summarize denials without logging raw inputs.
   continue
  }
  results=append(results,result)
 }
 return results,nil
}
