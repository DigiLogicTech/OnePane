package agentworker

import (
	"bytes"
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
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/contextcompiler"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/observation"
	"github.com/DigiLogicTech/OnePane/internal/operation"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/tool"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

func (s *Service) step(ctx context.Context, run Run) TickResult {
	res := TickResult{TaskID: run.TaskID, RunID: run.ID, Status: "running"}
	current, err := s.getRun(ctx, run.ID)
	if err != nil {
		return failedResult(res, err)
	}
	run = current
	t, err := s.tasks.Get(ctx, run.TaskID)
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	if run.Status != RunRunning || t.State != task.StateRunning {
		return failedResult(res, ErrInvalidWorkerState)
	}
	if run.StepCount >= run.MaxSteps {
		return s.failRun(ctx, run, res, ErrStepLimit)
	}

	compiled, err := s.compileContext(ctx, run, t)
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	rp := defaultRoutePolicy()
	_ = json.Unmarshal(run.RoutePolicy, &rp)
	label := policy.DataLabel{WorkspaceID: run.WorkspaceID, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction}
	tokens := int64((compiled.Manifest.UsedBytes + 3) / 4)
	if tokens < 1 {
		tokens = 1
	}
	decision, err := s.scheduler.Route(ctx, scheduler.RouteRequest{WorkspaceID: run.WorkspaceID, CapabilityID: run.CapabilityID, RoleName: run.RoleName, ProtocolLevel: run.ProtocolLevel, ContextTokens: tokens, DataLabel: label, AllowUntested: rp.AllowUntested, AllowLimited: rp.AllowLimited, AllowMediated: rp.AllowMediated, AllowDegraded: rp.AllowDegraded, RequireZeroIncrementalCost: rp.RequireZeroIncrementalCost, PreferZeroIncrementalCost: rp.PreferZeroIncrementalCost, AllowSubscriptionUsage: rp.AllowSubscriptionUsage, AllowPotentialMonetarySpend: rp.AllowPotentialMonetarySpend, LocalOnly: !rp.AllowRemote, IncludeCandidateIDs: rp.IncludeCandidateIDs, ExcludeCandidateIDs: rp.ExcludedCandidateIDs})
	if err != nil || decision.Selected == nil {
		if err == nil {
			err = scheduler.ErrNoEligibleCandidate
		}
		_ = s.journal(ctx, run.ID, "route", "failed", nil, nil, nil, nil, map[string]any{"error": err.Error(), "rejected": decision.Rejected})
		if shouldWaitForLocalModel(t,err,decision.Rejected) {
			return s.waitForLocalModel(ctx,run,res,"awaiting qualified local model: "+err.Error())
		}
		return s.blockRun(ctx, run, res, "no eligible inference/agent runtime: "+err.Error())
	}
	cand := decision.Selected.Candidate
	kind := string(cand.Kind)
	cid := cand.ID
	_ = s.journal(ctx, run.ID, "route", "succeeded", &kind, &cid, nil, nil, map[string]any{"score": decision.Selected.Score, "protocol_level": cand.ProtocolLevel, "cost_class": cand.CostClass, "qualification": cand.Qualification})
	reservationID, err := s.reserveReasoningBudget(ctx, run, rp, cand, tokens)
	if err != nil {
		_ = s.journal(ctx, run.ID, "budget", "failed", &kind, &cid, nil, nil, map[string]any{"error": err.Error(), "cost_class": cand.CostClass})
		return s.blockRun(ctx, run, res, "budget unavailable: "+err.Error())
	}
	if reservationID != "" {
		_ = s.journal(ctx, run.ID, "budget", "reserved", &kind, &cid, nil, &reservationID, map[string]any{"budget_account_id": rp.BudgetAccountID})
	}
	if err := s.markReasoningDispatch(ctx, &run, kind, cid); err != nil {
		s.releaseBudgetIfReserved(ctx, reservationID)
		return s.failRun(ctx, run, res, err)
	}

	constraints, _ := json.Marshal(map[string]any{"completion": json.RawMessage(t.Completion), "worker": map[string]any{"max_steps": run.MaxSteps, "steps_used": run.StepCount, "max_replans": run.MaxReplans, "replans_used": run.ReplanCount, "max_escalations": run.MaxEscalations, "escalations_used": run.EscalationCount}})
	response, ref, dispatchErr := s.dispatch(ctx, run, t, cand, compiled, constraints, label, reservationID)
	stepKind := "model"
	if cand.Kind == scheduler.CandidateAgentRuntime {
		stepKind = "agent_runtime"
	}
	if dispatchErr != nil {
		s.releaseBudgetIfReserved(ctx, reservationID)
		_ = s.journal(ctx, run.ID, stepKind, "failed", &kind, &cid, nil, &ref, map[string]any{"error": dispatchErr.Error()})
		return s.handleDispatchFailure(ctx, run, res, cid, dispatchErr)
	}
	pt := string(response.ProposalType)
	res.Proposal = pt
	_ = s.journal(ctx, run.ID, stepKind, "succeeded", &kind, &cid, &pt, &ref, map[string]any{"message": boundedString(response.Message, 1024)})
	return s.handleProposal(ctx, run, t, response, res)
}

func (s *Service) compileContext(ctx context.Context, run Run, t task.Task) (contextcompiler.Result, error) {
	taskRaw, _ := json.Marshal(map[string]any{"task_id": t.ID, "objective": t.Objective, "completion": json.RawMessage(t.Completion), "project_id": t.ProjectID, "scheduling_class": t.SchedulingClass, "priority": t.Priority})
	sections := []contextcompiler.Section{{ID: "task-current", Kind: "task", Trust: "USER_INSTRUCTION", Authoritative: true, Required: true, Priority: 100, Content: taskRaw}}
	routing := routingPolicyFromCompletion(t.Completion)
	profileID := strings.TrimSpace(routing.AgentProfile)
	if profileID == "" || strings.EqualFold(profileID, "onepane-default") { profileID = "agent.md" }
	var profileName, profileInstructions, profileRole string
	var profileRevision int64
	if err := s.db.QueryRowContext(ctx, `SELECT name,instructions_md,default_role,revision FROM agent_profiles WHERE id=? AND status='active' AND (workspace_id=? OR workspace_id IS NULL) ORDER BY CASE WHEN workspace_id=? THEN 0 ELSE 1 END LIMIT 1`, profileID, run.WorkspaceID, run.WorkspaceID).Scan(&profileName,&profileInstructions,&profileRole,&profileRevision); err == nil {
		profileRaw,_:=json.Marshal(map[string]any{"profile_id":profileID,"name":profileName,"role":profileRole,"revision":profileRevision,"instructions":profileInstructions,"authority":false,"note":"Profile instructions affect reasoning only and grant no capabilities or permissions."})
		sections=append(sections,contextcompiler.Section{ID:"agent-profile",Kind:"agent_profile",Trust:"USER_INSTRUCTION",Authoritative:false,Required:true,Priority:99,Content:profileRaw})
	} else if !errors.Is(err,sql.ErrNoRows) { return contextcompiler.Result{},err }
	// Resource backoff is internal scheduler bookkeeping, not an instruction
	// to the model. Once it becomes available, don't tell the model that it is
	// still unavailable or let historical wait data bias tool selection.
	if len(run.Continuation) > 0 && string(run.Continuation) != "{}" && decodeModelWait(run.Continuation)==nil {
		sections = append(sections, contextcompiler.Section{ID: "worker-continuation", Kind: "continuation", Trust: "UNVERIFIED_DERIVED", Authoritative: false, Required: false, Priority: 80, Content: run.Continuation})
	}
	if cp, err := s.verification.LatestValidCheckpoint(ctx, t.ID); err == nil {
		sections = append(sections, contextcompiler.Section{ID: "verified-checkpoint", Kind: "checkpoint", Trust: "VERIFIED_DERIVED", Authoritative: true, Required: false, Priority: 90, Content: cp.State})
	} else if !errors.Is(err, sql.ErrNoRows) {
		return contextcompiler.Result{}, err
	}
	// Team Mode execution is bound to the exact human-accepted TeamPlan. The
	// deliberation transcript remains derived context; only the accepted plan is
	// authoritative for execution.
	var teamPlanID, teamPlanJSON string
	if err := s.db.QueryRowContext(ctx, `SELECT p.id,p.plan_json FROM task_execution_profiles ep JOIN team_sessions ts ON ts.id=ep.team_session_id JOIN team_plans p ON p.id=ts.accepted_plan_id WHERE ep.task_id=? AND ep.execution_mode='team' AND p.status='accepted'`, t.ID).Scan(&teamPlanID, &teamPlanJSON); err == nil {
		raw, _ := json.Marshal(map[string]any{"team_plan_id": teamPlanID, "plan": json.RawMessage(teamPlanJSON)})
		sections = append(sections, contextcompiler.Section{ID: "team-plan-accepted", Kind: "team_plan", Trust: "AUTHORITATIVE_DATA", Authoritative: true, Required: true, Priority: 95, Content: raw})
	} else if !errors.Is(err, sql.ErrNoRows) {
		return contextcompiler.Result{}, err
	}
	if t.ProjectID != nil {
		var name, status string
		if err := s.db.QueryRowContext(ctx, `SELECT name,status FROM projects WHERE id=?`, *t.ProjectID).Scan(&name, &status); err == nil {
			raw, _ := json.Marshal(map[string]any{"project_id": *t.ProjectID, "name": name, "status": status})
			sections = append(sections, contextcompiler.Section{ID: "project-current", Kind: "project", Trust: "AUTHORITATIVE_DATA", Authoritative: true, Priority: 70, Content: raw})
		}
	}
	// Models must not guess a runtime/application identity. The manifest is
	// populated from the exact persisted Project Workspace and contains only
	// read-only OCI tool metadata; capability leases and independent runtime
	// verification remain mandatory before every command.
	if t.ProjectWorkspaceID != nil {
		manifest,err:=workspaceExecutionManifest(ctx,s.db,t)
		if err!=nil{return contextcompiler.Result{},err}
		if len(manifest)>0{
			sections=append(sections,contextcompiler.Section{
				ID:"workspace-execution-manifest",Kind:"resource_manifest",
				Trust:"AUTHORITATIVE_DATA",Authoritative:true,
				Required:false,Priority:75,Content:manifest,
			})
		}
	}
	return contextcompiler.Compile(s.cfg.ContextMaxBytes, sections)
}

func (s *Service) dispatch(ctx context.Context, run Run, t task.Task, c scheduler.Candidate, compiled contextcompiler.Result, constraints json.RawMessage, label policy.DataLabel, reservationID string) (agentprotocol.Response, string, error) {
	if c.Kind == scheduler.CandidateAgentRuntime {
		permitted,_:=proposalsForCandidate(c)
		taskID, attemptID := t.ID, run.AttemptID
		result, err := s.runtimes.Invoke(ctx, agentruntime.InvokeCommand{WorkspaceID: run.WorkspaceID, TaskID: &taskID, AttemptID: &attemptID, PrincipalID: WorkerPrincipal, ConnectionID: c.ID, Role: run.RoleName, Objective: t.Objective, Constraints: constraints, Context: compiled.Sections, ContextManifest: compiled.ManifestJSON, PermittedProposalTypes: permitted, InputLabel: label, ActorPrincipalID: strPtr(WorkerPrincipal), BudgetReservationID: optionalString(reservationID)})
		if err != nil {
			return agentprotocol.Response{}, result.Invocation.ID, err
		}
		if result.Response == nil {
			return agentprotocol.Response{}, result.Invocation.ID, fmt.Errorf("external runtime returned no AgentResponse")
		}
		return *result.Response, result.Invocation.ID, nil
	}
	reqID, _ := s.ids.New("agentreq")
	permitted,structuredJSONTools:=proposalsForCandidate(c)
	areq := agentprotocol.Request{ProtocolVersion: agentprotocol.Version, RequestID: reqID, WorkspaceID: run.WorkspaceID, TaskID: t.ID, AttemptID: run.AttemptID, PrincipalID: WorkerPrincipal, Role: run.RoleName, Objective: t.Objective, Constraints: constraints, Context: compiled.Sections, ContextManifest: compiled.ManifestJSON, PermittedProposalTypes: permitted, ToolCallback: c.ToolCallback, JSONToolProposals:structuredJSONTools}
	if err := areq.Validate(); err != nil {
		return agentprotocol.Response{}, "", err
	}
	body, err := buildModelRequest(areq, c.ProtocolLevel)
	if err != nil {
		return agentprotocol.Response{}, "", err
	}
	capJSON, _ := json.Marshal(map[string]any{"capability_id": run.CapabilityID, "role": run.RoleName, "protocol_level": c.ProtocolLevel, "agent_protocol": agentprotocol.Version})
	taskID := t.ID
	q, err := s.inference.Execute(ctx, inference.ExecuteCommand{WorkspaceID: run.WorkspaceID, TaskID: &taskID, PrincipalID: WorkerPrincipal, DeploymentID: c.ID, CapabilityJSON: capJSON, ContextManifestJSON: compiled.ManifestJSON, ClassificationMetadata: json.RawMessage(`{"agent_worker":true}`), InputLabel: label, RequestJSON: body, ActorPrincipalID: strPtr(WorkerPrincipal), BudgetReservationID: optionalString(reservationID)})
	if err != nil {
		return agentprotocol.Response{}, q.ID, err
	}
	if q.ResponseArtifactID == nil {
		return agentprotocol.Response{}, q.ID, fmt.Errorf("inference succeeded without response artifact")
	}
	if err := s.artifacts.VerifyContent(ctx, *q.ResponseArtifactID); err != nil {
		return agentprotocol.Response{}, q.ID, err
	}
	rc, _, err := s.artifacts.Open(ctx, *q.ResponseArtifactID)
	if err != nil {
		return agentprotocol.Response{}, q.ID, err
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, 32<<20))
	if err != nil {
		return agentprotocol.Response{}, q.ID, err
	}
	resp, err := decodeModelResponse(raw, areq)
	if err != nil {
		return agentprotocol.Response{}, q.ID, err
	}
	canonical, _ := json.Marshal(resp)
	outLabel, _ := policy.InheritLabels(label)
	meta, _ := json.Marshal(map[string]any{"inference_request_id": q.ID, "agent_request_id": reqID, "proposal_type": resp.ProposalType})
	a, err := s.artifacts.Create(ctx, artifact.CreateCommand{WorkspaceID: run.WorkspaceID, MediaType: "application/vnd.onepane.agent-response+json", Label: outLabel, Status: artifact.StatusActive, Metadata: meta, CreatedBy: strPtr(WorkerPrincipal), ActorPrincipalID: strPtr(WorkerPrincipal)}, bytes.NewReader(canonical))
	if err != nil {
		return agentprotocol.Response{}, q.ID, err
	}
	return resp, a.ID, nil
}

