package team

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
)

type LiveAssignment struct {
	Key               string   `json:"key"`
	MemberID          string   `json:"member_id"`
	TaskID            string   `json:"task_id"`
	Objective         string   `json:"objective"`
	State             string   `json:"state"`
	DependsOn         []string `json:"depends_on"`
	WorkerStatus      string   `json:"worker_status,omitempty"`
	LastStepKind      string   `json:"last_step_kind,omitempty"`
	LastStepStatus    string   `json:"last_step_status,omitempty"`
	ModelID           string   `json:"model_id,omitempty"`
	ComputeState      string   `json:"compute_state,omitempty"`
	CheckpointID      string   `json:"checkpoint_id,omitempty"`
	VerificationID    string   `json:"verification_id,omitempty"`
	VerificationLevel string   `json:"verification_level,omitempty"`
}

func (s *Service) liveAssignments(ctx context.Context, ss Session) ([]LiveAssignment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.assignment_key,a.member_id,a.task_id,t.objective,t.state,a.depends_on_json
	 FROM team_execution_assignments a JOIN tasks t ON t.id=a.task_id AND t.workspace_id=a.workspace_id
	 JOIN tasks parent ON parent.id=a.parent_task_id AND parent.workspace_id=a.workspace_id
	 WHERE a.session_id=? AND a.workspace_id=? AND a.parent_task_id=?
	 AND t.parent_task_id=parent.id AND t.project_id IS parent.project_id
	 AND t.project_workspace_id IS parent.project_workspace_id
	 AND t.artifact_session_id IS parent.artifact_session_id
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
		if err = s.liveAssignmentCompletion(ctx, ss, a); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Completion navigation is a current, ledger-bound observation. Never interpret
// arbitrary Task results, checkpoint state or model-supplied IDs as links.
func (s *Service) liveAssignmentCompletion(ctx context.Context, ss Session, a *LiveAssignment) error {
	if a.State != "complete" || ss.AcceptedPlanID == nil {
		return nil
	}
	var checkpoint, verification, level, completion, sealed string
	err := s.db.QueryRowContext(ctx, liveAssignmentCompletionQuery, *ss.AcceptedPlanID, ss.ID, ss.WorkspaceID, ss.TaskID, a.TaskID, a.Key, a.MemberID).Scan(&checkpoint, &verification, &level, &completion, &sealed)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(completion))
	if hex.EncodeToString(sum[:]) != sealed {
		return nil
	}
	a.CheckpointID, a.VerificationID, a.VerificationLevel = checkpoint, verification, level
	return nil
}

const liveAssignmentCompletionQuery = `SELECT c.id,v.id,v.achieved_level,t.completion_json,a.completion_sha256
 FROM team_execution_assignments a
 JOIN team_sessions s ON s.id=a.session_id AND s.workspace_id=a.workspace_id AND s.task_id=a.parent_task_id AND s.accepted_plan_id=a.plan_id AND s.accepted_plan_id=?
 JOIN team_assignment_roots root ON root.session_id=s.id AND root.plan_id=a.plan_id AND root.task_id=s.task_id
 JOIN team_plans p ON p.id=a.plan_id AND p.session_id=s.id AND p.workspace_id=s.workspace_id AND p.status='accepted'
 JOIN tasks parent ON parent.id=s.task_id AND parent.workspace_id=s.workspace_id AND parent.archived_at IS NULL
 JOIN tasks t ON t.id=a.task_id AND t.workspace_id=a.workspace_id AND t.parent_task_id=parent.id
  AND t.project_id IS parent.project_id AND t.project_workspace_id IS parent.project_workspace_id
  AND t.artifact_session_id IS parent.artifact_session_id AND t.archived_at IS NULL AND t.state='complete'
 JOIN task_attempts attempt ON attempt.id=(SELECT id FROM task_attempts WHERE task_id=t.id ORDER BY attempt_number DESC LIMIT 1)
  AND attempt.status='succeeded' AND attempt.ended_at IS NOT NULL AND attempt.started_at IS NOT NULL
 JOIN events started ON started.sequence=(SELECT sequence FROM events WHERE aggregate_type='task' AND aggregate_id=t.id AND event_type='task.started' ORDER BY sequence DESC LIMIT 1)
  AND started.workspace_id=t.workspace_id AND started.occurred_at=attempt.started_at
 JOIN events e ON e.sequence=(SELECT sequence FROM events WHERE aggregate_type='task' AND aggregate_id=t.id AND event_type='task.completed' ORDER BY sequence DESC LIMIT 1)
  AND e.workspace_id=t.workspace_id AND e.occurred_at>=attempt.ended_at AND e.sequence>started.sequence
 JOIN checkpoints c ON c.id=json_extract(CASE WHEN length(CAST(e.payload_json AS BLOB))<=16384 AND json_valid(e.payload_json) THEN e.payload_json ELSE '{}' END,'$.checkpoint_id')
  AND c.task_id=t.id AND c.workspace_id=t.workspace_id AND c.status='valid' AND c.invalidated_at IS NULL AND c.created_at<=e.occurred_at
 JOIN verifications v ON v.id=c.verification_id AND v.task_id=t.id AND v.workspace_id=t.workspace_id
  AND v.status='pass' AND v.completed_at IS NOT NULL AND c.created_at>=v.completed_at AND v.started_at>=attempt.started_at
  AND v.achieved_level IN ('V0','V1','V2','V3','V4','V5')
 WHERE a.session_id=? AND a.workspace_id=? AND a.parent_task_id=? AND a.task_id=? AND a.assignment_key=? AND a.member_id=?
 AND length(CAST(t.completion_json AS BLOB))<=65536
 AND v.required_level=json_extract(CASE WHEN length(CAST(t.completion_json AS BLOB))<=65536 AND json_valid(t.completion_json) THEN t.completion_json ELSE '{}' END,'$.required_verification') AND v.achieved_level>=v.required_level
 AND json_extract(CASE WHEN length(CAST(e.payload_json AS BLOB))<=16384 AND json_valid(e.payload_json) THEN e.payload_json ELSE '{}' END,'$.task_id')=t.id
 AND json_extract(CASE WHEN length(CAST(e.payload_json AS BLOB))<=16384 AND json_valid(e.payload_json) THEN e.payload_json ELSE '{}' END,'$.to')='complete'
 AND json_extract(CASE WHEN length(CAST(e.payload_json AS BLOB))<=16384 AND json_valid(e.payload_json) THEN e.payload_json ELSE '{}' END,'$.revision')=t.revision
 AND json_extract(CASE WHEN length(CAST(started.payload_json AS BLOB))<=16384 AND json_valid(started.payload_json) THEN started.payload_json ELSE '{}' END,'$.task_id')=t.id
 AND json_extract(CASE WHEN length(CAST(started.payload_json AS BLOB))<=16384 AND json_valid(started.payload_json) THEN started.payload_json ELSE '{}' END,'$.attempt_id')=attempt.id
 AND json_extract(CASE WHEN length(CAST(started.payload_json AS BLOB))<=16384 AND json_valid(started.payload_json) THEN started.payload_json ELSE '{}' END,'$.attempt_number')=attempt.attempt_number`
