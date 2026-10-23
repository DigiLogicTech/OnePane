package agentworker

import "context"

// A cycle owns every selected Run until all dispatches and proposal commits
// finish. Only inference overlaps: preparation and all proposal/tool effects
// stay serialized under workerCycleMu, including recovery-halt decisions.
type preparedWorkerStep struct {
	result   TickResult
	dispatch func() func() TickResult
	abort    func() TickResult
}

func immediateWorkerStep(result TickResult) preparedWorkerStep {
	return preparedWorkerStep{result: result}
}

func (s *Service) step(ctx context.Context, run Run) TickResult {
	p := s.prepareStep(ctx, run)
	if p.dispatch == nil {
		return p.result
	}
	return p.dispatch()()
}

// Parallelism is restricted to two distinct members in the same accepted
// assignment graph. Shared inference admission decides actual capacity;
// small machines still queue through the ordinary coordinator/Colibri path.
func (s *Service) parallelTeamPair(ctx context.Context, first, second string) bool {
	var valid int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
	 SELECT 1 FROM team_execution_assignments a
	 JOIN team_execution_assignments b ON b.session_id=a.session_id
	 AND b.plan_id=a.plan_id AND b.parent_task_id=a.parent_task_id
	 AND b.workspace_id=a.workspace_id AND b.member_id<>a.member_id
	 JOIN team_sessions ss ON ss.id=a.session_id AND ss.accepted_plan_id=a.plan_id
	 WHERE a.task_id=? AND b.task_id=? AND a.task_id<>b.task_id
	 AND ss.status IN ('plan_accepted','executing')
	 AND NOT EXISTS(SELECT 1 FROM task_dependencies d WHERE
	 (d.task_id=a.task_id AND d.depends_on_task_id=b.task_id) OR
	 (d.task_id=b.task_id AND d.depends_on_task_id=a.task_id)))`, first, second).Scan(&valid)
	return err == nil && valid == 1
}

func (s *Service) stepTeamPair(ctx context.Context, first, second Run) []TickResult {
	prepared := []preparedWorkerStep{s.prepareStep(ctx, first)}
	if s.dispatchReconciliationFailed {
		return []TickResult{prepared[0].result}
	}
	prepared = append(prepared, s.prepareStep(ctx, second))
	if s.dispatchReconciliationFailed {
		out := make([]TickResult, len(prepared))
		for i, p := range prepared {
			out[i] = p.result
			if p.abort != nil {
				out[i] = p.abort()
			}
		}
		return out
	}

	type completed struct {
		index  int
		finish func() TickResult
	}
	ch := make(chan completed, 2)
	pending := 0
	out := make([]TickResult, len(prepared))
	for i, p := range prepared {
		out[i] = p.result
		if p.dispatch != nil {
			pending++
			go func(i int, p preparedWorkerStep) { ch <- completed{i, p.dispatch()} }(i, p)
		}
	}
	// Join every dispatch before releasing the cycle. Proposal effects remain
	// serialized, and a failed recovery commit stops applying later proposals.
	for ; pending > 0; pending-- {
		done := <-ch
		if s.dispatchReconciliationFailed {
			out[done.index] = failedResult(out[done.index], ErrDispatchRequiresReconciliation)
			continue
		}
		out[done.index] = done.finish()
	}
	return out
}