func protectedCandidate(c scheduler.Candidate) bool {
	switch c.CostClass {
	case scheduler.CostLocal:
		return false
	case scheduler.CostFree:
		return !c.HardZeroIncrementalCost
	default:
		return true
	}
}

func optionalString(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

func (s *Service) reserveReasoningBudget(ctx context.Context, run Run, rp routePolicy, c scheduler.Candidate, contextTokens int64) (string, error) {
	if !protectedCandidate(c) {
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
	if a.WorkspaceID == nil || *a.WorkspaceID != run.WorkspaceID {
		return "", budget.ErrWorkspaceMismatch
	}
	amount := rp.BudgetReserveAmount
	if amount <= 0 {
		switch strings.ToLower(a.Unit) {
		case "request", "requests", "subscription_request", "subscription_requests":
			amount = 1
		case "token", "tokens":
			// Agent model requests currently bound output to 2048 tokens. Reserve
			// the compiled prompt estimate plus output ceiling and protocol overhead.
			amount = contextTokens + 4096
		default:
			return "", fmt.Errorf("budget account unit %q requires explicit budget_reserve_amount", a.Unit)
		}
	}
	taskID := run.TaskID
	r, err := s.budgets.Reserve(ctx, budget.ReserveCommand{WorkspaceID: run.WorkspaceID, AccountID: a.ID, TaskID: &taskID, Amount: amount, TTLMillis: int64((10 * time.Minute) / time.Millisecond), ActorPrincipalID: strPtr(AuthorityPrincipal)})
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

func (s *Service) releaseBudgetIfReserved(ctx context.Context, reservationID string) {
	if strings.TrimSpace(reservationID) == "" || s.budgets == nil {
		return
	}
	r, err := s.budgets.Reservation(ctx, reservationID)
	if err == nil && r.Status == budget.ReservationReserved {
		_, _ = s.budgets.Release(ctx, budget.CloseCommand{ReservationID: reservationID, ActorPrincipalID: strPtr(AuthorityPrincipal)})
	}
}

func (s *Service) handleDispatchFailure(ctx context.Context, run Run, res TickResult, candidateID string, cause error) TickResult {
	rp := defaultRoutePolicy()
	_ = json.Unmarshal(run.RoutePolicy, &rp)
	if !rp.RoutingEnabled {
		return s.blockRun(ctx, run, res, "single-path dispatch failed: "+cause.Error())
	}
	if run.EscalationCount < run.MaxEscalations {
		if !contains(rp.ExcludedCandidateIDs, candidateID) {
			rp.ExcludedCandidateIDs = append(rp.ExcludedCandidateIDs, candidateID)
		}
		b, _ := json.Marshal(rp)
		cont, _ := json.Marshal(map[string]any{"last_dispatch_error": boundedString(cause.Error(), 2048), "failed_candidate_id": candidateID, "action": "bounded automatic escalation"})
		if err := s.updateRun(ctx, run.ID, run.Revision, RunRunning, cont, b, 1, 0, nil, nil, nil); err != nil {
			return failedResult(res, err)
		}
		res.Status = "escalating"
		res.Error = cause.Error()
		return res
	}
	return s.blockRun(ctx, run, res, "dispatch failed after bounded escalation: "+cause.Error())
}

func (s *Service) handleProposal(ctx context.Context, run Run, t task.Task, resp agentprotocol.Response, res TickResult) TickResult {
	switch resp.ProposalType {
	case agentprotocol.ProposalTool:
		return s.handleTool(ctx, run, t, resp, res)
	case agentprotocol.ProposalDelegate:
		return s.handleDelegate(ctx, run, t, resp, res)
	case agentprotocol.ProposalReplan:
		return s.handleReplan(ctx, run, resp, res)
	case agentprotocol.ProposalComplete:
		return s.handleComplete(ctx, run, t, resp, res)
	case agentprotocol.ProposalHuman:
		return s.handleHuman(ctx, run, t, resp, res)
	case agentprotocol.ProposalEscalate:
		return s.handleEscalate(ctx, run, resp, res)
	case agentprotocol.ProposalWait:
		return s.handleWait(ctx, run, t, resp, res)
	case agentprotocol.ProposalFail:
		return s.handleFail(ctx, run, t, resp, res)
	default:
		return s.failRun(ctx, run, res, fmt.Errorf("unsupported proposal %s", resp.ProposalType))
	}
}

func (s *Service) handleTool(ctx context.Context, run Run, t task.Task, resp agentprotocol.Response, res TickResult) TickResult {
	var p toolProposal
	if err := json.Unmarshal(resp.Proposal, &p); err != nil || strings.TrimSpace(p.ToolID) == "" || strings.TrimSpace(p.ToolVersion) == "" || strings.TrimSpace(p.ResourceRef) == "" || len(p.Input) == 0 || !json.Valid(p.Input) {
		return s.failRun(ctx, run, res, fmt.Errorf("invalid tool proposal"))
	}
	def, err := s.tools.Definition(p.ToolID, p.ToolVersion)
	if err != nil {
		return s.continueWithError(ctx, run, res, "tool_definition_error", err)
	}
	if err := workspaceToolAllowedForTask(t, def.CapabilityID, def.Mode, p.ToolID, p.ResourceRef); err != nil {
		_ = s.journal(ctx, run.ID, "tool", "denied", nil, nil, strPtr("tool"), nil, map[string]any{"tool_id": p.ToolID, "resource_ref": p.ResourceRef, "reason": err.Error(), "policy": "project_workspace"})
		return s.blockRun(ctx, run, res, "workspace policy denied tool: "+err.Error())
	}
	// The lease can authorize a generic resource pattern, but cannot authorize
	// a model to select another Workspace's runtime, application or OCI image.
	// Check persisted Task ownership before attempting to find/consume a lease.
	if err := enforceSandboxToolOwnership(ctx, s.db, t, p.ToolID, p.ResourceRef, p.Input); err != nil {
		_ = s.journal(ctx, run.ID, "tool", "denied", nil, nil, strPtr("tool"), nil,
			map[string]any{"tool_id": p.ToolID, "resource_ref": p.ResourceRef, "reason": err.Error(), "policy": "sandbox_ownership"})
		return s.blockRun(ctx, run, res, "sandbox scope denied tool: "+err.Error())
	}
	// A human-approved Workspace toolchain cannot be bypassed by leaving
	// required_executables empty or naming another registered OCI application.
	// Validate before consulting any authority lease or invoking a tool.
	if p.ToolID=="project.app.exec"{
		p.Input,err=applyApprovedWorkspaceToolchain(ctx,s.db,t,p.Input)
		if err!=nil{
			_ = s.journal(ctx,run.ID,"tool","denied",nil,nil,strPtr("tool"),nil,
				map[string]any{"tool_id":p.ToolID,"resource_ref":p.ResourceRef,
				"reason":err.Error(),"policy":"approved_workspace_toolchain"})
			return s.blockRun(ctx,run,res,"Workspace toolchain admission denied: "+err.Error())
		}
	}
	leaseID, err := s.findLease(ctx, run.WorkspaceID, run.TaskID, def.CapabilityID, def.Mode, p.ResourceRef)
	if err != nil {
		cont, _ := json.Marshal(map[string]any{"authority_required": map[string]any{"capability_id": def.CapabilityID, "action": def.Mode, "resource_ref": p.ResourceRef, "tool_id": p.ToolID, "tool_version": p.ToolVersion}})
		_ = s.updateRun(ctx, run.ID, run.Revision, RunWaiting, cont, nil, 0, 0, nil, nil, strPtr(err.Error()))
		cur, _ := s.tasks.Get(ctx, t.ID)
		actor := WorkerPrincipal
		_, _ = s.tasks.WaitApproval(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &actor, Reason: "capability lease required for proposed tool"})
		_ = s.journal(ctx, run.ID, "tool", "waiting", nil, nil, strPtr("tool"), nil, map[string]any{"tool_id": p.ToolID, "resource_ref": p.ResourceRef, "reason": "capability_lease_required"})
		res.Status = "waiting_authority"
		res.Error = ErrAuthorityRequired.Error()
		return res
	}
	taskID, attemptID := t.ID, run.AttemptID
	actor := WorkerPrincipal
	if def.Mode == authority.ActionMutate || def.Mode == authority.ActionExternalSend {
		idem := fmt.Sprintf("agentworker:%s:%d:%s:%s", run.ID, run.StepCount, p.ToolID, p.ResourceRef)
		op, err := s.operations.Prepare(ctx, operation.PrepareCommand{WorkspaceID: run.WorkspaceID, TaskID: &taskID, AttemptID: &attemptID, PrincipalID: WorkerPrincipal, CapabilityLeaseID: leaseID, IdempotencyKey: idem, ToolID: p.ToolID, ToolVersion: p.ToolVersion, ResourceRef: p.ResourceRef, Input: p.Input, DesiredState: defaultJSON(p.DesiredState), Precondition: defaultJSON(p.Precondition), Reconciliation: defaultJSON(p.Reconciliation), Compensation: p.Compensation, ActorPrincipalID: &actor})
		if errors.Is(err, operation.ErrApprovalRequired) {
			cont, _ := json.Marshal(map[string]any{"operation_id": op.ID, "state": op.State, "reason": "approval_required"})
			_ = s.updateRun(ctx, run.ID, run.Revision, RunWaiting, cont, nil, 0, 0, nil, nil, nil)
			cur, _ := s.tasks.Get(ctx, t.ID)
			_, _ = s.tasks.WaitApproval(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &actor, Reason: "operation approval required"})
			res.Status = "waiting_approval"
			return res
		}
		if err != nil {
			return s.continueWithError(ctx, run, res, "operation_prepare_failed", err)
		}
		op, err = s.operations.Execute(ctx, operation.ExecuteCommand{OperationID: op.ID, ExpectedRevision: op.Revision, LeaseID: leaseID, Input: p.Input, ActorPrincipalID: &actor})
		detail := map[string]any{"operation_id": op.ID, "state": op.State, "tool_id": p.ToolID, "resource_ref": p.ResourceRef}
		_ = s.journal(ctx, run.ID, "operation", map[bool]string{true: "succeeded", false: "unknown"}[err == nil], nil, nil, strPtr("tool"), strPtr(op.ID), detail)
		cont, _ := json.Marshal(detail)
		if err != nil || op.State == operation.StateUnknownOutcome || op.State == operation.StateBlockedUnknownOutcome {
			_ = s.updateRun(ctx, run.ID, run.Revision, RunBlocked, cont, nil, 0, 0, nil, nil, strPtr(errorText(err, "mutation outcome requires reconciliation")))
			cur, _ := s.tasks.Get(ctx, t.ID)
			_, _ = s.tasks.MarkBlocked(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &actor, Reason: "mutation outcome requires reconciliation"})
			res.Status = "blocked_reconciliation"
			res.Error = errorText(err, "unknown mutation outcome")
			return res
		}
		// A successful mutating adapter response is still only OBSERVING. Independent
		// post-state verification must commit the Operation before the Task can continue.
		// This is a dependency wait (not a terminal block) because a later assurance
		// component can commit the Operation and safely resume this same Attempt.
		_ = s.updateRun(ctx, run.ID, run.Revision, RunWaiting, cont, nil, 0, 0, nil, nil, nil)
		cur, _ := s.tasks.Get(ctx, t.ID)
		_, _ = s.tasks.WaitDependency(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &actor, Reason: "operation requires independent post-state verification"})
		res.Status = "waiting_operation_verification"
		return res
	}
	inv, err := s.tools.Invoke(ctx, tool.InvokeCommand{WorkspaceID: run.WorkspaceID, TaskID: &taskID, AttemptID: &attemptID, PrincipalID: WorkerPrincipal, LeaseID: leaseID, ToolID: p.ToolID, ToolVersion: p.ToolVersion, ResourceRef: p.ResourceRef, Input: p.Input, ActorPrincipalID: &actor})
	if err != nil {
		return s.continueWithError(ctx, run, res, "tool_failed", err)
	}
	adapterID, adapterVersion := inv.AdapterID, inv.AdapterVersion
	obs, err := s.observations.Record(ctx, observation.RecordCommand{WorkspaceID: run.WorkspaceID, SubjectRef: "tool_invocation:" + inv.ID, ObservationType: "agent_tool_result", ProbeToolID: inv.ToolID, ProbeToolVersion: inv.ToolVersion, SourcePrincipalID: &actor, AdapterID: &adapterID, AdapterVersion: &adapterVersion, Value: inv.Result, Label: policy.DataLabel{WorkspaceID: run.WorkspaceID, Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustUnverifiedDerived}, ActorPrincipalID: &actor})
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	if err := s.observations.VerifyIntegrity(ctx, obs.ID); err != nil {
		return s.failRun(ctx, run, res, err)
	}
	cont, _ := json.Marshal(map[string]any{"tool_result": map[string]any{"tool_id": inv.ToolID, "invocation_id": inv.ID, "observation_id": obs.ID, "summary": inv.Summary, "result": boundedToolResult(inv.Result, 16<<10)}})
	if err := s.updateRun(ctx, run.ID, run.Revision, RunRunning, cont, nil, 0, 0, nil, nil, nil); err != nil {
		return failedResult(res, err)
	}
	_ = s.journal(ctx, run.ID, "tool", "succeeded", nil, nil, strPtr("tool"), strPtr(obs.ID), map[string]any{"invocation_id": inv.ID, "observation_id": obs.ID})
	res.Status = "tool_complete"
	return res
}

