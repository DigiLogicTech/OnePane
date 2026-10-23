// Package teamgate fences accepted Team contributions without depending on
// Task/Worker services. The same check runs at admission and after model waits.
package teamgate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var ErrExecution = errors.New("accepted Team assignment or dependency authority unavailable")

type Contribution struct {
	Key            string          `json:"key"`
	MemberID       string          `json:"member_id"`
	TaskID         string          `json:"task_id"`
	CheckpointID   string          `json:"checkpoint_id"`
	VerificationID string          `json:"verification_id"`
	AchievedLevel  string          `json:"achieved_level"`
	State          json.RawMessage `json:"state"`
}

// Require returns only explicitly approved predecessor contributions. A V0
// checkpoint remains a declaration; callers must preserve its achieved level
// and treat contributed text as derived data, never as instructions/authority.
func Require(ctx context.Context, q Queryer, workspaceID, taskID string) ([]Contribution, error) {
	return require(ctx, q, workspaceID, taskID, false)
}

// RequireAdmission is for a transaction that independently validates the
// Task's Start/Resume transition. It may resume that exact paused Task; it
// never authorizes a child while its Team root remains paused.
func RequireAdmission(ctx context.Context, q Queryer, workspaceID, taskID string) ([]Contribution, error) {
	return require(ctx, q, workspaceID, taskID, true)
}

