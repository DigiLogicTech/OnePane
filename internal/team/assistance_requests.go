package team

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// AssistanceRequest is an observation of an original waiting contribution.
// Reading it grants no execution, reply, capability or continuation authority.
type AssistanceRequest struct {
	RequestID          string `json:"request_id"`
	WorkspaceID        string `json:"workspace_id"`
	SessionID          string `json:"session_id"`
	PlanID             string `json:"plan_id"`
	AssignmentKey      string `json:"assignment_key"`
	MemberID           string `json:"member_id"`
	MemberName         string `json:"member_name"`
	ParentTaskID       string `json:"parent_task_id"`
	TaskID             string `json:"task_id"`
	AttemptID          string `json:"attempt_id"`
	RunID              string `json:"run_id"`
	WaitStepID         string `json:"wait_step_id"`
	NoticeMessageID    string `json:"notice_message_id"`
	AuthorPrincipalID  string `json:"author_principal_id"`
	Question           string `json:"question"`
	ProjectID          string `json:"project_id,omitempty"`
	ProjectWorkspaceID string `json:"project_workspace_id,omitempty"`
	ArtifactSessionID  string `json:"artifact_session_id,omitempty"`
	TaskState          string `json:"task_state"`
	TaskRevision       int64  `json:"task_revision"`
	WorkerRevision     int64  `json:"worker_revision"`
	CreatedAt          int64  `json:"created_at"`
}

var assistanceRequestReadSQL = assistanceBoundedReadSQL()

