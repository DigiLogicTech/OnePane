package agentworker

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"
)

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
	err := s.db.QueryRowContext(ctx, `SELECT `+parallelTeamPairPredicate+`
 FROM tasks first_task JOIN tasks second_task ON second_task.id=? WHERE first_task.id=?`, second, first).Scan(&valid)
	return err == nil && valid == 1
}

// A match is only a batching hint. prepareStep and every transport/proposal
// boundary still check current accepted ownership and ordinary admission.
const parallelTeamPairPredicate = `EXISTS(
	 SELECT 1 FROM team_execution_assignments a
	 JOIN team_execution_assignments b ON b.session_id=a.session_id
	 AND b.plan_id=a.plan_id AND b.parent_task_id=a.parent_task_id
	 AND b.workspace_id=a.workspace_id AND b.member_id<>a.member_id
	 JOIN team_sessions ss ON ss.id=a.session_id AND ss.accepted_plan_id=a.plan_id
	 WHERE a.task_id=first_task.id AND b.task_id=second_task.id AND a.task_id<>b.task_id
	 AND ss.status IN ('plan_accepted','executing')
	 AND NOT EXISTS(SELECT 1 FROM task_dependencies d WHERE
	 (d.task_id=a.task_id AND d.depends_on_task_id=b.task_id) OR
	 (d.task_id=b.task_id AND d.depends_on_task_id=a.task_id)))`

// parallelTeamPartner looks only within the already selected remaining batch.
// A single bounded query supplies ordered candidate hints and current scheduling
// tiers. Never leap over a different priority/class (or missing Task) to pair
// later work; every selected ordinary Task retains its opportunity this tick.
func (s *Service) parallelTeamPartner(ctx context.Context, first string, remaining []string) (int, error) {
	if len(remaining) == 0 {
		return -1, nil
	}
	if len(remaining) > 100 || len(first) > 256 || strings.TrimSpace(first) == "" || !utf8.ValidString(first) {
		return -1, ErrInvalidWorkerState
	}
	values := make([]string, len(remaining))
	args := make([]any, 0, len(remaining)+1)
	seen := map[string]bool{first: true}
	for i, id := range remaining {
		if len(id) > 256 || strings.TrimSpace(id) == "" || !utf8.ValidString(id) || seen[id] {
			return -1, ErrInvalidWorkerState
		}
		seen[id] = true
		values[i] = fmt.Sprintf("(?,%d)", i)
		args = append(args, id)
	}
	args = append(args, first)
	query := `WITH selected(task_id,position) AS (VALUES ` + strings.Join(values, ",") + `)
 SELECT selected.position,first_task.priority,first_task.scheduling_class,
 second_task.priority,second_task.scheduling_class,` + parallelTeamPairPredicate + `
 FROM selected JOIN tasks first_task ON first_task.id=?
 LEFT JOIN tasks second_task ON second_task.id=selected.task_id
 ORDER BY selected.position LIMIT 100`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return -1, err
	}
	defer rows.Close()
	for rows.Next() {
		var position, priority int
		var class string
		var otherPriority sql.NullInt64
		var otherClass sql.NullString
		var valid bool
		if err = rows.Scan(&position, &priority, &class, &otherPriority, &otherClass, &valid); err != nil {
			return -1, err
		}
		if !otherPriority.Valid || !otherClass.Valid || otherPriority.Int64 != int64(priority) || otherClass.String != class {
			return -1, nil
		}
		if valid {
			return position, nil
		}
	}
	return -1, rows.Err()
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
