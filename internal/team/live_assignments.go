package team

import (
	"context"
	"database/sql"
	"encoding/json"
)

type LiveAssignment struct {
	Key            string   `json:"key"`
	MemberID       string   `json:"member_id"`
	TaskID         string   `json:"task_id"`
	Objective      string   `json:"objective"`
	State          string   `json:"state"`
	DependsOn      []string `json:"depends_on"`
	WorkerStatus   string   `json:"worker_status,omitempty"`
	LastStepKind   string   `json:"last_step_kind,omitempty"`
	LastStepStatus string   `json:"last_step_status,omitempty"`
	ModelID        string   `json:"model_id,omitempty"`
	ComputeState   string   `json:"compute_state,omitempty"`
}

func (s *Service) liveAssignments(ctx context.Context, ss Session) ([]LiveAssignment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.assignment_key,a.member_id,a.task_id,t.objective,t.state,a.depends_on_json
	 FROM team_execution_assignments a JOIN tasks t ON t.id=a.task_id AND t.workspace_id=a.workspace_id
	 JOIN tasks parent ON parent.id=a.parent_task_id AND parent.workspace_id=a.workspace_id
	 WHERE a.session_id=? AND a.workspace_id=? AND a.parent_task_id=?
	 AND t.parent_task_id=parent.id AND t.project_id IS parent.project_id
	 AND t.project_workspace_id IS parent.project_workspace_id
	 ORDER BY a.created_at,a.assignment_key LIMIT 9`, ss.ID, ss.WorkspaceID, ss.TaskID)
	if err != nil {
		return nil, err
	}
	out := []LiveAssignment{}
	for rows.Next() {
		var a LiveAssignment
		var deps string
		if err = rows.Scan(&a.Key, &a.MemberID, &a.TaskID, &a.Objective, &a.State, &deps); err != nil {
			break
		}
		if json.Unmarshal([]byte(deps), &a.DependsOn) != nil || len(a.DependsOn) > 7 {
			err = ErrInvalid
			break
		}
		a.Objective = roomText(a.Objective)
		out = append(out, a)
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(out) > 8 {
		return nil, ErrInvalid
	}
	for i := range out {
		a := &out[i]
		var run string
		var candidate sql.NullString
		err = s.db.QueryRowContext(ctx, `SELECT id,status,last_candidate_id FROM agent_worker_runs WHERE task_id=? AND workspace_id=? ORDER BY started_at DESC,id DESC LIMIT 1`, a.TaskID, ss.WorkspaceID).Scan(&run, &a.WorkerStatus, &candidate)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if candidate.Valid {
			a.ModelID = roomText(candidate.String)
		}
		if run != "" {
			err = s.db.QueryRowContext(ctx, `SELECT step_kind,status FROM agent_worker_steps WHERE run_id=? ORDER BY step_number DESC LIMIT 1`, run).Scan(&a.LastStepKind, &a.LastStepStatus)
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
		}
		err = s.db.QueryRowContext(ctx, `SELECT q.state FROM inference_requests i JOIN runtime_execution_queue q ON q.inference_request_id=i.id AND q.task_id=i.task_id AND q.workspace_id=i.workspace_id WHERE i.workspace_id=? AND i.task_id=? AND i.principal_id='system:agent-worker' ORDER BY i.created_at DESC,i.id DESC LIMIT 1`, ss.WorkspaceID, a.TaskID).Scan(&a.ComputeState)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
	}
	return out, nil
}
