#!/usr/bin/env python3
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
def read(rel):
    p=ROOT/rel
    if not p.exists(): raise SystemExit(f"M31 Team Mode validation: FAIL: missing {rel}")
    return p.read_text(encoding='utf-8')
def req(rel,*needles):
    t=read(rel); missing=[n for n in needles if n not in t]
    if missing: raise SystemExit(f"M31 Team Mode validation: FAIL: {rel} missing {missing}")
    return t
req('migrations/0014_team_mode.sql','CREATE TABLE teams','CREATE TABLE team_members','CREATE TABLE team_sessions','CREATE TABLE task_execution_profiles','CREATE TABLE team_messages','CREATE TABLE team_plans','CREATE TABLE team_objections','CREATE TABLE team_decisions','CREATE TABLE team_turn_requests',"'direct','supervisor','team','council'")
req('internal/task/service.go','type admissionGuard interface','AllowReady','AllowStart','SetAdmissionGuard')
req('internal/team/service.go','func (s *Service) AcceptPlan','ErrOpenBlockingObjection','accepted_risk','func (s *Service) ReopenDeliberation','func (s *Service) RequestRound','func (s *Service) AllowReady','func (s *Service) AllowStart','memberActorAllowed')
req('internal/teamworker/service.go','system:team-worker','PermittedProposalTypes','ProposalReplan','RequireZeroIncrementalCost','BudgetAccountID','team_turn_requests','deliberation_only','no_side_effects')
req('internal/agentworker/service.go',"ep.execution_mode='team'")
req('internal/agentworker/execution.go','team-plan-accepted','AUTHORITATIVE_DATA',"p.status='accepted'")
req('internal/api/server.go','POST /v1/teams','POST /v1/tasks/{taskID}/team-session','POST /v1/team-sessions/{sessionID}/rounds','POST /v1/team-sessions/{sessionID}/plans/{planID}/accept','POST /v1/team-sessions/{sessionID}/reopen')
req('internal/bootstrap/bootstrap.go','team.NewService','SetAdmissionGuard','teamworker.New','TeamWorker')
req('cmd/harnessd/main.go','SetTeam(runtime.Team)','runtime.TeamWorker.Tick')
req('docs/team-mode.md','Team Mode','PLAN_ACCEPTED','accepted TeamPlan','reasoning-only')
print('M31 Team Mode validation: PASS')
