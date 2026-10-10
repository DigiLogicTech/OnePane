package teamworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
 "github.com/DigiLogicTech/OnePane/internal/team"
)

// researchSeatConsent is a bound control-plane signal, never a capability
// granting byte reads or model-visible Library context.
type researchSeatConsent struct {
 Active bool
 EvidenceSHA256 string
 ResearchSHA256 string
}

// consentForSeat validates the complete frozen session/tenant/Team/Project
// relationship before affecting routing. Missing, revoked or other-seat
// approvals do not authorize a Research prompt to receive Library content.
// A protected Council is one information-flow domain. If any other
// active seat is unapproved or human-mediated, it could receive quoted
// private evidence via later cross-critique. Refuse delivery until every
// participating seat has the same origin-node restriction.
func researchEvidenceCouncilIsolationSafe(snapshotJSON string,approved []string)bool{
 var snapshot team.SessionSnapshot
 if json.Unmarshal([]byte(snapshotJSON),&snapshot)!=nil||
  !snapshot.ResearchMode||snapshot.ExecutionMode!="council"||
  snapshot.Research.ChairMode=="manual"||len(snapshot.Members)==0{return false}
 set:=make(map[string]bool,len(approved))
 for _,id:=range approved{set[id]=true}
 active:=0
 for _,m:=range snapshot.Members{
  if m.Status!="active"{continue}
  if m.MemberKind!="agent"||!set[m.ID]{return false}
  var cfg struct{
   CouncilChairOnly bool `json:"council_chair_only"`
   ManualWeb struct{Enabled bool `json:"enabled"`} `json:"manual_web"`
  }
  if len(m.Config)>0&&json.Unmarshal(m.Config,&cfg)!=nil{return false}
  if cfg.ManualWeb.Enabled||cfg.CouncilChairOnly{return false}
  active++
 }
 return active==len(set)&&active>0
}

func(s *Service) consentForSeat(ctx context.Context,tenant,sessionID,seatID,manifestSHA string)(researchSeatConsent,error){
 out:=researchSeatConsent{}
 if s==nil||s.db==nil||tenant==""||sessionID==""||seatID==""||manifestSHA=="" {
  return out,errors.New("Research seat consent requires bound session and seat")
 }
 var state,policyName,seatsRaw,researchSHA,evidenceSHA,snapshotJSON string
 err:=s.db.QueryRowContext(ctx,`SELECT approval.state,approval.policy,
  approval.seat_ids_json,approval.research_manifest_sha256,
  approval.evidence_manifest_sha256,manifest.snapshot_json
 FROM research_evidence_seat_approvals approval
 JOIN research_session_evidence_metadata frozen
  ON frozen.session_id=approval.session_id
  AND frozen.workspace_id=approval.workspace_id
  AND frozen.project_id=approval.project_id
  AND frozen.project_workspace_id=approval.project_workspace_id
  AND frozen.research_manifest_sha256=approval.research_manifest_sha256
  AND frozen.evidence_manifest_sha256=approval.evidence_manifest_sha256
 JOIN team_sessions session ON session.id=approval.session_id
  AND session.workspace_id=approval.workspace_id
 JOIN team_session_manifests manifest ON manifest.session_id=session.id
  AND manifest.research_mode=1 AND manifest.execution_mode='council'
  AND manifest.snapshot_sha256=approval.research_manifest_sha256
 JOIN teams team ON team.id=session.team_id
  AND team.workspace_id=session.workspace_id AND team.status='active'
 JOIN tasks task ON task.id=session.task_id
  AND task.workspace_id=session.workspace_id
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
 WHERE approval.session_id=? AND approval.workspace_id=?`,
 sessionID,tenant).Scan(&state,&policyName,&seatsRaw,&researchSHA,&evidenceSHA,&snapshotJSON)
 if errors.Is(err,sql.ErrNoRows){
  // An approval record may exist but have since lost its authority:
  // do NOT silently downgrade that situation to unapproved inference.
  var recordCount int
  if e:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM research_evidence_seat_approvals
    WHERE session_id=?`,sessionID).Scan(&recordCount);e!=nil{return out,e}
  if recordCount!=0{return out,fmt.Errorf("Research evidence approval authority has expired")}
  return out,nil
 }
 if err!=nil{return out,err}
 if state=="revoked"{
  // Previously generated replies may quote protected evidence. Stop the
  // whole Research Council; do not resume into an unfenced cloud seat.
  return out,fmt.Errorf("Research evidence consent was revoked; create a new Council session")
 }
 if state!="approved"||policyName!="local_only"||
   researchSHA!=manifestSHA||evidenceSHA==""{
  return out,fmt.Errorf("invalid or stale Research evidence seat consent")
 }
 var ids []string
 if err=json.Unmarshal([]byte(seatsRaw),&ids);err!=nil||len(ids)==0||len(ids)>8{
  return out,fmt.Errorf("invalid Research evidence seat list")
 }
 for i,id:=range ids{
  if id==""||strings.TrimSpace(id)!=id||(i>0&&ids[i-1]>=id){
   return out,fmt.Errorf("noncanonical Research evidence seat list")
  }
  if id==seatID{out.Active=true}
 }
 if !researchEvidenceCouncilIsolationSafe(snapshotJSON,ids){
  return researchSeatConsent{},fmt.Errorf("protected Research evidence requires all Council seats to be approved local agents")
 }
 if !out.Active{
  return researchSeatConsent{},fmt.Errorf("Research seat was not approved for protected Council evidence")
 }
 if out.Active{
  out.ResearchSHA256=researchSHA
  out.EvidenceSHA256=evidenceSHA
 }
 return out,nil
}

// Local-only means a pinned origin-node model deployment, not just a free
// API endpoint or a remote runtime that happens to bill at zero cost.
func researchEvidenceCandidateAllowed(c scheduler.Candidate)bool{
 return c.Kind==scheduler.CandidateModel&&c.Local&&
  c.CostClass==scheduler.CostLocal&&c.HardZeroIncrementalCost&&
  c.Destination.Kind==policy.DestinationOriginNode&&
  strings.TrimSpace(c.Provider)==""&&
  c.NodeID!=""&&
  (c.ComputeMode=="cpu"||c.ComputeMode=="gpu"||c.ComputeMode=="hybrid")
}

func applyResearchEvidenceRouteFence(r *scheduler.RouteRequest){
 r.LocalOnly=true
 r.AllowedKinds=[]scheduler.CandidateKind{scheduler.CandidateModel}
 r.RequireZeroIncrementalCost=true
 r.AllowSubscriptionUsage=false
 r.AllowPotentialMonetarySpend=false
 r.PreferZeroIncrementalCost=true
}