func (s *Service) handleDelegate(ctx context.Context, run Run, t task.Task, resp agentprotocol.Response, res TickResult) TickResult {
	rp := defaultRoutePolicy()
	_ = json.Unmarshal(run.RoutePolicy, &rp)
	if !rp.AllowDelegation {
		return s.blockRun(ctx, run, res, "delegation is disabled by workspace routing policy")
	}
	var p delegateProposal
	if err := json.Unmarshal(resp.Proposal, &p); err != nil || strings.TrimSpace(p.Objective) == "" {
		return s.failRun(ctx, run, res, fmt.Errorf("invalid delegate proposal"))
	}
	if len(p.Completion) == 0 {
		p.Completion = json.RawMessage(`{}`)
	}
	if !json.Valid(p.Completion) {
		return s.failRun(ctx, run, res, fmt.Errorf("delegate completion JSON invalid"))
	}
	// Canonical ProjectWorkspaceID is an authoritative relational boundary,
	// not merely a model-visible JSON hint. It MUST survive delegation so the
	// child cannot disappear from the Workspace queue or lose OCI tool scoping.
	// Child work inherits the parent's OnePane workspace routing/access policy.
	// This prevents delegated workers from escaping the originating workspace's
	// model-routing, remote-access, sandbox, or secret boundaries.
	p.Completion = inheritOnePaneRouting(t.Completion, p.Completion)
	actor := WorkerPrincipal
	var child task.Task
	// A model response cannot be considered a committed delegation until the
	// entire child/edge/Task/Attempt/Worker/journal checkpoint is durable.
	// Every mutation below is in ONE transaction: crash/retry cannot leave an
	// orphaned child or duplicate its work in another Workspace.
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var observedStatus string
		var revision int64
		if err:=tx.QueryRowContext(ctx,`SELECT status,revision FROM agent_worker_runs
		 WHERE id=? AND task_id=? AND attempt_id=? AND workspace_id=?`,
		 run.ID,t.ID,run.AttemptID,t.WorkspaceID).Scan(&observedStatus,&revision);err!=nil{return err}
		if observedStatus!=string(RunRunning)||revision!=run.Revision {
			return fmt.Errorf("%w: delegation Worker incarnation changed",ErrInvalidWorkerState)
		}
		// Validate persisted ancestry inside the write transaction before
		// allocating any child ID, event, dependency or admission outbox job.
		if err:=verifyDelegationAncestry(ctx,tx,t);err!=nil{return err}
		var e error
		child, e = s.tasks.CreateInTransaction(ctx, tx, task.CreateCommand{
			WorkspaceID:t.WorkspaceID,ProjectID:t.ProjectID,ProjectWorkspaceID:t.ProjectWorkspaceID,
			ArtifactSessionID:t.ArtifactSessionID,PlanID:t.PlanID,ParentTaskID:&t.ID,
			Objective:p.Objective,SchedulingClass:t.SchedulingClass,Priority:t.Priority,
			Completion:p.Completion,ActorPrincipalID:&actor,
		})
		if e!=nil{return e}
		meta,_:=json.Marshal(map[string]any{"source":"agent_delegate","run_id":run.ID})
		if _,e=tx.ExecContext(ctx,`INSERT INTO task_dependencies
		 (task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json)
		 VALUES(?,?,'hard','all',?)`,t.ID,child.ID,string(meta));e!=nil{return e}
		if e=s.tasks.WaitDependencyInTransaction(ctx,tx,task.TransitionCommand{
			TaskID:t.ID,ExpectedRevision:t.Revision,ActorPrincipalID:&actor,
			Reason:"waiting for delegated child task "+child.ID,
		});e!=nil{return e}
		cont,_:=json.Marshal(map[string]any{"delegated_task_id":child.ID,"objective":child.Objective})
		now:=s.clock.UnixMilli()
		updated,e:=tx.ExecContext(ctx,`UPDATE agent_worker_runs
		 SET status='waiting',continuation_json=?,last_error=NULL,revision=revision+1,updated_at=?
		 WHERE id=? AND task_id=? AND attempt_id=? AND workspace_id=?
		 AND status='running' AND revision=?`,string(cont),now,
		 run.ID,t.ID,run.AttemptID,t.WorkspaceID,run.Revision)
		if e!=nil{return e}
		n,e:=updated.RowsAffected()
		if e!=nil{return e}
		if n!=1{return fmt.Errorf("%w: concurrent Worker delegation",ErrInvalidWorkerState)}
		return s.journalInTransaction(ctx,tx,run.ID,"delegate","waiting",nil,nil,
		 strPtr("delegate"),strPtr(child.ID),map[string]any{"child_task_id":child.ID})
	})
	if errors.Is(err,ErrDelegationDepthLimit)||errors.Is(err,ErrDelegationAncestry){
		return s.blockRun(ctx,run,res,err.Error())
	}
	if err!=nil{
		// The transaction rolled back all state. Do not call failRun here:
		// a concurrent Worker may now own this incarnation, and failing it
		// would destroy the safe optimistic-concurrency boundary.
		return failedResult(res,err)
	}
	res.Status = "waiting_dependency"
	return res
}

