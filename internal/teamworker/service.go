package teamworker

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/agentworker"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

const WorkerPrincipal = "system:team-worker"

type Service struct {
	db        *sql.DB
	tx        storage.Transactor
	clock     clock.Clock
	ids       id.Generator
	events    event.Store
	teams     *team.Service
	scheduler *scheduler.Service
	inference *inference.Service
	runtimes  *agentruntime.Service
	artifacts *artifact.Service
	budgets   *budget.Service
}

type Result struct {
	TurnID, SessionID, MemberID, Status string
	Error                               string `json:"error,omitempty"`
}
type routePolicy struct {
	AllowSubscriptionUsage      bool   `json:"allow_subscription_usage"`
	AllowPotentialMonetarySpend bool   `json:"allow_potential_monetary_spend"`
	PreferZeroIncrementalCost   bool   `json:"prefer_zero_incremental_cost"`
	RequireZeroIncrementalCost  bool   `json:"require_zero_incremental_cost"`
	AllowUntested               bool   `json:"allow_untested"`
	AllowLimited                bool   `json:"allow_limited"`
	AllowMediated               bool   `json:"allow_mediated"`
	AllowDegraded               bool   `json:"allow_degraded"`
	BudgetAccountID             string `json:"budget_account_id,omitempty"`
	BudgetReserveAmount         int64  `json:"budget_reserve_amount,omitempty"`
}

func New(db *sql.DB, tx storage.Transactor, clk clock.Clock, teams *team.Service, sched *scheduler.Service, infer *inference.Service, runtimes *agentruntime.Service, artifacts *artifact.Service, budgets *budget.Service) *Service {
	return &Service{db: db, tx: tx, clock: clk, ids: id.Generator{}, events: event.Store{}, teams: teams, scheduler: sched, inference: infer, runtimes: runtimes, artifacts: artifacts, budgets: budgets}
}

