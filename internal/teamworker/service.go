package teamworker

import (
	"context"
	"database/sql"
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
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,session_id,member_id FROM team_turn_requests WHERE status='pending' ORDER BY created_at,id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type q struct{ id, ws, session, member string }
	var qs []q
	for rows.Next() {
		var v q
		if err := rows.Scan(&v.id, &v.ws, &v.session, &v.member); err != nil {
			return nil, err
		}
		qs = append(qs, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(qs))
	for _, v := range qs {
		out = append(out, s.process(ctx, v.id, v.ws, v.session, v.member))
	}
	_ = s.teams.SyncTaskStates(ctx)
	return out, nil
}

func (s *Service) process(ctx context.Context, turnID, ws, sessionID, memberID string) Result {
	res := Result{TurnID: turnID, SessionID: sessionID, MemberID: memberID, Status: "failed"}
	if err := s.ensureWorkspace(ctx, ws); err != nil {
		res.Error = err.Error()
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
	ss, err := s.teams.Session(ctx, sessionID)
	if err != nil {
		return s.fail(ctx, res, err)
	}
	if ss.WorkspaceID != ws {
		return s.fail(ctx, res, team.ErrWorkspaceMismatch)
	}
	members, err := s.teams.ListMembers(ctx, ss.TeamID)
	if err != nil {
		return s.fail(ctx, res, err)
	}
	var member *team.Member
	for i := range members {
		if members[i].ID == memberID {
			member = &members[i]
			break
		}
	}
	if member == nil || member.Status != "active" || member.MemberKind == "human" {
		return s.fail(ctx, res, fmt.Errorf("ineligible team member"))
	}
	var taskObjective string
	if err := s.db.QueryRowContext(ctx, `SELECT objective FROM tasks WHERE id=?`, ss.TaskID).Scan(&taskObjective); err != nil {
		return s.fail(ctx, res, err)
	}
	var executionMode string
	if err := s.db.QueryRowContext(ctx, `SELECT execution_mode FROM task_execution_profiles WHERE task_id=?`, ss.TaskID).Scan(&executionMode); err != nil {
		return s.fail(ctx, res, err)
	}
	if executionMode != "council" {
		executionMode = "team"
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
	if err:=s.db.QueryRowContext(ctx,`SELECT name,instructions_md,default_role,revision FROM agent_profiles WHERE id=? AND status='active' AND (workspace_id=? OR workspace_id IS NULL) ORDER BY CASE WHEN workspace_id=? THEN 0 ELSE 1 END LIMIT 1`,profileID,ws,ws).Scan(&profileName,&profileInstructions,&profileRole,&profileRevision);err==nil{
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
		trust := "UNVERIFIED_DERIVED"
		if m.Kind == "human" {
			trust = "USER_INSTRUCTION"
		}
		raw, _ := json.Marshal(map[string]any{"kind": m.Kind, "content": json.RawMessage(m.Content), "round": m.RoundNumber, "author_member_id": m.AuthorMemberID})
		if used+len(raw) > 96<<10 {
			continue
		}
		used += len(raw)
		sections = append(sections, agentprotocol.ContextSection{ID: "msg-" + m.ID, Kind: "team_message", Trust: trust, Authoritative: m.Kind == "human", Content: raw})
	}
	manifest, _ := json.Marshal(map[string]any{"session_id": ss.ID, "message_count": len(sections) - 1, "bytes": used, "round": ss.RoundNumber})
	rp := routePolicy{PreferZeroIncrementalCost: true, AllowMediated: true, AllowDegraded: true}
	_ = json.Unmarshal(member.RoutePolicy, &rp)
	label := policy.DataLabel{WorkspaceID: ws, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction}
	decision, err := s.scheduler.Route(ctx, scheduler.RouteRequest{WorkspaceID: ws, CapabilityID: member.CapabilityID, RoleName: member.RoleName, ProtocolLevel: member.ProtocolLevel, ContextTokens: int64((used + 3) / 4), DataLabel: label, AllowUntested: rp.AllowUntested, AllowLimited: rp.AllowLimited, AllowMediated: rp.AllowMediated, AllowDegraded: rp.AllowDegraded, RequireZeroIncrementalCost: rp.RequireZeroIncrementalCost, PreferZeroIncrementalCost: rp.PreferZeroIncrementalCost, AllowSubscriptionUsage: rp.AllowSubscriptionUsage, AllowPotentialMonetarySpend: rp.AllowPotentialMonetarySpend})
	if err != nil || decision.Selected == nil {
		if err == nil {
			err = scheduler.ErrNoEligibleCandidate
		}
		return s.block(ctx, res, err)
	}
	cand := decision.Selected.Candidate
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
		objective = fmt.Sprintf("Participate independently in a pre-execution Council deliberation as %s. Produce your own assessment without converging on other members' conclusions; surface assumptions, objections, risks, and a recommended answer for later synthesis. Do not execute tools or side effects. Task: %s", member.RoleName, taskObjective)
	}
	areq := agentprotocol.Request{ProtocolVersion: agentprotocol.Version, RequestID: "", WorkspaceID: ws, TaskID: ss.TaskID, PrincipalID: WorkerPrincipal, Role: member.RoleName, Objective: objective, Constraints: json.RawMessage(`{"deliberation_only":true,"no_side_effects":true}`), Context: sections, ContextManifest: manifest, PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalReplan, agentprotocol.ProposalHuman, agentprotocol.ProposalComplete, agentprotocol.ProposalWait, agentprotocol.ProposalFail}, ToolCallback: false}
	response, err := s.dispatch(ctx, areq, cand, label, reservationID)
	if err != nil {
		s.release(ctx, reservationID)
		return s.fail(ctx, res, err)
	}
	content, _ := json.Marshal(map[string]any{"text": response.Message, "proposal_type": response.ProposalType, "proposal": json.RawMessage(response.Proposal), "candidate_kind": kind, "candidate_id": cid})
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