func (s *Service) handleReplan(ctx context.Context, run Run, resp agentprotocol.Response, res TickResult) TickResult {
	if run.ReplanCount >= run.MaxReplans {
		return s.blockRun(ctx, run, res, ErrReplanLimit.Error())
	}
	var p replanProposal
	_ = json.Unmarshal(resp.Proposal, &p)
	cont, _ := json.Marshal(map[string]any{"replan": map[string]any{"reason": p.Reason, "instructions": p.Instructions}})
	if err := s.updateRun(ctx, run.ID, run.Revision, RunRunning, cont, nil, 0, 1, nil, nil, nil); err != nil {
		return failedResult(res, err)
	}
	_ = s.journal(ctx, run.ID, "replan", "succeeded", nil, nil, strPtr("replan"), nil, map[string]any{"reason": p.Reason})
	res.Status = "replanning"
	return res
}

func (s *Service) handleEscalate(ctx context.Context, run Run, resp agentprotocol.Response, res TickResult) TickResult {
	rp := defaultRoutePolicy()
	_ = json.Unmarshal(run.RoutePolicy, &rp)
	if !rp.RoutingEnabled {
		return s.blockRun(ctx, run, res, "automatic escalation is disabled by workspace routing policy")
	}
	if run.EscalationCount >= run.MaxEscalations {
		return s.blockRun(ctx, run, res, ErrEscalationLimit.Error())
	}
	var p escalateProposal
	_ = json.Unmarshal(resp.Proposal, &p)
	if run.LastCandidateID != nil && !contains(rp.ExcludedCandidateIDs, *run.LastCandidateID) {
		rp.ExcludedCandidateIDs = append(rp.ExcludedCandidateIDs, *run.LastCandidateID)
	}
	rb, _ := json.Marshal(rp)
	cont, _ := json.Marshal(map[string]any{"escalation": map[string]any{"reason": p.Reason, "excluded": rp.ExcludedCandidateIDs}})
	if err := s.updateRun(ctx, run.ID, run.Revision, RunRunning, cont, rb, 1, 0, nil, nil, nil); err != nil {
		return failedResult(res, err)
	}
	_ = s.journal(ctx, run.ID, "escalate", "succeeded", nil, nil, strPtr("escalate"), nil, map[string]any{"reason": p.Reason})
	res.Status = "escalating"
	return res
}

