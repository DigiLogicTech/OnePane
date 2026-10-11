package agentworker

import (
	"context"
	"fmt"
)

// activeWorkerExecutionBindingPredicate is deliberately reused both by
// step's early preflight and by the final atomic dispatch claim. A RUNNING
// Worker row alone is not authorization to issue model requests: its exact
// Task, original active Attempt, system principal and active canonical
// tenancy/Project/Workspace must still agree.
//
// This is not a lease on any external side effect. Other tool authority
// checks and the recovery coordinator remain independently mandatory.
const activeWorkerExecutionBindingPredicate = `
	r.worker_principal_id='system:agent-worker'
	AND EXISTS (
		SELECT 1 FROM task_attempts a
		WHERE a.id=r.attempt_id AND a.task_id=r.task_id
			AND a.worker_principal_id=r.worker_principal_id
			AND a.status='running'
			AND json_valid(a.metadata_json)=1
			AND json_extract(a.metadata_json,'$.agent_worker')='v1'
	)
	AND (
		SELECT COUNT(*) FROM task_attempts active
		WHERE active.task_id=r.task_id
			AND active.status IN ('created','queued','running','waiting')
	)=1
	AND EXISTS (
		SELECT 1 FROM tasks t
		JOIN workspaces tenant ON tenant.id=t.workspace_id
			AND tenant.status='active'
		LEFT JOIN projects p ON p.id=t.project_id
		LEFT JOIN project_workspaces pw ON pw.id=t.project_workspace_id
		WHERE t.id=r.task_id AND t.workspace_id=r.workspace_id
			AND t.state='running' AND t.archived_at IS NULL
			AND (
				t.project_id IS NULL
				OR (p.id=t.project_id AND p.workspace_id=t.workspace_id
					AND p.status='active')
			)
			AND (
				t.project_workspace_id IS NULL
				OR (t.project_id IS NOT NULL AND pw.id=t.project_workspace_id
					AND pw.project_id=t.project_id AND pw.status='active')
			)
	)`

// requireActiveWorkerExecutionBinding refuses to plan inference/tool work
// if the previous Task/Attempt/Worker ownership contract has drifted. The
// final dispatch CAS repeats this SQL predicate under the UPDATE statement.
func (s *Service) requireActiveWorkerExecutionBinding(
	ctx context.Context, r Run,
) error {
	if s==nil || s.db==nil || r.ID=="" || r.TaskID=="" || r.AttemptID=="" {
		return ErrInvalidWorkerState
	}
	var valid int
	err:=s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM agent_worker_runs r
			WHERE r.id=? AND r.task_id=? AND r.attempt_id=?
				AND r.status='running' AND `+
			activeWorkerExecutionBindingPredicate+`)`,
		r.ID,r.TaskID,r.AttemptID,
	).Scan(&valid)
	if err!=nil{return fmt.Errorf("read active Worker execution binding: %w",err)}
	if valid!=1{return fmt.Errorf("%w: active Task/Attempt/Workspace owner mismatch; operator recovery review required",ErrInvalidWorkerState)}
	return nil
}
