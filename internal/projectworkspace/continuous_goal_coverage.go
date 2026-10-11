package projectworkspace

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
 "strings"
)

// GoalEvidenceCoverage reports attached evidence freshness, not a monitored
// service's security, uptime or verified compliance. It reads only bounded
// metadata from the goal's immutable receipts; it does not dispatch work.
type GoalEvidenceCoverage struct {
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 GoalRevision int64 `json:"goal_revision"`
 GoalStatus string `json:"goal_status"`
 GoalHealth string `json:"goal_health"`
 AssessedAt int64 `json:"assessed_at"`
 FreshnessMinutes int64 `json:"freshness_minutes"`
 Sources []GoalEvidenceSource `json:"sources"`
 Assessment string `json:"assessment"`
 MonitoringActive bool `json:"monitoring_active"`
}

type GoalEvidenceSource struct {
 SourceID string `json:"source_id"`
 EvidenceState string `json:"evidence_state"`
 LastObservedAt int64 `json:"last_observed_at"`
}

func(s *Service)GoalEvidenceCoverage(ctx context.Context,projectID,goalID string)(GoalEvidenceCoverage,error){
 projectID=strings.TrimSpace(projectID)
 goalID=strings.TrimSpace(goalID)
 if projectID==""||goalID==""{return GoalEvidenceCoverage{},ErrInvalidCommand}
 var planRaw,status,health string
 var cadence int
 var revision int64
 // A goal must belong to the canonical Project and an active Workspace
 // before its current coverage can be evaluated. Archived Workspaces do
 // not silently become a Project-wide monitoring source.
 err:=s.db.QueryRowContext(ctx,`SELECT g.monitoring_plan_json,g.status,g.health,g.cadence_minutes,g.revision
  FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id
  JOIN project_workspaces w ON w.id=g.project_workspace_id
      AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=? AND p.status='active'`,
  goalID,projectID).Scan(&planRaw,&status,&health,&cadence,&revision)
 if err!=nil{return GoalEvidenceCoverage{},err}
 // Stored source allowlists are security inputs, too: a legacy or
 // tampered row must not bypass the same strict admission as a new goal.
 canonical,err:=canonicalGoalPlan(json.RawMessage(planRaw))
 if err!=nil{return GoalEvidenceCoverage{},fmt.Errorf("%w: invalid stored goal plan: %v",ErrInvalidCommand,err)}
 var plan goalMonitorPlan
 if err=json.Unmarshal(canonical,&plan);err!=nil||plan.Version!=1||len(plan.SourceIDs)>32{
  return GoalEvidenceCoverage{},fmt.Errorf("%w: unsupported stored goal plan",ErrInvalidCommand)
 }
 now:=s.clock.UnixMilli()
 freshness:=int64(cadence)*2
 if freshness<15{freshness=15}
 if freshness>24*60{freshness=24*60}
 // The aggregate avoids holding complete source payloads, identifiers or
 // unbounded histories in memory, even on small local hardware.
 rows,err:=s.db.QueryContext(ctx,`SELECT source_id,MAX(observed_at)
   FROM project_goal_observation_receipts WHERE goal_id=? GROUP BY source_id`,goalID)
 if err!=nil{return GoalEvidenceCoverage{},err}
 last:=map[string]int64{}
 for rows.Next(){
  var id string
  var timestamp int64
  if err:=rows.Scan(&id,&timestamp);err!=nil{rows.Close();return GoalEvidenceCoverage{},err}
  last[id]=timestamp
 }
 if err:=rows.Err();err!=nil{rows.Close();return GoalEvidenceCoverage{},err}
 rows.Close()
 sources:=make([]GoalEvidenceSource,0,len(plan.SourceIDs))
 fresh,stale,missing:=0,0,0
 for _,id:=range plan.SourceIDs{
  seen:=last[id]
  item:=GoalEvidenceSource{SourceID:id,LastObservedAt:seen}
  switch{
  case seen==0:
   item.EvidenceState="missing"
   missing++
  case seen>now||now-seen>freshness*60*1000:
   item.EvidenceState="stale_attached"
   stale++
  default:
   item.EvidenceState="recent_attached"
   fresh++
  }
  sources=append(sources,item)
 }
 _=fresh
 assessment:="unconfigured_sources"
 if len(sources)>0 {
  assessment="recent_evidence_attached"
  if missing>0||stale>0{assessment="incomplete_evidence"}
 }
 return GoalEvidenceCoverage{
  ProjectID:projectID,GoalID:goalID,GoalRevision:revision,
  GoalStatus:status,GoalHealth:health,AssessedAt:now,
  FreshnessMinutes:freshness,Sources:sources,Assessment:assessment,
  MonitoringActive:false,
 },nil
}

// Compile-time guard that future DB schema changes do not make missing goal
// ownership look like an empty, healthy status.
var _ = sql.ErrNoRows