func (s *Service) handleHuman(ctx context.Context, run Run, t task.Task, resp agentprotocol.Response, res TickResult) TickResult {
	var p humanProposal
	_ = json.Unmarshal(resp.Proposal, &p)
	cont, _ := json.Marshal(map[string]any{"human_input_required": p.Question})
	if err := s.updateRun(ctx, run.ID, run.Revision, RunWaiting, cont, nil, 0, 0, nil, nil, nil); err != nil {
		return failedResult(res, err)
	}
	cur, _ := s.tasks.Get(ctx, t.ID)
	actor := WorkerPrincipal
	_, err := s.tasks.WaitApproval(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &actor, Reason: "human input required: " + boundedString(p.Question, 512)})
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	res.Status = "waiting_human"
	return res
}
func (s *Service) handleWait(ctx context.Context, run Run, t task.Task, resp agentprotocol.Response, res TickResult) TickResult {
	var p waitProposal
	_ = json.Unmarshal(resp.Proposal, &p)
	cont, _ := json.Marshal(map[string]any{"wait_reason": p.Reason})
	if err := s.updateRun(ctx, run.ID, run.Revision, RunWaiting, cont, nil, 0, 0, nil, nil, nil); err != nil {
		return failedResult(res, err)
	}
	cur, _ := s.tasks.Get(ctx, t.ID)
	actor := WorkerPrincipal
	_, err := s.tasks.Pause(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &actor, Reason: boundedString(p.Reason, 512)})
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	res.Status = "paused"
	return res
}
func (s *Service) handleFail(ctx context.Context, run Run, t task.Task, resp agentprotocol.Response, res TickResult) TickResult {
	var p failProposal
	_ = json.Unmarshal(resp.Proposal, &p)
	if strings.TrimSpace(p.Reason) == "" {
		p.Reason = "worker model proposed failure"
	}
	cur, _ := s.tasks.Get(ctx, t.ID)
	actor := WorkerPrincipal
	_, err := s.tasks.Fail(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &actor, Reason: boundedString(p.Reason, 1024)})
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	_ = s.finishRun(ctx, run.ID, RunFailed, p.Reason)
	res.Status = "failed"
	res.Error = p.Reason
	return res
}