func assistanceBoundedReadSQL() string {
	query := `SELECT h.id,h.workspace_id,h.session_id,h.plan_id,h.assignment_key,h.member_id,m.display_name,
 s.task_id,h.task_id,h.attempt_id,h.run_id,h.wait_step_id,h.notice_message_id,h.author_principal_id,h.question,
 COALESCE(t.project_id,''),COALESCE(t.project_workspace_id,''),COALESCE(t.artifact_session_id,''),t.state,t.revision,r.revision,h.created_at,
 t.completion_json,a.completion_sha256,parent.completion_json,root.completion_sha256,
 p.plan_json,manifest.snapshot_json,manifest.snapshot_sha256,root.prior_dependencies_json,m.config_json,notice.content_json,s.team_id,
 m.role_name,m.capability_id,m.protocol_level,COALESCE(m.principal_id,''),m.route_policy_json
 FROM team_assistance_requests h
 JOIN team_assistance_request_scopes pinned ON pinned.request_id=h.id
 JOIN team_execution_assignments a ON a.task_id=h.task_id AND a.workspace_id=h.workspace_id AND a.session_id=h.session_id AND a.plan_id=h.plan_id AND a.assignment_key=h.assignment_key AND a.member_id=h.member_id
 JOIN team_sessions s ON s.id=h.session_id AND s.workspace_id=h.workspace_id AND s.accepted_plan_id=h.plan_id AND s.task_id=a.parent_task_id AND s.status IN ('plan_accepted','executing','paused_for_deliberation')
 JOIN teams group_team ON group_team.id=s.team_id AND group_team.workspace_id=s.workspace_id AND group_team.status='active'
 JOIN team_plans p ON p.id=h.plan_id AND p.session_id=s.id AND p.workspace_id=s.workspace_id AND p.status='accepted'
 JOIN team_assignment_roots root ON root.session_id=s.id AND root.plan_id=p.id AND root.task_id=s.task_id
 JOIN team_session_manifests manifest ON manifest.session_id=s.id AND manifest.workspace_id=s.workspace_id AND manifest.team_id=s.team_id AND manifest.execution_mode='team' AND manifest.research_mode=0
 JOIN tasks parent ON parent.id=s.task_id AND parent.workspace_id=s.workspace_id AND parent.archived_at IS NULL AND parent.state IN ('created','ready','running','waiting_dependency','waiting_approval','paused')
 JOIN tasks t ON t.id=h.task_id AND t.workspace_id=h.workspace_id AND t.parent_task_id=parent.id AND t.archived_at IS NULL AND t.state IN ('waiting_approval','paused') AND t.revision>0
 AND t.project_id IS parent.project_id AND t.project_workspace_id IS parent.project_workspace_id AND t.artifact_session_id IS parent.artifact_session_id
 AND t.project_id IS pinned.project_id AND t.project_workspace_id IS pinned.project_workspace_id AND t.artifact_session_id IS pinned.artifact_session_id
 JOIN task_execution_profiles ep ON ep.task_id=t.id AND ep.workspace_id=h.workspace_id AND ep.team_id=s.team_id AND ep.team_session_id=s.id AND ep.execution_mode='team'
 JOIN task_execution_profiles parent_ep ON parent_ep.task_id=parent.id AND parent_ep.workspace_id=h.workspace_id AND parent_ep.team_id=s.team_id AND parent_ep.team_session_id=s.id AND parent_ep.execution_mode='team'
 JOIN task_attempts attempt ON attempt.id=h.attempt_id AND attempt.task_id=h.task_id AND attempt.worker_principal_id=h.author_principal_id AND attempt.status='waiting' AND attempt.ended_at IS NULL
 AND attempt.id=(SELECT id FROM task_attempts WHERE task_id=t.id ORDER BY attempt_number DESC LIMIT 1)
 AND (SELECT COUNT(*) FROM task_attempts active WHERE active.task_id=t.id AND active.status IN ('created','queued','running','waiting'))=1
 AND json_extract(CASE WHEN length(CAST(attempt.metadata_json AS BLOB))<=16384 AND json_valid(attempt.metadata_json) THEN attempt.metadata_json ELSE '{}' END,'$.agent_worker')='v1'
 JOIN agent_worker_runs r ON r.id=h.run_id AND r.workspace_id=h.workspace_id AND r.task_id=h.task_id AND r.attempt_id=h.attempt_id AND r.worker_principal_id=h.author_principal_id AND r.status='waiting' AND r.revision>0
 AND json_extract(CASE WHEN length(CAST(r.continuation_json AS BLOB))<=4096 AND json_valid(r.continuation_json) THEN r.continuation_json ELSE '{}' END,'$.human_input_required')=h.question
 JOIN agent_worker_steps step ON step.id=h.wait_step_id AND step.run_id=r.id AND step.step_kind='human' AND step.proposal_type='human' AND step.status='waiting' AND step.completed_at IS NOT NULL
 AND step.step_number=(SELECT MAX(step_number) FROM agent_worker_steps WHERE run_id=r.id)
 JOIN team_members m ON m.id=h.member_id AND m.team_id=s.team_id AND m.workspace_id=h.workspace_id AND m.member_kind='agent' AND m.status='active'
 JOIN team_messages notice ON notice.id=h.notice_message_id AND notice.workspace_id=h.workspace_id AND notice.session_id=h.session_id AND notice.author_member_id=h.member_id AND notice.author_principal_id=h.author_principal_id AND notice.message_kind='system'
 JOIN principals author ON author.id=h.author_principal_id AND author.id='system:agent-worker' AND author.principal_type='system' AND author.status='active'
 JOIN workspace_memberships author_member ON author_member.principal_id=author.id AND author_member.workspace_id=h.workspace_id AND author_member.status='active'
 JOIN workspaces tenant ON tenant.id=h.workspace_id AND tenant.status='active'
 JOIN principals reader ON reader.id=? AND reader.status='active'
 JOIN workspace_memberships reader_member ON reader_member.principal_id=reader.id AND reader_member.workspace_id=h.workspace_id AND reader_member.status='active'
 LEFT JOIN projects project ON project.id=t.project_id
 LEFT JOIN project_workspaces named ON named.id=t.project_workspace_id
 LEFT JOIN artifact_sessions artifact_session ON artifact_session.id=t.artifact_session_id
 WHERE h.id=? AND h.workspace_id=? AND h.session_id=?
 AND (t.project_id IS NULL OR (project.workspace_id=h.workspace_id AND project.status='active'))
 AND (t.project_workspace_id IS NULL OR (t.project_id IS NOT NULL AND named.project_id=t.project_id AND named.status='active'))
 AND (t.artifact_session_id IS NULL OR (artifact_session.workspace_id=h.workspace_id AND artifact_session.status IN ('active','published')))
 AND length(CAST(h.question AS BLOB)) BETWEEN 1 AND 512
 AND length(CAST(m.display_name AS BLOB)) BETWEEN 1 AND 256
 AND parent.objective=json_extract(CASE WHEN length(CAST(manifest.snapshot_json AS BLOB))<=262144 AND json_valid(manifest.snapshot_json) THEN manifest.snapshot_json ELSE '{}' END,'$.task_objective')
 AND length(CAST(t.completion_json AS BLOB))<=65536 AND length(CAST(parent.completion_json AS BLOB))<=65536
 AND length(CAST(a.completion_sha256 AS BLOB))=64 AND length(CAST(root.completion_sha256 AS BLOB))=64
 AND length(CAST(p.plan_json AS BLOB))<=65536 AND length(CAST(manifest.snapshot_json AS BLOB))<=262144 AND length(CAST(manifest.snapshot_sha256 AS BLOB))=64
 AND length(CAST(root.prior_dependencies_json AS BLOB))<=16384 AND length(CAST(m.config_json AS BLOB))<=16384 AND length(CAST(notice.content_json AS BLOB))<=4096
 AND length(CAST(m.role_name AS BLOB))<=256 AND length(CAST(m.capability_id AS BLOB))<=256
 AND length(CAST(m.protocol_level AS BLOB))<=256 AND length(CAST(COALESCE(m.principal_id,'') AS BLOB))<=256 AND length(CAST(m.route_policy_json AS BLOB))<=16384`
	for _, column := range []string{"h.id", "h.workspace_id", "h.session_id", "h.plan_id", "h.assignment_key", "h.member_id", "s.task_id", "h.task_id", "h.attempt_id", "h.run_id", "h.wait_step_id", "h.notice_message_id", "h.author_principal_id", "s.team_id", "COALESCE(t.project_id,'')", "COALESCE(t.project_workspace_id,'')", "COALESCE(t.artifact_session_id,'')"} {
		query += " AND length(CAST(" + column + " AS BLOB))<=256"
	}
	return query
}