func require(ctx context.Context, q Queryer, workspaceID, taskID string, admission bool) ([]Contribution, error) {
	var completion string
	if err := q.QueryRowContext(ctx, `SELECT completion_json FROM tasks WHERE id=? AND workspace_id=?`, taskID, workspaceID).Scan(&completion); err != nil {
		return nil, err
	}
	var envelope map[string]json.RawMessage
	// Ordinary legacy completion criteria may be any valid JSON. Assignment
	// records and the sealed completion hash below remain authoritative.
	_ = json.Unmarshal([]byte(completion), &envelope)
	_, marked := envelope["team_assignment"]
	// Pre-assignment schemas cannot contain accepted assignment records.
	// A marked Task may never use that compatibility path.
	var installed int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='team_execution_assignments'`).Scan(&installed); err != nil {
		return nil, err
	}
	if installed == 0 {
		if marked {
			return nil, ErrExecution
		}
		return nil, nil
	}
	if paused, err := TaskPaused(ctx, q, workspaceID, taskID); err != nil {
		return nil, err
	} else if paused {
		return nil, ErrPaused
	}
	var session, root, plan, sessionState, planState, rootState, body string
	err := q.QueryRowContext(ctx, `SELECT s.id,s.task_id,p.id,s.status,p.status,t.state,p.plan_json
	 FROM task_execution_profiles ep JOIN team_sessions s ON s.id=ep.team_session_id
	 JOIN tasks t ON t.id=s.task_id AND t.workspace_id=s.workspace_id AND t.archived_at IS NULL
	 JOIN team_plans p ON p.id=s.accepted_plan_id AND p.session_id=s.id AND p.workspace_id=s.workspace_id
	 WHERE ep.task_id=? AND ep.workspace_id=? AND s.workspace_id=? AND ep.execution_mode='team'`, taskID, workspaceID, workspaceID).Scan(&session, &root, &plan, &sessionState, &planState, &rootState, &body)
	if errors.Is(err, sql.ErrNoRows) {
		if marked {
			return nil, ErrExecution
		}
		var associated int
		if e := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_execution_assignments WHERE task_id=? OR parent_task_id=?`, taskID, taskID).Scan(&associated); e != nil {
			return nil, e
		}
		if associated != 0 {
			return nil, ErrExecution
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if paused, err := Paused(ctx, q, session); err != nil {
		return nil, err
	} else if paused {
		return nil, ErrPaused
	}
	var manifest struct {
		Assignments []struct {
			Key       string   `json:"key"`
			MemberID  string   `json:"member_id"`
			Objective string   `json:"objective"`
			DependsOn []string `json:"depends_on"`
		} `json:"assignments"`
	}
	if json.Unmarshal([]byte(body), &manifest) != nil {
		if marked {
			return nil, ErrExecution
		}
		return nil, nil
	}
	if len(manifest.Assignments) == 0 {
		if marked {
			return nil, ErrExecution
		}
		return nil, nil
	}
	if len(manifest.Assignments) > 8 || planState != "accepted" || (sessionState != "plan_accepted" && sessionState != "executing") {
		return nil, ErrExecution
	}
	var ownState string
	if err = q.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id=? AND workspace_id=? AND archived_at IS NULL`, taskID, workspaceID).Scan(&ownState); err != nil {
		return nil, ErrExecution
	}
	switch ownState {
	case "created", "ready", "running", "waiting_dependency":
	default:
		if !admission {
			return nil, ErrExecution
		}
	}
	switch rootState {
	case "created", "ready", "running", "waiting_dependency":
	default:
		if !admission || root != taskID || (rootState != "paused" && rootState != "waiting_approval") {
			return nil, ErrExecution
		}
	}
	type assignment struct {
		key, member, task, dependencies, hash, profile, objective string
		revision                                                  int64
	}
	rows, err := q.QueryContext(ctx, `SELECT a.assignment_key,a.member_id,a.task_id,a.depends_on_json,a.completion_sha256,a.profile_id,a.profile_revision,child.objective
	 FROM team_execution_assignments a JOIN tasks child ON child.id=a.task_id
	 JOIN tasks parent ON parent.id=a.parent_task_id
	 WHERE a.session_id=? AND a.plan_id=? AND a.workspace_id=? AND a.parent_task_id=?
	 AND child.workspace_id=a.workspace_id AND child.parent_task_id=a.parent_task_id
	 AND child.project_id IS parent.project_id AND child.project_workspace_id IS parent.project_workspace_id
	 AND child.artifact_session_id IS parent.artifact_session_id AND child.archived_at IS NULL LIMIT 9`, session, plan, workspaceID, root)
	if err != nil {
		return nil, err
	}
	byKey := map[string]assignment{}
	byTask := map[string]assignment{}
	for rows.Next() {
		var a assignment
		if err = rows.Scan(&a.key, &a.member, &a.task, &a.dependencies, &a.hash, &a.profile, &a.revision, &a.objective); err != nil {
			break
		}
		byKey[a.key] = a
		byTask[a.task] = a
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
	if len(byKey) != len(manifest.Assignments) {
		return nil, ErrExecution
	}
	expected := map[string]assignment{}
	var priorJSON, rootHash string
	if err = q.QueryRowContext(ctx, `SELECT completion_sha256,prior_dependencies_json FROM team_assignment_roots WHERE session_id=? AND plan_id=? AND task_id=?`, session, plan, root).Scan(&rootHash, &priorJSON); err != nil {
		return nil, ErrExecution
	}
	var prior []string
	if json.Unmarshal([]byte(priorJSON), &prior) != nil || len(prior) > 32 {
		return nil, ErrExecution
	}
	for _, id := range prior {
		if id == "" {
			return nil, ErrExecution
		}
		expected[id] = assignment{task: id}
		var ready int
		if err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id=? AND workspace_id=? AND state='complete' AND archived_at IS NULL`, id, workspaceID).Scan(&ready); err != nil || ready != 1 {
			return nil, ErrExecution
		}
	}
	if taskID == root {
		sum := sha256.Sum256([]byte(completion))
		if hex.EncodeToString(sum[:]) != rootHash {
			return nil, ErrExecution
		}
	}
	for _, spec := range manifest.Assignments {
		a, ok := byKey[spec.Key]
		if !ok || a.member != spec.MemberID || a.objective != strings.TrimSpace(spec.Objective) {
			return nil, ErrExecution
		}
		var approved []string
		if json.Unmarshal([]byte(a.dependencies), &approved) != nil || len(approved) != len(spec.DependsOn) {
			return nil, ErrExecution
		}
		for i, key := range approved {
			if key != spec.DependsOn[i] {
				return nil, ErrExecution
			}
		}
		if taskID == root {
			expected[a.task] = a
		}
		if a.task == taskID {
			if !marked {
				return nil, ErrExecution
			}
			sum := sha256.Sum256([]byte(completion))
			if hex.EncodeToString(sum[:]) != a.hash {
				return nil, ErrExecution
			}
			if a.revision > 0 {
				var rev int64
				if err = q.QueryRowContext(ctx, `SELECT revision FROM agent_profiles WHERE id=? AND status='active' AND (workspace_id=? OR (id='agent.md' AND workspace_id IS NULL))`, a.profile, workspaceID).Scan(&rev); err != nil || rev != a.revision {
					return nil, ErrExecution
				}
			}
			for _, key := range approved {
				pred, ok := byKey[key]
				if !ok {
					return nil, ErrExecution
				}
				expected[pred.task] = pred
			}
		}
	}
	if taskID != root {
		if _, ok := byTask[taskID]; !ok {
			return nil, ErrExecution
		}
	}
	// Verify exact SQL dependency set, including kind/mode and predecessor
	// identity. Deleted, weakened or substituted edges fail closed.
	rows, err = q.QueryContext(ctx, `SELECT d.depends_on_task_id,d.dependency_type,d.dependency_mode FROM task_dependencies d JOIN tasks p ON p.id=d.depends_on_task_id AND p.workspace_id=? AND p.archived_at IS NULL WHERE d.task_id=? LIMIT 41`, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for rows.Next() {
		var id, kind, mode string
		if err = rows.Scan(&id, &kind, &mode); err != nil {
			break
		}
		if _, ok := expected[id]; !ok || seen[id] || kind != "hard" || mode != "all" {
			err = ErrExecution
			break
		}
		seen[id] = true
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr = rows.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(seen) != len(expected) {
		return nil, ErrExecution
	}
	out := make([]Contribution, 0, len(expected))
	for _, spec := range manifest.Assignments {
		a := byKey[spec.Key]
		if _, ok := expected[a.task]; !ok {
			continue
		}
		var v Contribution
		var state string
		err = q.QueryRowContext(ctx, `SELECT c.id,c.verification_id,v.achieved_level,c.state_json FROM tasks t
		 JOIN checkpoints c ON c.task_id=t.id AND c.workspace_id=t.workspace_id AND c.status='valid' AND c.invalidated_at IS NULL
		 JOIN verifications v ON v.id=c.verification_id AND v.workspace_id=t.workspace_id AND v.task_id=t.id AND v.status='pass'
		 WHERE t.id=? AND t.workspace_id=? AND t.state='complete' AND t.archived_at IS NULL
		 ORDER BY c.created_at DESC,c.id DESC LIMIT 1`, a.task, workspaceID).Scan(&v.CheckpointID, &v.VerificationID, &v.AchievedLevel, &state)
		if err != nil {
			return nil, ErrExecution
		}
		if len(state) > 16<<10 || !json.Valid([]byte(state)) {
			return nil, ErrExecution
		}
		v.Key = a.key
		v.MemberID = a.member
		v.TaskID = a.task
		v.State = json.RawMessage(state)
		out = append(out, v)
	}
	return out, nil
}