func (s *Service) handleComplete(ctx context.Context, run Run, t task.Task, resp agentprotocol.Response, res TickResult) TickResult {
	var p completeProposal
	if err := json.Unmarshal(resp.Proposal, &p); err != nil {
		return s.failRun(ctx, run, res, fmt.Errorf("invalid complete proposal"))
	}
	if len(p.Result) == 0 {
		p.Result = json.RawMessage(`{}`)
	}
	if !json.Valid(p.Result) {
		return s.failRun(ctx, run, res, fmt.Errorf("complete result invalid JSON"))
	}
	required := completionVerification(t.Completion)
	worker := WorkerPrincipal
	verifier := VerifierPrincipal
	cur, err := s.tasks.Get(ctx, t.ID)
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	cur, err = s.tasks.RequestCompletion(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &worker, Reason: "agent proposed completion"})
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	cur, err = s.tasks.BeginVerification(ctx, task.TransitionCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, ActorPrincipalID: &verifier, Reason: "independent completion gate"})
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	if required != policy.VerificationV0 {
		criteria := completionAssuranceCriteria(t.Completion)
		evidence := p.Evidence
		if len(evidence) == 0 || !json.Valid(evidence) {
			evidence = json.RawMessage(`{}`)
		}
		spec, _ := json.Marshal(map[string]any{
			"assurance_version": 1,
			"source":            "agent_completion_proposal",
			"worker_run_id":     run.ID,
			"proposed_result":   json.RawMessage(p.Result),
			"criteria":          json.RawMessage(criteria),
			"evidence":          json.RawMessage(evidence),
		})
		v, err := s.verification.Create(ctx, verification.CreateCommand{WorkspaceID: run.WorkspaceID, TaskID: &cur.ID, SubjectRef: "agent_worker_run:" + run.ID, RequiredLevel: required, Spec: spec, ActorPrincipalID: &verifier})
		if err != nil {
			return s.failRun(ctx, run, res, err)
		}
		cont, _ := json.Marshal(map[string]any{"completion_proposal": json.RawMessage(resp.Proposal), "required_verification": required, "verification_id": v.ID})
		_ = s.updateRun(ctx, run.ID, run.Revision, RunBlocked, cont, nil, 0, 0, nil, nil, strPtr(ErrHigherVerification.Error()))
		_ = s.journal(ctx, run.ID, "verification", "queued", nil, nil, strPtr("complete"), strPtr(v.ID), map[string]any{"verification_id": v.ID, "required_level": required})
		res.Status = "blocked_verification"
		res.Error = fmt.Sprintf("completion requires %s; independent assurance queued", required)
		return res
	}
	spec, _ := json.Marshal(map[string]any{"source": "agent_completion_proposal", "worker_run_id": run.ID, "rule": "V0 is declaration only; no independent factual claim is inferred"})
	v, err := s.verification.Create(ctx, verification.CreateCommand{WorkspaceID: run.WorkspaceID, TaskID: &cur.ID, SubjectRef: "agent_worker_run:" + run.ID, RequiredLevel: policy.VerificationV0, Spec: spec, ActorPrincipalID: &verifier})
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	level := policy.VerificationV0
	vr, _ := json.Marshal(map[string]any{"worker_run_id": run.ID, "proposal": json.RawMessage(resp.Proposal), "verification_semantics": "model/result declaration only"})
	v, err = s.verification.Resolve(ctx, verification.ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: verification.StatusPass, AchievedLevel: &level, Result: vr, VerifiedBy: verifier, ActorPrincipalID: &verifier})
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	cpState, _ := json.Marshal(map[string]any{"worker_run_id": run.ID, "verification_id": v.ID, "result": json.RawMessage(p.Result)})
	cp, err := s.verification.CreateCheckpoint(ctx, verification.CheckpointCommand{WorkspaceID: run.WorkspaceID, TaskID: cur.ID, VerificationID: v.ID, State: cpState, ActorPrincipalID: &verifier})
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	cur, err = s.tasks.Get(ctx, cur.ID)
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	_, err = s.tasks.CompleteVerified(ctx, task.CompleteCommand{TaskID: cur.ID, ExpectedRevision: cur.Revision, CheckpointID: cp.ID, Result: p.Result, ActorPrincipalID: &verifier})
	if err != nil {
		return s.failRun(ctx, run, res, err)
	}
	_ = s.finishRun(ctx, run.ID, RunSucceeded, "")
	_ = s.journal(ctx, run.ID, "verification", "succeeded", nil, nil, strPtr("complete"), strPtr(cp.ID), map[string]any{"verification_id": v.ID, "checkpoint_id": cp.ID, "level": "V0"})
	res.Status = "complete"
	return res
}