func (s *Service) EnsureSystemPrincipal(ctx context.Context) error {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM principals WHERE id=?`, WorkerPrincipal).Scan(&status)
	if err == nil {
		if status != "active" {
			return fmt.Errorf("team worker principal is %s", status)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES(?,'system','Team Deliberation Worker','active',1,?,?)`, WorkerPrincipal, now, now); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		actor := WorkerPrincipal
		p, _ := json.Marshal(map[string]any{"principal_id": WorkerPrincipal, "purpose": "team_deliberation"})
		return s.events.Append(ctx, tx, event.Event{ID: eid, Type: "system_principal.registered", AggregateType: "principal", AggregateID: WorkerPrincipal, ActorPrincipalID: &actor, Payload: p, OccurredAt: now})
	})
}
func (s *Service) ensureWorkspace(ctx context.Context, ws string) error {
	if err := s.EnsureSystemPrincipal(ctx); err != nil {
		return err
	}
	var st string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM workspace_memberships WHERE workspace_id=? AND principal_id=?`, ws, WorkerPrincipal).Scan(&st)
	if err == nil {
		if st != "active" {
			return fmt.Errorf("team worker membership is %s", st)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := s.clock.UnixMilli()
	_, err = s.db.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES(?,?,'active',?,?)`, ws, WorkerPrincipal, now, now)
	return err
}
func (s *Service) Tick(ctx context.Context, limit int) ([]Result, error) {
	if limit <= 0 || limit > 64 {
		limit = 16
	}
	if err := s.advanceResearchSessions(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT tr.id,tr.workspace_id,tr.session_id,tr.member_id,tr.round_number,tr.research_phase
		FROM team_turn_requests tr
		JOIN team_sessions ss ON ss.id=tr.session_id
		WHERE tr.status='pending' AND ss.status='deliberating'
		ORDER BY tr.created_at,tr.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type q struct {
		id, ws, session, member, phase string
		round int64
	}
	var qs []q
	for rows.Next() {
		var v q
		if err := rows.Scan(&v.id, &v.ws, &v.session, &v.member, &v.round, &v.phase); err != nil {
			return nil, err
		}
		qs = append(qs, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(qs))
	for _, v := range qs {
		out = append(out, s.process(ctx, v.id, v.ws, v.session, v.member, v.round, v.phase))
	}
	if err := s.advanceResearchSessions(ctx); err != nil {
		return out, err
	}
	_ = s.teams.SyncTaskStates(ctx)
	return out, nil
}

func (s *Service) process(ctx context.Context, turnID, ws, sessionID, memberID string, turnRound int64, researchPhase string) Result {
	res := Result{TurnID: turnID, SessionID: sessionID, MemberID: memberID, Status: "failed"}
	if err := s.ensureWorkspace(ctx, ws); err != nil {
		res.Error = err.Error()
		return res
	}
	ss, err := s.teams.Session(ctx, sessionID)
	if err != nil {
		return s.fail(ctx, res, err)
	}
	if ss.Status != "deliberating" {
		res.Status = "paused"
		res.Error = "team session is not dispatching"
		return res
	}
	now := s.clock.UnixMilli()
	r, err := s.db.ExecContext(ctx, `UPDATE team_turn_requests SET status='running',updated_at=? WHERE id=? AND status='pending'`, now, turnID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		res.Error = "turn already claimed"
		return res
	}
	if ss.WorkspaceID != ws {
		return s.fail(ctx, res, team.ErrWorkspaceMismatch)
	}
	manifestRecord, manifestErr := s.teams.SessionManifest(ctx, ss.ID)
	hasManifest := manifestErr == nil
	if manifestErr != nil && !errors.Is(manifestErr, sql.ErrNoRows) {
		return s.fail(ctx, res, manifestErr)
	}
	var snapshot team.SessionSnapshot
	if hasManifest {
		if err := json.Unmarshal(manifestRecord.Snapshot, &snapshot); err != nil {
			return s.fail(ctx, res, fmt.Errorf("decode immutable session manifest: %w", err))
		}
	}
	var member *team.Member
	var profileSnapshot *team.SnapshotAgentProfile
	var taskObjective, executionMode string
	var research team.ResearchSettings
	var researchMode bool
	if hasManifest {
		for _, sm := range snapshot.Members {
			if sm.ID != memberID {
				continue
			}
			member = &team.Member{ID: sm.ID, TeamID: ss.TeamID, WorkspaceID: ss.WorkspaceID, PrincipalID: sm.PrincipalID, MemberKind: sm.MemberKind, DisplayName: sm.DisplayName, RoleName: sm.RoleName, CapabilityID: sm.CapabilityID, ProtocolLevel: sm.ProtocolLevel, RoutePolicy: append(json.RawMessage(nil), sm.RoutePolicy...), Ordinal: sm.Ordinal, Status: sm.Status, Config: append(json.RawMessage(nil), sm.Config...)}
			profileSnapshot = sm.Profile
			break
		}
		taskObjective = snapshot.TaskObjective
		executionMode = snapshot.ExecutionMode
		researchMode = snapshot.ResearchMode && executionMode == "council"
		research = team.NormalizeResearchSettings(snapshot.Research)
	} else {
		members, err := s.teams.ListMembers(ctx, ss.TeamID)
		if err != nil {
			return s.fail(ctx, res, err)
		}
		for i := range members {
			if members[i].ID == memberID {
				member = &members[i]
				break
			}
		}
		if err := s.db.QueryRowContext(ctx, `SELECT objective FROM tasks WHERE id=?`, ss.TaskID).Scan(&taskObjective); err != nil {
			return s.fail(ctx, res, err)
		}
		if err := s.db.QueryRowContext(ctx, `SELECT execution_mode FROM task_execution_profiles WHERE task_id=?`, ss.TaskID).Scan(&executionMode); err != nil {
			return s.fail(ctx, res, err)
		}
	}
	if member == nil || member.Status != "active" || member.MemberKind == "human" {
		return s.fail(ctx, res, fmt.Errorf("ineligible team member"))
	}
	if executionMode != "council" {
		executionMode = "team"
		researchMode = false
	}
	msgs, err := s.teams.ListMessages(ctx, ss.ID, 200)
	if err != nil {
		return s.fail(ctx, res, err)
	}
	sections := []agentprotocol.ContextSection{}
	profileID := "agent.md"
	var memberCfg map[string]any
	if json.Unmarshal(member.Config,&memberCfg)==nil {
		if v,ok:=memberCfg["agent_profile_id"].(string);ok&&strings.TrimSpace(v)!=""{profileID=strings.TrimSpace(v)}
		if v,ok:=memberCfg["agent_profile"].(string);ok&&strings.TrimSpace(v)!=""{profileID=strings.TrimSpace(v)}
	}
	if strings.EqualFold(profileID,"onepane-default"){profileID="agent.md"}
	var profileName,profileInstructions,profileRole string
	var profileRevision int64
	if profileSnapshot != nil {
		profileID, profileName, profileInstructions, profileRole, profileRevision = profileSnapshot.ID, profileSnapshot.Name, profileSnapshot.Instructions, profileSnapshot.Role, profileSnapshot.Revision
		raw,_:=json.Marshal(map[string]any{"profile_id":profileID,"name":profileName,"role":profileRole,"revision":profileRevision,"instructions":profileInstructions,"authority":false,"frozen":true,"note":"Profile instructions are frozen at session start, affect reasoning only, and grant no capabilities or permissions."})
		sections=append(sections,agentprotocol.ContextSection{ID:"agent-profile",Kind:"agent_profile",Trust:"USER_INSTRUCTION",Authoritative:false,Content:raw})
	} else if err:=s.db.QueryRowContext(ctx,`SELECT name,instructions_md,default_role,revision FROM agent_profiles WHERE id=? AND status='active' AND (workspace_id=? OR workspace_id IS NULL) ORDER BY CASE WHEN workspace_id=? THEN 0 ELSE 1 END LIMIT 1`,profileID,ws,ws).Scan(&profileName,&profileInstructions,&profileRole,&profileRevision);err==nil{
		raw,_:=json.Marshal(map[string]any{"profile_id":profileID,"name":profileName,"role":profileRole,"revision":profileRevision,"instructions":profileInstructions,"authority":false,"note":"Profile instructions affect reasoning only and grant no capabilities or permissions."})
		sections=append(sections,agentprotocol.ContextSection{ID:"agent-profile",Kind:"agent_profile",Trust:"USER_INSTRUCTION",Authoritative:false,Content:raw})
	}else if !errors.Is(err,sql.ErrNoRows){return s.fail(ctx,res,err)}
	taskRaw, _ := json.Marshal(map[string]any{"task_id": ss.TaskID, "objective": taskObjective, "mode": executionMode + "_deliberation", "team_role": member.RoleName})
	sections = append(sections, agentprotocol.ContextSection{ID: "task", Kind: "task", Trust: "USER_INSTRUCTION", Authoritative: true, Content: taskRaw})
	used := len(taskRaw)
	start := 0
	if len(msgs) > 80 {
		start = len(msgs) - 80
	}
	for _, m := range msgs[start:] {
		if researchMode && m.Kind == "agent" {
			// Same-round outputs are hidden even if another seat finished first.
			if m.RoundNumber >= turnRound {
				continue
			}
			if researchPhase == team.ResearchPhaseIndependent {
				continue
			}
		}
		trust := "UNVERIFIED_DERIVED"
		if m.Kind == "human" {
			trust = "USER_INSTRUCTION"
		}
		payload := map[string]any{"kind": m.Kind, "content": json.RawMessage(m.Content), "round": m.RoundNumber}
		if m.Kind == "agent" && researchMode && research.AnonymizedCrossCritique && researchPhase != team.ResearchPhaseIndependent {
			payload["author_alias"] = researchAuthorAlias(snapshot, m.AuthorMemberID)
		} else {
			payload["author_member_id"] = m.AuthorMemberID
		}
		raw, _ := json.Marshal(payload)
		if used+len(raw) > 96<<10 {
			continue
		}
		used += len(raw)
		sections = append(sections, agentprotocol.ContextSection{ID: "msg-" + m.ID, Kind: "team_message", Trust: trust, Authoritative: m.Kind == "human", Content: raw})
	}
	// Operator-approved Chair agenda and follow-up questions are visible only
	// to subsequent rounds. Unapproved proposals never enter seat evidence.
	if researchMode && research.ChairMode=="manual" {
		chairEvidence,err:=s.chairApprovedContext(ctx,ss.ID,turnRound)
		if err!=nil{return s.fail(ctx,res,err)}
		sections=append(sections,chairEvidence...)
	}
	// Web Chat seats are human-mediated. They never call an inference API,
	// consume a scheduled provider candidate or acquire tool permissions.
	// Snapshot config and evidence filtering above still govern their prompt.
	manual, manualEnabled, manualErr := manualWebSeatFromConfig(member.Config)
	if manualErr != nil { return s.fail(ctx, res, manualErr) }
	if manualEnabled {
		if executionMode != "council" {
			return s.fail(ctx, res, fmt.Errorf("manual Web Chat seats require Council execution mode"))
		}
		prompt := buildManualWebCouncilPrompt(ss.ID, member.RoleName, taskObjective, turnRound, researchPhase, research, sections)
		_, err := s.teams.QueueManualWebTurn(ctx, team.QueueManualWebTurnCommand{
			TurnID: turnID, SessionID: ss.ID, WorkspaceID: ws, MemberID: member.ID,
			ProviderID: manual.ProviderID, ModelLabel: manual.ModelLabel, Prompt: prompt,
		})
		if err != nil { return s.fail(ctx, res, fmt.Errorf("queue manual Web Chat Council turn: %w", err)) }
		res.Status = "awaiting_manual_input"
		return res
	}
	rp := routePolicy{PreferZeroIncrementalCost: true, AllowMediated: true, AllowDegraded: true}
	_ = json.Unmarshal(member.RoutePolicy, &rp)
	label := policy.DataLabel{WorkspaceID: ws, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction}
	request := scheduler.RouteRequest{WorkspaceID: ws, CapabilityID: member.CapabilityID, RoleName: member.RoleName, ProtocolLevel: member.ProtocolLevel, ContextTokens: int64((used + 3) / 4), DataLabel: label, AllowUntested: rp.AllowUntested, AllowLimited: rp.AllowLimited, AllowMediated: rp.AllowMediated, AllowDegraded: rp.AllowDegraded, RequireZeroIncrementalCost: rp.RequireZeroIncrementalCost, PreferZeroIncrementalCost: rp.PreferZeroIncrementalCost, AllowSubscriptionUsage: rp.AllowSubscriptionUsage, AllowPotentialMonetarySpend: rp.AllowPotentialMonetarySpend}
	pinSeat := researchMode && (research.PinModels || research.DisableModelSubstitution || research.SameModelRetries)
	var seatBinding team.SeatBinding
	if pinSeat {
		if b, err := s.teams.SeatBinding(ctx, ss.ID, member.ID); err == nil {
			seatBinding = b
			request.IncludeCandidateIDs = []string{b.CandidateID}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return s.fail(ctx, res, err)
		}
	}
	decision, err := s.scheduler.Route(ctx, request)
	if err != nil || decision.Selected == nil {
		if err == nil {
			err = scheduler.ErrNoEligibleCandidate
		}
		if researchMode {
			return s.pauseResearch(ctx, res, ss, turnID, err)
		}
		return s.block(ctx, res, err)
	}
	cand := decision.Selected.Candidate
	if pinSeat && seatBinding.CandidateID == "" {
		raw,_:=json.Marshal(map[string]any{"id":cand.ID,"kind":cand.Kind,"display_name":cand.DisplayName,"provider":cand.Provider,"node_id":cand.NodeID,"compute_mode":cand.ComputeMode,"runtime_backend":cand.RuntimeBackend,"local":cand.Local,"cost_class":cand.CostClass,"qualification":cand.Qualification,"protocol_level":cand.ProtocolLevel,"context_max":cand.ContextMax,"metadata":cand.Metadata})
		bound, bindErr := s.teams.BindSeat(ctx, ss.ID, member.ID, string(cand.Kind), cand.ID, raw)
		if bindErr != nil {
			return s.fail(ctx, res, bindErr)
		}
		seatBinding = bound
		if bound.CandidateID != cand.ID {
			request.IncludeCandidateIDs = []string{bound.CandidateID}
			decision, err = s.scheduler.Route(ctx, request)
			if err != nil || decision.Selected == nil {
				if err == nil { err = scheduler.ErrNoEligibleCandidate }
				if researchMode {
					return s.pauseResearch(ctx, res, ss, turnID, err)
				}
				return s.block(ctx, res, err)
			}
			cand = decision.Selected.Candidate
		}
	}
	kind, cid := string(cand.Kind), cand.ID
	_, _ = s.db.ExecContext(ctx, `UPDATE team_turn_requests SET selected_candidate_kind=?,selected_candidate_id=?,updated_at=? WHERE id=?`, kind, cid, s.clock.UnixMilli(), turnID)
	reservationID, err := s.reserve(ctx, ws, ss.TaskID, rp, cand, int64((used+3)/4))
	if err != nil {
		return s.block(ctx, res, err)
	}
	if reservationID != "" {
		_, _ = s.db.ExecContext(ctx, `UPDATE team_turn_requests SET budget_reservation_id=? WHERE id=?`, reservationID, turnID)
	}
	objective := fmt.Sprintf("Participate in a pre-execution Team Mode deliberation as %s. Respond to the team's latest discussion, surface assumptions or objections, and help converge on a concrete plan. Do not execute tools or side effects. Task: %s", member.RoleName, taskObjective)
	if executionMode == "council" {
		switch researchPhase {
		case team.ResearchPhaseIndependent:
			objective = fmt.Sprintf("Research Council independent pass as %s. Produce your own assessment without seeing or converging on other Council seats. Surface assumptions, evidence, objections, risks, uncertainty, and a recommended answer for later critique and synthesis. Do not execute tools or side effects. Task: %s", member.RoleName, taskObjective)
		case team.ResearchPhaseCritique:
			objective = fmt.Sprintf("Research Council anonymized cross-critique round %d of %d as %s. Critically evaluate prior-round assessments, identify contradictions, unsupported claims, evidence gaps and hidden assumptions, then revise your recommendation where warranted. Do not infer anonymous peer identities. Do not execute tools or side effects. Task: %s", turnRound-1, research.CritiqueRounds, member.RoleName, taskObjective)
		case team.ResearchPhaseSynthesis:
			objective = fmt.Sprintf("Research Council final synthesis as %s. Integrate independent assessments and critique rounds into one final answer. Preserve material disagreements, unresolved uncertainty and minority findings rather than forcing artificial consensus. Do not execute tools or side effects. Task: %s", member.RoleName, taskObjective)
		default:
			objective = fmt.Sprintf("Participate independently in a pre-execution Council deliberation as %s. Produce an evidence-aware assessment for later synthesis. Do not execute tools or side effects. Task: %s", member.RoleName, taskObjective)
		}
	}
	sectionsRaw,_:=json.Marshal(sections)
	contextSum:=sha256.Sum256(sectionsRaw)
	contextHash:=hex.EncodeToString(contextSum[:])
	manifestPayload:=map[string]any{"session_id":ss.ID,"message_count":len(sections)-1,"bytes":used,"round":turnRound,"research_phase":researchPhase,"research_total_rounds":team.ResearchTotalRounds(research),"critique_rounds":research.CritiqueRounds,"context_sha256":contextHash,"research_mode":researchMode,"candidate_id":cand.ID,"candidate_kind":cand.Kind,"profile_id":profileID,"profile_revision":profileRevision}
	if hasManifest { manifestPayload["session_manifest_sha256"]=manifestRecord.SnapshotSHA256 }
	if seatBinding.CandidateID!="" { manifestPayload["seat_binding_candidate_id"]=seatBinding.CandidateID }
	manifest,_:=json.Marshal(manifestPayload)
	areq := agentprotocol.Request{ProtocolVersion: agentprotocol.Version, RequestID: "", WorkspaceID: ws, TaskID: ss.TaskID, PrincipalID: WorkerPrincipal, Role: member.RoleName, Objective: objective, Constraints: json.RawMessage(`{"deliberation_only":true,"no_side_effects":true}`), Context: sections, ContextManifest: manifest, PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalReplan, agentprotocol.ProposalHuman, agentprotocol.ProposalComplete, agentprotocol.ProposalWait, agentprotocol.ProposalFail}, ToolCallback: false}
	response, err := s.dispatch(ctx, areq, cand, label, reservationID)
	if err != nil {
		s.release(ctx, reservationID)
		if researchMode {
			return s.pauseResearch(ctx, res, ss, turnID, err)
		}
		return s.fail(ctx, res, err)
	}
	content, _ := json.Marshal(map[string]any{"text": response.Message, "proposal_type": response.ProposalType, "proposal": json.RawMessage(response.Proposal), "candidate_kind": kind, "candidate_id": cid, "research_phase": researchPhase, "round": turnRound})
	msg, err := s.teams.PostMessage(ctx, team.PostMessageCommand{SessionID: ss.ID, AuthorPrincipalID: WorkerPrincipal, AuthorMemberID: &member.ID, Kind: "agent", Content: content})
	if err != nil {
		return s.fail(ctx, res, err)
	}
	completed := s.clock.UnixMilli()
	_, err = s.db.ExecContext(ctx, `UPDATE team_turn_requests SET status='succeeded',response_message_id=?,updated_at=?,completed_at=? WHERE id=? AND status='running'`, msg.ID, completed, completed, turnID)
	if err != nil {
		return s.fail(ctx, res, err)
	}
	res.Status = "succeeded"
	return res
}


func researchAuthorAlias(snapshot team.SessionSnapshot, memberID *string) string {
	if memberID == nil || strings.TrimSpace(*memberID) == "" {
		return "Anonymous source"
	}
	for i, m := range snapshot.Members {
		if m.ID == *memberID {
			return fmt.Sprintf("Source %d", i+1)
		}
	}
	return "Anonymous source"
}

func researchSynthesisMember(snapshot team.SessionSnapshot, research team.ResearchSettings) string {
	if strings.TrimSpace(research.SynthesisMemberID) != "" {
		for _, m := range snapshot.Members {
			if m.ID == research.SynthesisMemberID && m.Status == "active" && m.MemberKind != "human" {
				return m.ID
			}
		}
	}
	for _, m := range snapshot.Members {
		label := strings.ToLower(strings.Join([]string{m.DisplayName, m.RoleName}, " "))
		if m.Profile != nil {
			label += " " + strings.ToLower(m.Profile.Name+" "+m.Profile.Role)
		}
		if m.Status == "active" && m.MemberKind != "human" && strings.Contains(label, "synth") {
			return m.ID
		}
	}
	for _, m := range snapshot.Members {
		if m.Status == "active" && m.MemberKind != "human" {
			return m.ID
		}
	}
	return ""
}

func retryAfterFromProviderError(err error) *int64 {
	var te *inference.TransportError
	if errors.As(err, &te) && te.RetryAfterMS != nil {
		v := *te.RetryAfterMS
		return &v
	}
	return nil
}

func (s *Service) pauseResearch(ctx context.Context, res Result, ss team.Session, turnID string, cause error) Result {
	retryAfter := retryAfterFromProviderError(cause)
	_, err := s.teams.PauseResearchRound(ctx, team.PauseResearchRoundCommand{
		SessionID: ss.ID, TurnID: turnID, ActorPrincipalID: WorkerPrincipal,
		Reason: cause.Error(), RetryAfter: retryAfter,
	})
	if err != nil {
		return s.fail(ctx, res, fmt.Errorf("pause research round after provider failure: %w", err))
	}
	res.Status = "paused"
	res.Error = cause.Error()
	return res
}

func (s *Service) advanceResearchSessions(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.workspace_id,s.status,s.round_number,m.snapshot_json
		FROM team_sessions s
		JOIN team_session_manifests m ON m.session_id=s.id
		WHERE m.research_mode=1 AND m.execution_mode='council'
		AND s.status IN ('deliberating','paused_for_deliberation')
		ORDER BY s.created_at,s.id LIMIT 64`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type candidate struct {
		id, ws, status, raw string
		round int64
	}
	var sessions []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.ws, &c.status, &c.round, &c.raw); err != nil {
			return err
		}
		sessions = append(sessions, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range sessions {
		if err := s.ensureWorkspace(ctx, c.ws); err != nil {
			return err
		}
		var snapshot team.SessionSnapshot
		if err := json.Unmarshal([]byte(c.raw), &snapshot); err != nil {
			return fmt.Errorf("decode research workflow snapshot %s: %w", c.id, err)
		}
		research := team.NormalizeResearchSettings(snapshot.Research)
		totalRounds := team.ResearchTotalRounds(research)

		if c.status == "paused_for_deliberation" {
			// Auto-resume only when the provider gave an explicit reset time.
			// Otherwise this remains parked until the user presses Retry this round.
			_, err := s.teams.RetryResearchRound(ctx, team.RetryResearchRoundCommand{
				SessionID: c.id, RequestedByPrincipalID: WorkerPrincipal, Automatic: true,
			})
			if err != nil && !errors.Is(err, team.ErrSessionState) {
				return err
			}
			continue
		}

		if c.round == 0 {
			if research.ChairMode=="manual" {
				approved,err:=s.ensureManualChair(ctx,snapshot,c.ws,"agenda",0)
				if err!=nil{return err}
				if !approved {continue} // Never start independent research before the Chair agenda gate.
			}
			if _, err := s.teams.RequestRound(ctx, team.RequestRoundCommand{
				SessionID: c.id, RequestedByPrincipalID: WorkerPrincipal,
				ResearchPhase: team.ResearchPhaseIndependent,
			}); err != nil && !errors.Is(err, team.ErrSessionState) {
				return err
			}
			continue
		}

		var pending, running, blocked, failed, succeeded int
		if err := s.db.QueryRowContext(ctx, `SELECT
			COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status='running' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status='blocked' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN status='succeeded' THEN 1 ELSE 0 END),0)
			FROM team_turn_requests WHERE session_id=? AND round_number=?`,
			c.id, c.round).Scan(&pending, &running, &blocked, &failed, &succeeded); err != nil {
			return err
		}
		if pending > 0 || running > 0 || blocked > 0 || failed > 0 {
			continue
		}
		if succeeded == 0 {
			continue
		}
		if c.round >= totalRounds {
			now := s.clock.UnixMilli()
			_, err := s.db.ExecContext(ctx, `UPDATE team_sessions SET status='team_review',revision=revision+1,updated_at=? WHERE id=? AND status='deliberating'`, now, c.id)
			if err != nil {
				return err
			}
			continue
		}

		if research.ChairMode=="manual" {
			approved,err:=s.ensureManualChair(ctx,snapshot,c.ws,"review",c.round)
			if err!=nil{return err}
			if !approved {continue} // Await Chair critique proposal and optional human approval.
		}
		nextRound := c.round + 1
		phase := team.ResearchPhaseForRound(research, nextRound)
		cmd := team.RequestRoundCommand{
			SessionID: c.id, RequestedByPrincipalID: WorkerPrincipal, ResearchPhase: phase,
		}
		if phase == team.ResearchPhaseSynthesis {
			memberID := researchSynthesisMember(snapshot, research)
			if memberID == "" {
				return fmt.Errorf("research council %s has no eligible synthesis member", c.id)
			}
			cmd.MemberIDs = []string{memberID}
			cmd.ExactMemberSelection = true
		}
		if _, err := s.teams.RequestRound(ctx, cmd); err != nil && !errors.Is(err, team.ErrSessionState) {
			return err
		}
	}
	return nil
}

func (s *Service) dispatch(ctx context.Context, req agentprotocol.Request, c scheduler.Candidate, label policy.DataLabel, reservationID string) (agentprotocol.Response, error) {
	if c.Kind == scheduler.CandidateAgentRuntime {
		taskID := req.TaskID
		out, err := s.runtimes.Invoke(ctx, agentruntime.InvokeCommand{WorkspaceID: req.WorkspaceID, TaskID: &taskID, PrincipalID: WorkerPrincipal, ConnectionID: c.ID, Role: req.Role, Objective: req.Objective, Constraints: req.Constraints, Context: req.Context, ContextManifest: req.ContextManifest, PermittedProposalTypes: req.PermittedProposalTypes, InputLabel: label, ActorPrincipalID: strptr(WorkerPrincipal), BudgetReservationID: optional(reservationID)})
		if err != nil {
			return agentprotocol.Response{}, err
		}
		if out.Response == nil {
			return agentprotocol.Response{}, fmt.Errorf("runtime returned no response")
		}
		return *out.Response, nil
	}
	reqID, _ := s.ids.New("teamreq")
	req.RequestID = reqID
	body, err := agentworker.BuildModelRequest(req, c.ProtocolLevel)
	if err != nil {
		return agentprotocol.Response{}, err
	}
	capJSON, _ := json.Marshal(map[string]any{"capability_id": "team.deliberate", "role": req.Role, "protocol_level": c.ProtocolLevel, "agent_protocol": agentprotocol.Version})
	taskID := req.TaskID
	q, err := s.inference.Execute(ctx, inference.ExecuteCommand{WorkspaceID: req.WorkspaceID, TaskID: &taskID, PrincipalID: WorkerPrincipal, DeploymentID: c.ID, CapabilityJSON: capJSON, ContextManifestJSON: req.ContextManifest, ClassificationMetadata: json.RawMessage(`{"team_mode":true,"deliberation_only":true}`), InputLabel: label, RequestJSON: body, ActorPrincipalID: strptr(WorkerPrincipal), BudgetReservationID: optional(reservationID)})
	if err != nil {
		return agentprotocol.Response{}, err
	}
	if q.ResponseArtifactID == nil {
		return agentprotocol.Response{}, fmt.Errorf("inference returned no artifact")
	}
	if err := s.artifacts.VerifyContent(ctx, *q.ResponseArtifactID); err != nil {
		return agentprotocol.Response{}, err
	}
	rc, _, err := s.artifacts.Open(ctx, *q.ResponseArtifactID)
	if err != nil {
		return agentprotocol.Response{}, err
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, 32<<20))
	if err != nil {
		return agentprotocol.Response{}, err
	}
	return agentworker.DecodeModelResponse(raw, req)
}
func (s *Service) reserve(ctx context.Context, ws, taskID string, rp routePolicy, c scheduler.Candidate, tokens int64) (string, error) {
	if c.CostClass == scheduler.CostLocal || (c.CostClass == scheduler.CostFree && c.HardZeroIncrementalCost) {
		return "", nil
	}
	if s.budgets == nil {
		return "", inference.ErrBudgetUnavailable
	}
	if strings.TrimSpace(rp.BudgetAccountID) == "" {
		return "", inference.ErrBudgetRequired
	}
	a, err := s.budgets.Account(ctx, rp.BudgetAccountID)
	if err != nil {
		return "", err
	}
	if a.WorkspaceID == nil || *a.WorkspaceID != ws {
		return "", budget.ErrWorkspaceMismatch
	}
	amount := rp.BudgetReserveAmount
	if amount <= 0 {
		switch strings.ToLower(a.Unit) {
		case "request", "requests", "subscription_request", "subscription_requests":
			amount = 1
		case "token", "tokens":
			amount = tokens + 4096
		default:
			return "", fmt.Errorf("budget account unit %q requires budget_reserve_amount", a.Unit)
		}
	}
	r, err := s.budgets.Reserve(ctx, budget.ReserveCommand{WorkspaceID: ws, AccountID: a.ID, TaskID: &taskID, Amount: amount, TTLMillis: int64((10 * time.Minute) / time.Millisecond), ActorPrincipalID: strptr(WorkerPrincipal)})
	if err != nil {
		return "", err
	}
	return r.ID, nil
}
func (s *Service) release(ctx context.Context, idv string) {
	if idv == "" || s.budgets == nil {
		return
	}
	r, err := s.budgets.Reservation(ctx, idv)
	if err == nil && r.Status == budget.ReservationReserved {
		_, _ = s.budgets.Release(ctx, budget.CloseCommand{ReservationID: idv, ActorPrincipalID: strptr(WorkerPrincipal)})
	}
}
func (s *Service) fail(ctx context.Context, r Result, err error) Result {
	now := s.clock.UnixMilli()
	_, _ = s.db.ExecContext(ctx, `UPDATE team_turn_requests SET status='failed',error_text=?,updated_at=?,completed_at=? WHERE id=?`, bound(err.Error(), 2048), now, now, r.TurnID)
	r.Status = "failed"
	r.Error = err.Error()
	return r
}
func (s *Service) block(ctx context.Context, r Result, err error) Result {
	now := s.clock.UnixMilli()
	_, _ = s.db.ExecContext(ctx, `UPDATE team_turn_requests SET status='blocked',error_text=?,updated_at=?,completed_at=? WHERE id=?`, bound(err.Error(), 2048), now, now, r.TurnID)
	r.Status = "blocked"
	r.Error = err.Error()
	return r
}
func bound(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}
func strptr(v string) *string { return &v }
func optional(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}
