#!/usr/bin/env python3
from pathlib import Path
import sqlite3, sys

ROOT = Path(__file__).resolve().parents[1]

def fail(msg):
    print('M26 agent worker validation failed:', msg, file=sys.stderr)
    raise SystemExit(1)

def main():
    db = sqlite3.connect(':memory:')
    db.execute('PRAGMA foreign_keys=ON')
    for m in sorted((ROOT/'migrations').glob('*.sql')):
        db.executescript(m.read_text())

    tables = {r[0] for r in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    for name in ['agent_worker_runs','agent_worker_steps']:
        if name not in tables:
            fail('missing '+name)

    run_cols = {r[1] for r in db.execute("PRAGMA table_info(agent_worker_runs)")}
    for c in ['attempt_id','status','max_steps','step_count','max_replans','replan_count','max_escalations','escalation_count','route_policy_json','continuation_json','revision']:
        if c not in run_cols:
            fail('missing agent_worker_runs.'+c)
    step_cols = {r[1] for r in db.execute("PRAGMA table_info(agent_worker_steps)")}
    for c in ['run_id','step_number','step_kind','status','candidate_kind','proposal_type','result_ref','detail_json']:
        if c not in step_cols:
            fail('missing agent_worker_steps.'+c)

    indexes = {r[1] for r in db.execute("PRAGMA index_list('agent_worker_runs')")}
    # SQLite auto-indexes the UNIQUE attempt_id; explicit status/task indexes are contractual.
    for idx in ['idx_agent_worker_runs_status','idx_agent_worker_runs_task']:
        if idx not in indexes:
            fail('missing '+idx)

    scheduler = (ROOT/'internal/scheduler/types.go').read_text()
    for token in ['ExcludeCandidateIDs', 'excluded_by_request', 'AllowSubscriptionUsage', 'AllowPotentialMonetarySpend']:
        if token not in scheduler:
            fail('scheduler safety contract missing '+token)

    service = (ROOT/'internal/agentworker/service.go').read_text()
    for token in ['RecoverLostRuns', 'ensureWorkspaceAccess', 'workspace.system_membership_added', 'syncResumedRuns', 'agent_worker_restart_unknown_step_outcome']:
        if token not in service:
            fail('worker lifecycle contract missing '+token)

    execution = (ROOT/'internal/agentworker/execution.go').read_text()
    for token in ['contextcompiler.Compile', 'ExcludeCandidateIDs', 'CandidateAgentRuntime', 'inference.Execute', 'handleTool', 'handleDelegate', 'handleComplete']:
        if token not in execution:
            fail('worker execution contract missing '+token)

    helpers = (ROOT/'internal/agentworker/helpers.go').read_text()
    if 'findLease' not in helpers or 'authority.Issue' in helpers:
        fail('worker must consume pre-existing authority and never issue itself a lease')

    main_go = (ROOT/'cmd/harnessd/main.go').read_text()
    if 'AgentWorker.Tick' not in main_go:
        fail('daemon does not run general agent worker')

    print('M26 autonomous agent worker: PASS')

if __name__ == '__main__':
    main()