// AssistanceRequest observes one exact request inside a single database
// transaction. Current human/Worker membership and the accepted identity are
// checked again on each read. Pauses are readable; terminal/replaced owners are
// denied. This intentionally does not call execution admission or release holds.
func (s *Service) AssistanceRequest(ctx context.Context, workspaceID, sessionID, requestID, readerPrincipalID string) (AssistanceRequest, error) {
	var out AssistanceRequest
	if s == nil || s.db == nil || s.tx == nil {
		return out, ErrInvalid
	}
	for _, value := range []string{workspaceID, sessionID, requestID, readerPrincipalID} {
		if value == "" || len(value) > 256 || !utf8.ValidString(value) {
			return out, ErrInvalid
		}
	}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		out, err = s.assistanceRequestTx(ctx, tx, workspaceID, sessionID, requestID, readerPrincipalID)
		return err
	})
	if err != nil {
		return AssistanceRequest{}, err
	}
	return out, nil
}

// assistanceRequestTx shares the exact bounded observation with domain writes.
// The caller owns the transaction; this helper never starts another one.
func (s *Service) assistanceRequestTx(ctx context.Context, tx storage.Tx, workspaceID, sessionID, requestID, readerPrincipalID string) (AssistanceRequest, error) {
	var out AssistanceRequest
	err := func() error {
		var completion, seal, parentCompletion, parentSeal, planRaw, snapshotRaw, snapshotSeal, priorRaw, memberConfig, noticeRaw, teamID string
		var role, capability, protocol, memberPrincipal, routePolicy string
		err := tx.QueryRowContext(ctx, assistanceRequestReadSQL, readerPrincipalID, requestID, workspaceID, sessionID).Scan(&out.RequestID, &out.WorkspaceID, &out.SessionID, &out.PlanID, &out.AssignmentKey, &out.MemberID, &out.MemberName, &out.ParentTaskID, &out.TaskID, &out.AttemptID, &out.RunID, &out.WaitStepID, &out.NoticeMessageID, &out.AuthorPrincipalID, &out.Question, &out.ProjectID, &out.ProjectWorkspaceID, &out.ArtifactSessionID, &out.TaskState, &out.TaskRevision, &out.WorkerRevision, &out.CreatedAt, &completion, &seal, &parentCompletion, &parentSeal, &planRaw, &snapshotRaw, &snapshotSeal, &priorRaw, &memberConfig, &noticeRaw, &teamID, &role, &capability, &protocol, &memberPrincipal, &routePolicy)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		for _, value := range []string{out.RequestID, out.WorkspaceID, out.SessionID, out.PlanID, out.AssignmentKey, out.MemberID, out.MemberName, out.ParentTaskID, out.TaskID, out.AttemptID, out.RunID, out.WaitStepID, out.NoticeMessageID, out.AuthorPrincipalID, out.Question, out.ProjectID, out.ProjectWorkspaceID, out.ArtifactSessionID, completion, parentCompletion, planRaw, snapshotRaw, memberConfig, noticeRaw} {
			if !utf8.ValidString(value) {
				return ErrInvalid
			}
		}
		if !assistanceSeal(completion, seal) || !assistanceSeal(parentCompletion, parentSeal) || !assistanceSeal(snapshotRaw, snapshotSeal) || strings.TrimSpace(out.Question) == "" || out.CreatedAt <= 0 {
			return ErrInvalid
		}
		var notice struct {
			Text    string `json:"text"`
			Request string `json:"assistance_request_id"`
			Task    string `json:"task_id"`
			Member  string `json:"target_member_id"`
		}
		if json.Unmarshal([]byte(noticeRaw), &notice) != nil || notice.Request != out.RequestID || notice.Task != out.TaskID || notice.Member != out.MemberID || notice.Text != "Assignment "+out.AssignmentKey+" needs human input." {
			return ErrInvalid
		}
		var snapshot SessionSnapshot
		if json.Unmarshal([]byte(snapshotRaw), &snapshot) != nil || snapshot.SessionID != sessionID || snapshot.TaskID != out.ParentTaskID || snapshot.TeamID != teamID || snapshot.ExecutionMode != "team" || snapshot.ResearchMode || len(snapshot.Members) > 32 {
			return ErrInvalid
		}
		var frozen *SnapshotMember
		for i := range snapshot.Members {
			if snapshot.Members[i].ID == out.MemberID {
				frozen = &snapshot.Members[i]
			}
		}
		if frozen == nil || frozen.RoleName != role || frozen.CapabilityID != capability || frozen.ProtocolLevel != protocol {
			return ErrInvalid
		}
		if frozen.DisplayName == "" || len(frozen.DisplayName) > 256 || !utf8.ValidString(frozen.DisplayName) {
			return ErrInvalid
		}
		out.MemberName = frozen.DisplayName
		principal := ""
		if frozen.PrincipalID != nil {
			principal = *frozen.PrincipalID
		}
		if principal != memberPrincipal || !assistanceEqualJSON([]byte(memberConfig), frozen.Config) || !assistanceEqualJSON([]byte(routePolicy), frozen.RoutePolicy) {
			return ErrInvalid
		}
		return validateAssistanceGraph(ctx, tx, out, planRaw, priorRaw, memberConfig, snapshot)
	}()
	if err != nil {
		return AssistanceRequest{}, err
	}
	return out, nil
}

