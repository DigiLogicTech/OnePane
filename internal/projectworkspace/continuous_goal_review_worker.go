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
// dueGoalEvidenceReviewCandidates selects a stable 64-row cursor page.
// The cursor is operational state, never a source of authority.
func(s *Service)dueGoalEvidenceReviewCandidates(ctx context.Context,now int64,after string)([]dueGoalReview,error){
 rows,err:=s.db.QueryContext(ctx,`SELECT p.project_id,p.goal_id,p.id,g.revision,p.revision
  FROM project_goal_evidence_review_permits p
  JOIN project_continuous_goals g ON g.id=p.goal_id AND g.project_id=p.project_id
   AND g.project_workspace_id=p.project_workspace_id
   AND g.status IN ('configured','active')
  JOIN projects pr ON pr.id=p.project_id AND pr.status='active'
  JOIN workspaces t ON t.id=pr.workspace_id AND t.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=pr.id AND w.status='active'
  WHERE p.id>? AND p.status='approved'
   AND p.effect_class='existing_receipt_metadata'
   AND p.expires_at>? AND g.cadence_minutes BETWEEN 5 AND 10080
   AND NOT EXISTS (
     SELECT 1 FROM project_goal_evidence_review_runs r
      WHERE r.permit_id=p.id
       AND r.cadence_window=CAST(?/(g.cadence_minutes*60000) AS INTEGER))
  ORDER BY p.id LIMIT 64`,after,now,now)
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
 return candidates,err
}

// Advancing an operational cursor cannot expand permits or mint authority.
// Racing workers may observe the same due source, but the immutable
// (permit,window) uniqueness check prevents a duplicate review.
func(s *Service)advanceGoalReviewCursor(ctx context.Context,revision int64,after string,now int64)error{
 result,err:=s.db.ExecContext(ctx,`UPDATE goal_review_worker_cursors
   SET after_permit_id=?,revision=revision+1,updated_at=?
   WHERE worker_id='receipt_metadata' AND revision=?`,after,now,revision)
 if err!=nil{return err}
 n,err:=result.RowsAffected()
 if err!=nil{return err}
 // CAS lost to another poller; its cursor supersedes ours safely.
 _=n
 return nil
}

// TickDueGoalEvidenceReviews visits at most one 64-row cursor page per
// invocation and records at most limit successful metadata reviews.
// Persisting progress past denied permits prevents a small group of
// corrupt/high-priority sources from starving valid reviews. No external
// side effect or inference is performed by this loop.
func(s *Service)TickDueGoalEvidenceReviews(ctx context.Context,limit int)([]GoalEvidenceReviewRun,error){
 if limit<1||limit>32{return nil,ErrInvalidCommand}
 now:=s.clock.UnixMilli()
 var after string
 var revision int64
 err:=s.db.QueryRowContext(ctx,`SELECT after_permit_id,revision
  FROM goal_review_worker_cursors WHERE worker_id='receipt_metadata'`).Scan(&after,&revision)
 if err!=nil{return nil,err}
 candidates,err:=s.dueGoalEvidenceReviewCandidates(ctx,now,after)
 if err!=nil{return nil,err}
 if len(candidates)==0&&after!=""{
  // Continue from the first currently due permit on the next cycle.
  // No catch-up replay of windows missed during system downtime.
  candidates,err=s.dueGoalEvidenceReviewCandidates(ctx,now,"")
  if err!=nil{return nil,err}
  if len(candidates)==0{
   if err:=s.advanceGoalReviewCursor(ctx,revision,"",now);err!=nil{return nil,err}
   return []GoalEvidenceReviewRun{},nil
  }
 }
 results:=make([]GoalEvidenceReviewRun,0,limit)
 lastAttempted:=""
 for _,candidate:=range candidates{
  if ctx.Err()!=nil{return results,ctx.Err()}
  if len(results)>=limit{break}
  lastAttempted=candidate.permitID
  record,e:=s.ReviewGoalEvidence(ctx,ReviewGoalEvidenceCommand{
   ProjectID:candidate.projectID,GoalID:candidate.goalID,
   PermitID:candidate.permitID,ExpectedGoalRevision:candidate.goalRevision,
   ExpectedPermitRevision:candidate.permitRevision,
   ActorPrincipalID:GoalEvidenceReviewWorkerPrincipal,
  })
  if e!=nil{
   if ctx.Err()!=nil{return results,ctx.Err()}
   // Denied permit goes behind the persisted cursor. A broken record
   // cannot indefinitely block healthy sources farther along the index.
   continue
  }
  results=append(results,record)
 }
 if lastAttempted!=""{
  if err:=s.advanceGoalReviewCursor(ctx,revision,lastAttempted,now);err!=nil{
   return results,err
  }
 }
 return results,nil
}
