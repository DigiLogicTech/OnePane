package team

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// Execution assignments are part of the exact plan a human accepts. They
// schedule ordinary governed Tasks; member names/profiles never grant tools.
type AssignmentSpec struct {
	Key                  string          `json:"key"`
	MemberID             string          `json:"member_id"`
	Objective            string          `json:"objective"`
	DependsOn            []string        `json:"depends_on,omitempty"`
	RequiredVerification string          `json:"required_verification,omitempty"`
	VerificationSpec     json.RawMessage `json:"verification_spec,omitempty"`
}

var assignmentKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,39}$`)

func assignmentSpecs(body json.RawMessage) ([]AssignmentSpec, error) {
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "{") {
		return nil, nil
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return nil, ErrInvalid
	}
	raw, exists := envelope["assignments"]
	if !exists {
		return nil, nil
	}
	var specs []AssignmentSpec
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(body) > 64<<10 || string(raw) == "null" || decoder.Decode(&specs) != nil || len(specs) < 1 || len(specs) > 8 {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	for _, spec := range specs {
		if !assignmentKey.MatchString(spec.Key) || seen[spec.Key] || spec.MemberID == "" || len(spec.MemberID) > 128 || strings.TrimSpace(spec.Objective) == "" || len(spec.Objective) > 4096 || len(spec.DependsOn) > 7 {
			return nil, ErrInvalid
		}
		seen[spec.Key] = true
		if _, err := assignmentCompletionCriteria(map[string]json.RawMessage{}, spec); err != nil {
			return nil, err
		}
	}
	// Stable topological traversal rejects cycles, unknown and repeated edges.
	ordered := make([]AssignmentSpec, 0, len(specs))
	done := map[string]bool{}
	for len(ordered) < len(specs) {
		progress := false
		for _, spec := range specs {
			if done[spec.Key] {
				continue
			}
			ready := true
			deps := map[string]bool{}
			for _, key := range spec.DependsOn {
				if !seen[key] || key == spec.Key || deps[key] {
					return nil, ErrInvalid
				}
				deps[key] = true
				if !done[key] {
					ready = false
				}
			}
			if ready {
				ordered = append(ordered, spec)
				done[spec.Key] = true
				progress = true
			}
		}
		if !progress {
			return nil, ErrInvalid
		}
	}
	return ordered, nil
}

func (s *Service) createAssignmentsTx(ctx context.Context, tx storage.Tx, ss Session, plan Plan, actor string, now int64) error {
	specs, err := assignmentSpecs(plan.Body)
	if err != nil || len(specs) == 0 {
		return err
	}
	var mode, snapshotRaw string
	if err = tx.QueryRowContext(ctx, `SELECT execution_mode,snapshot_json FROM team_session_manifests WHERE session_id=? AND workspace_id=?`, ss.ID, ss.WorkspaceID).Scan(&mode, &snapshotRaw); err != nil {
		return err
	}
	if mode != "team" {
		return ErrInvalid
	}
	var frozen SessionSnapshot
	if json.Unmarshal([]byte(snapshotRaw), &frozen) != nil || frozen.SessionID != ss.ID || frozen.TaskID != ss.TaskID || frozen.TeamID != ss.TeamID {
		return ErrInvalid
	}
	members := map[string]SnapshotMember{}
	for _, m := range frozen.Members {
		members[m.ID] = m
	}
	var parent task.Task
	var project, world, artifactSession *string
	var projectValue, worldValue, artifactValue interface{}
	var completion string
	// Read scope inside the same transaction as assignment and Task creation.
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state,project_id,project_workspace_id,artifact_session_id,completion_json FROM tasks WHERE id=? AND workspace_id=? AND archived_at IS NULL`, ss.TaskID, ss.WorkspaceID).Scan(&state, &projectValue, &worldValue, &artifactValue, &completion); err != nil {
		return err
	}
	if state != string(task.StateCreated) {
		return ErrSessionState
	}
	var attempts int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_attempts WHERE task_id=?`, ss.TaskID).Scan(&attempts); err != nil {
		return err
	}
	if attempts != 0 {
		return ErrSessionState
	}
	prior := []string{}
	rows, err := tx.QueryContext(ctx, `SELECT depends_on_task_id,dependency_type,dependency_mode FROM task_dependencies WHERE task_id=? ORDER BY depends_on_task_id LIMIT 33`, ss.TaskID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, kind, mode string
		if err = rows.Scan(&id, &kind, &mode); err != nil {
			break
		}
		if kind != "hard" || mode != "all" || len(prior) >= 32 {
			err = ErrInvalid
			break
		}
		prior = append(prior, id)
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	priorJSON, _ := json.Marshal(prior)
	parentSum := sha256.Sum256([]byte(completion))
	if _, err = tx.ExecContext(ctx, `INSERT INTO team_assignment_roots(session_id,plan_id,task_id,completion_sha256,prior_dependencies_json) VALUES(?,?,?,?,?)`, ss.ID, plan.ID, ss.TaskID, hex.EncodeToString(parentSum[:]), string(priorJSON)); err != nil {
		return err
	}
	if v, ok := projectValue.(string); ok {
		project = &v
	}
	if v, ok := worldValue.(string); ok {
		world = &v
	}
	if v, ok := artifactValue.(string); ok {
		artifactSession = &v
	}
	parent = task.Task{ID: ss.TaskID, WorkspaceID: ss.WorkspaceID, ProjectID: project, ProjectWorkspaceID: world, ArtifactSessionID: artifactSession}
	var base map[string]json.RawMessage
	if json.Unmarshal([]byte(completion), &base) != nil || base == nil {
		return ErrInvalid
	}
	ids := map[string]string{}
	for _, spec := range specs {
		member, ok := members[spec.MemberID]
		if !ok || member.Status != "active" || member.MemberKind != "agent" {
			return ErrInvalid
		}
		profileID := profileIDFromMemberConfig(member.Config)
		profileRevision := int64(0)
		if member.Profile != nil {
			profileID = member.Profile.ID
			profileRevision = member.Profile.Revision
		}
		if profileID != "agent.md" {
			var revision int64
			if err = tx.QueryRowContext(ctx, `SELECT revision FROM agent_profiles WHERE id=? AND workspace_id=? AND status='active'`, profileID, ss.WorkspaceID).Scan(&revision); err != nil {
				return ErrInvalid
			}
			if profileRevision < 1 || revision != profileRevision {
				return ErrInvalid
			}
		}
		childCompletion := map[string]json.RawMessage{}
		for k, v := range base {
			childCompletion[k] = v
		}
		for _, key := range []string{"graph_id", "node_key", "dependency_policy", "operator_approved", "standing_autonomy_policy"} {
			delete(childCompletion, key)
		}
		childCompletion["source"] = json.RawMessage(`"accepted-team-assignment"`)
		if childCompletion, err = assignmentCompletionCriteria(childCompletion, spec); err != nil {
			return err
		}
		routing := map[string]json.RawMessage{}
		if raw, exists := base["onepane_routing"]; exists && json.Unmarshal(raw, &routing) != nil {
			return ErrInvalid
		}
		if routing == nil {
			return ErrInvalid
		}
		// Profile identity is the reviewed roster selection; all parent access,
		// placement, candidate restrictions and assurance criteria are inherited.
		routing["agent_profile"], _ = json.Marshal(profileID)
		routing["fallback_agent_profiles"] = json.RawMessage(`[]`)
		childCompletion["onepane_routing"], _ = json.Marshal(routing)
		childCompletion["team_assignment"], _ = json.Marshal(map[string]string{"session_id": ss.ID, "plan_id": plan.ID, "key": spec.Key, "member_id": member.ID, "member_name": member.DisplayName, "role_name": member.RoleName})
		childJSON, _ := json.Marshal(childCompletion)
		child, err := s.tasks.CreateInTransaction(ctx, tx, task.CreateCommand{WorkspaceID: ss.WorkspaceID, ProjectID: parent.ProjectID, ProjectWorkspaceID: parent.ProjectWorkspaceID, ArtifactSessionID: parent.ArtifactSessionID, ParentTaskID: &ss.TaskID, Objective: spec.Objective, Completion: childJSON, ActorPrincipalID: &actor})
		if err != nil {
			return err
		}
		ids[spec.Key] = child.ID
		deps, _ := json.Marshal(append([]string{}, spec.DependsOn...))
		sum := sha256.Sum256(child.Completion)
		if _, err = tx.ExecContext(ctx, `INSERT INTO team_execution_assignments(session_id,plan_id,assignment_key,workspace_id,member_id,task_id,parent_task_id,depends_on_json,completion_sha256,profile_id,profile_revision,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, ss.ID, plan.ID, spec.Key, ss.WorkspaceID, member.ID, child.ID, ss.TaskID, string(deps), hex.EncodeToString(sum[:]), profileID, profileRevision, now); err != nil {
			return err
		}
		for _, key := range spec.DependsOn {
			if _, err = tx.ExecContext(ctx, `INSERT INTO task_dependencies(task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json) VALUES(?,?,'hard','all','{"source":"accepted_team_assignment"}')`, child.ID, ids[key]); err != nil {
				return err
			}
		}
		for _, priorID := range prior {
			if _, err = tx.ExecContext(ctx, `INSERT INTO task_dependencies(task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json) VALUES(?,?,'hard','all','{"source":"inherited_team_prerequisite"}')`, child.ID, priorID); err != nil {
				return err
			}
		}
		// The original Task remains the consolidator and cannot start before
		// every contribution has reached the ordinary verified completion gate.
		if _, err = tx.ExecContext(ctx, `INSERT INTO task_dependencies(task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json) VALUES(?,?,'hard','all','{"source":"accepted_team_assignment"}')`, ss.TaskID, child.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO task_execution_profiles(task_id,workspace_id,execution_mode,team_id,team_session_id,config_json,created_by,created_at,updated_at) VALUES(?,?,'team',?,?,'{}',?,?,?)`, child.ID, ss.WorkspaceID, ss.TeamID, ss.ID, actor, now, now); err != nil {
			return err
		}
		if err = s.emit(ctx, tx, ss.WorkspaceID, "team.assignment_created", "team_session", ss.ID, &actor, map[string]string{"plan_id": plan.ID, "assignment_key": spec.Key, "member_id": member.ID, "task_id": child.ID}); err != nil {
			return err
		}
	}
	return nil
}