func assistanceSeal(raw, seal string) bool {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]) == seal
}

func assistanceEqualJSON(a, b json.RawMessage) bool {
	left, err := canonical(a)
	if err != nil {
		return false
	}
	right, err := canonical(b)
	return err == nil && string(left) == string(right)
}

func validateAssistanceGraph(ctx context.Context, tx storage.Tx, out AssistanceRequest, planRaw, priorRaw, memberConfig string, snapshot SessionSnapshot) error {
	specs, err := assignmentSpecs(json.RawMessage(planRaw))
	if err != nil || len(specs) == 0 {
		return ErrInvalid
	}
	members := map[string]SnapshotMember{}
	for _, m := range snapshot.Members {
		if _, duplicate := members[m.ID]; duplicate {
			return ErrInvalid
		}
		members[m.ID] = m
	}
	type binding struct {
		key, task, member, objective, deps, profile string
		revision                                    int64
	}
	rows, err := tx.QueryContext(ctx, assistanceGraphReadSQL, out.SessionID)
	if err != nil {
		return err
	}
	bindings := map[string]binding{}
	for rows.Next() {
		var b binding
		var valid bool
		if err = rows.Scan(&b.key, &b.task, &b.member, &b.objective, &b.deps, &b.profile, &b.revision, &valid); err != nil {
			break
		}
		if !valid || len(bindings) >= 8 {
			err = ErrInvalid
			break
		}
		for _, value := range []string{b.key, b.task, b.member, b.objective, b.deps, b.profile} {
			if !utf8.ValidString(value) {
				err = ErrInvalid
				break
			}
		}
		if err != nil {
			break
		}
		if _, exists := bindings[b.key]; exists {
			err = ErrInvalid
			break
		}
		bindings[b.key] = b
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
	if len(bindings) != len(specs) {
		return ErrInvalid
	}
	var prior []string
	if json.Unmarshal([]byte(priorRaw), &prior) != nil || len(prior) > 32 {
		return ErrInvalid
	}
	rootDeps := map[string]bool{}
	ownDeps := map[string]bool{}
	for _, id := range prior {
		if id == "" || len(id) > 256 || !utf8.ValidString(id) || rootDeps[id] {
			return ErrInvalid
		}
		rootDeps[id] = true
		ownDeps[id] = true
	}
	found := false
	for _, spec := range specs {
		b, ok := bindings[spec.Key]
		if !ok || b.member != spec.MemberID || b.objective != strings.TrimSpace(spec.Objective) {
			return ErrInvalid
		}
		frozen, ok := members[b.member]
		if !ok || frozen.MemberKind != "agent" || frozen.Status != "active" {
			return ErrInvalid
		}
		profile, revision := profileIDFromMemberConfig(frozen.Config), int64(0)
		if frozen.Profile != nil {
			profile, revision = frozen.Profile.ID, frozen.Profile.Revision
		}
		if b.profile != profile || b.revision != revision {
			return ErrInvalid
		}
		var declared []string
		if json.Unmarshal([]byte(b.deps), &declared) != nil || len(declared) != len(spec.DependsOn) {
			return ErrInvalid
		}
		for i, key := range declared {
			if key != spec.DependsOn[i] {
				return ErrInvalid
			}
		}
		if rootDeps[b.task] {
			return ErrInvalid
		}
		rootDeps[b.task] = true
		if b.task == out.TaskID {
			if found || b.key != out.AssignmentKey || b.member != out.MemberID || profileIDFromMemberConfig(json.RawMessage(memberConfig)) != profile {
				return ErrInvalid
			}
			found = true
			if revision > 0 {
				var current int64
				if err = tx.QueryRowContext(ctx, `SELECT revision FROM agent_profiles WHERE id=? AND status='active' AND (workspace_id=? OR (id='agent.md' AND workspace_id IS NULL))`, profile, out.WorkspaceID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
					return ErrInvalid
				} else if err != nil {
					return err
				}
				if current != revision {
					return ErrInvalid
				}
			}
			for _, key := range spec.DependsOn {
				pred, ok := bindings[key]
				if !ok {
					return ErrInvalid
				}
				ownDeps[pred.task] = true
			}
		}
	}
	if !found {
		return ErrInvalid
	}
	for id, expected := range map[string]map[string]bool{out.ParentTaskID: rootDeps, out.TaskID: ownDeps} {
		if err = assistanceDependencies(ctx, tx, id, out.WorkspaceID, expected); err != nil {
			return err
		}
	}
	return nil
}

const assistanceGraphReadSQL = `SELECT
 CASE WHEN length(CAST(a.assignment_key AS BLOB))<=256 THEN a.assignment_key ELSE '' END,
 CASE WHEN length(CAST(a.task_id AS BLOB))<=256 THEN a.task_id ELSE '' END,
 CASE WHEN length(CAST(a.member_id AS BLOB))<=256 THEN a.member_id ELSE '' END,
 CASE WHEN length(CAST(t.objective AS BLOB))<=4096 THEN t.objective ELSE '' END,
 CASE WHEN length(CAST(a.depends_on_json AS BLOB))<=4096 THEN a.depends_on_json ELSE '' END,
 CASE WHEN length(CAST(a.profile_id AS BLOB))<=256 THEN a.profile_id ELSE '' END,a.profile_revision,
 COALESCE(length(CAST(a.assignment_key AS BLOB)) BETWEEN 1 AND 256 AND length(CAST(a.task_id AS BLOB)) BETWEEN 1 AND 256
 AND length(CAST(a.member_id AS BLOB)) BETWEEN 1 AND 256 AND length(CAST(t.objective AS BLOB))<=4096
 AND length(CAST(a.depends_on_json AS BLOB))<=4096 AND length(CAST(a.profile_id AS BLOB)) BETWEEN 1 AND 256
 AND a.workspace_id=s.workspace_id AND a.plan_id=s.accepted_plan_id AND a.parent_task_id=s.task_id
 AND t.workspace_id=a.workspace_id AND t.parent_task_id=a.parent_task_id AND t.archived_at IS NULL
 AND t.project_id IS parent.project_id AND t.project_workspace_id IS parent.project_workspace_id AND t.artifact_session_id IS parent.artifact_session_id,0)
 FROM team_execution_assignments a JOIN team_sessions s ON s.id=a.session_id
 LEFT JOIN tasks t ON t.id=a.task_id LEFT JOIN tasks parent ON parent.id=s.task_id
 WHERE a.session_id=? LIMIT 9`

func assistanceDependencies(ctx context.Context, tx storage.Tx, id, workspace string, expected map[string]bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN length(CAST(d.depends_on_task_id AS BLOB))<=256 THEN d.depends_on_task_id ELSE '' END,
 COALESCE(d.dependency_type='hard' AND d.dependency_mode='all' AND t.workspace_id=? AND length(CAST(d.depends_on_task_id AS BLOB)) BETWEEN 1 AND 256,0)
 FROM task_dependencies d LEFT JOIN tasks t ON t.id=d.depends_on_task_id WHERE d.task_id=? LIMIT 41`, workspace, id)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for rows.Next() {
		var dep string
		var valid bool
		if err = rows.Scan(&dep, &valid); err != nil {
			break
		}
		if !valid || !utf8.ValidString(dep) || seen[dep] || !expected[dep] || len(seen) >= 40 {
			err = ErrInvalid
			break
		}
		seen[dep] = true
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
	if len(seen) != len(expected) {
		return ErrInvalid
	}
	return nil
}
