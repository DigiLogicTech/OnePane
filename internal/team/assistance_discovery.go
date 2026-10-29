package team

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// AssistanceRequests discovers only current pending original-owner requests.
// The accepted assignment graph bounds discovery, not recent room traffic or
// a global historical request window. Observation grants no execution authority.
func (s *Service) AssistanceRequests(ctx context.Context, workspaceID, sessionID, readerPrincipalID string) ([]AssistanceRequest, error) {
	if s == nil || s.db == nil || s.tx == nil {
		return nil, ErrInvalid
	}
	for _, value := range []string{workspaceID, sessionID, readerPrincipalID} {
		if !assistanceDiscoveryID(value) {
			return nil, ErrInvalid
		}
	}
	out := make([]AssistanceRequest, 0)
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var rootID, teamID, planID, completion, seal, planRaw, snapshotRaw, snapshotSeal, priorRaw string
		err := tx.QueryRowContext(ctx, assistanceDiscoverySessionSQL, readerPrincipalID, sessionID, workspaceID).Scan(&rootID, &teamID, &planID, &completion, &seal, &planRaw, &snapshotRaw, &snapshotSeal, &priorRaw)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		for _, value := range []string{rootID, teamID, planID} {
			if !assistanceDiscoveryID(value) {
				return ErrInvalid
			}
		}
		for _, value := range []string{completion, planRaw, snapshotRaw, priorRaw} {
			if !utf8.ValidString(value) {
				return ErrInvalid
			}
		}
		if !assistanceSeal(completion, seal) || !assistanceSeal(snapshotRaw, snapshotSeal) {
			return ErrInvalid
		}
		var snapshot SessionSnapshot
		if json.Unmarshal([]byte(snapshotRaw), &snapshot) != nil || snapshot.SessionID != sessionID || snapshot.TaskID != rootID || snapshot.TeamID != teamID || snapshot.ExecutionMode != "team" || snapshot.ResearchMode || len(snapshot.Members) > 32 {
			return ErrInvalid
		}
		specs, err := assignmentSpecs(json.RawMessage(planRaw))
		if err != nil || len(specs) == 0 || len(specs) > 8 {
			return ErrInvalid
		}
		// Read every physical assignment, including malformed/mismatched rows. SQL
		// predicates must not silently suppress a damaged ninth or current binding.
		rows, err := tx.QueryContext(ctx, assistanceGraphReadSQL, sessionID)
		if err != nil {
			return err
		}
		type assignment struct{ key, task, member string }
		assignments := make([]assignment, 0, 8)
		for rows.Next() {
			var a assignment
			var objective, deps, profile string
			var revision int64
			var valid bool
			if err = rows.Scan(&a.key, &a.task, &a.member, &objective, &deps, &profile, &revision, &valid); err != nil {
				break
			}
			if !valid || len(assignments) >= 8 || !assistanceDiscoveryID(a.key) || !assistanceDiscoveryID(a.task) || !assistanceDiscoveryID(a.member) {
				err = ErrInvalid
				break
			}
			assignments = append(assignments, a)
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
		if len(assignments) != len(specs) {
			return ErrInvalid
		}
		for _, a := range assignments {
			var childCompletion, childSeal string
			err = tx.QueryRowContext(ctx, `SELECT t.completion_json,a.completion_sha256 FROM tasks t
 JOIN team_execution_assignments a ON a.task_id=t.id AND a.session_id=? AND a.plan_id=? AND a.workspace_id=? AND a.parent_task_id=?
 JOIN task_execution_profiles ep ON ep.task_id=t.id AND ep.workspace_id=a.workspace_id AND ep.team_id=? AND ep.team_session_id=a.session_id AND ep.execution_mode='team'
 WHERE t.id=? AND length(CAST(t.completion_json AS BLOB))<=65536 AND length(CAST(a.completion_sha256 AS BLOB))=64`, sessionID, planID, workspaceID, rootID, teamID, a.task).Scan(&childCompletion, &childSeal)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvalid
			}
			if err != nil {
				return err
			}
			if !utf8.ValidString(childCompletion) || !assistanceSeal(childCompletion, childSeal) {
				return ErrInvalid
			}
			var memberConfig, role, capability, protocol, principal, route string
			err = tx.QueryRowContext(ctx, assistanceDiscoveryMemberSQL, a.member, teamID, workspaceID).Scan(&memberConfig, &role, &capability, &protocol, &principal, &route)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvalid
			}
			if err != nil {
				return err
			}
			for _, value := range []string{memberConfig, role, capability, protocol, principal, route} {
				if !utf8.ValidString(value) {
					return ErrInvalid
				}
			}
			var frozen *SnapshotMember
			for i := range snapshot.Members {
				if snapshot.Members[i].ID == a.member {
					if frozen != nil {
						return ErrInvalid
					}
					frozen = &snapshot.Members[i]
				}
			}
			if frozen == nil || frozen.RoleName != role || frozen.CapabilityID != capability || frozen.ProtocolLevel != protocol || !assistanceDiscoveryID(frozen.DisplayName) || !assistanceEqualJSON([]byte(memberConfig), frozen.Config) || !assistanceEqualJSON([]byte(route), frozen.RoutePolicy) {
				return ErrInvalid
			}
			frozenPrincipal := ""
			if frozen.PrincipalID != nil {
				frozenPrincipal = *frozen.PrincipalID
			}
			if frozenPrincipal != principal {
				return ErrInvalid
			}
			binding := AssistanceRequest{WorkspaceID: workspaceID, SessionID: sessionID, PlanID: planID, ParentTaskID: rootID, TaskID: a.task, AssignmentKey: a.key, MemberID: a.member}
			if err = validateAssistanceGraph(ctx, tx, binding, planRaw, priorRaw, memberConfig, snapshot); err != nil {
				return err
			}
			var attempt, run, step, request sql.NullString
			var workerStatus, kind, stepStatus sql.NullString
			err = tx.QueryRowContext(ctx, assistanceDiscoveryCandidateSQL, a.task).Scan(&attempt, &run, &workerStatus, &step, &kind, &stepStatus, &request)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if !workerStatus.Valid || workerStatus.String != "waiting" {
				continue
			}
			for _, value := range []sql.NullString{attempt, run, step} {
				if value.Valid && !assistanceDiscoveryID(value.String) {
					return ErrInvalid
				}
			}
			if !kind.Valid || kind.String != "human" || !stepStatus.Valid || stepStatus.String != "waiting" {
				continue
			}
			if !request.Valid {
				continue
			}
			if !assistanceDiscoveryID(request.String) {
				return ErrInvalid
			}
			observed, err := s.assistanceRequestTx(ctx, tx, workspaceID, sessionID, request.String, readerPrincipalID)
			if err != nil {
				return err
			}
			if observed.TaskID != a.task || observed.AttemptID != attempt.String || observed.RunID != run.String || observed.WaitStepID != step.String {
				return ErrInvalid
			}
			out = append(out, observed)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RequestID < out[j].RequestID })
	return out, nil
}

func assistanceDiscoveryID(value string) bool {
	return len(value) <= 256 && utf8.ValidString(value) && strings.TrimSpace(value) != ""
}

const assistanceDiscoverySessionSQL = `SELECT parent.id,s.team_id,s.accepted_plan_id,parent.completion_json,root.completion_sha256,p.plan_json,manifest.snapshot_json,manifest.snapshot_sha256,root.prior_dependencies_json
 FROM team_sessions s
 JOIN teams g ON g.id=s.team_id AND g.workspace_id=s.workspace_id AND g.status='active'
 JOIN tasks parent ON parent.id=s.task_id AND parent.workspace_id=s.workspace_id AND parent.archived_at IS NULL AND parent.state IN ('created','ready','running','waiting_dependency','waiting_approval','paused')
 JOIN task_execution_profiles ep ON ep.task_id=parent.id AND ep.workspace_id=s.workspace_id AND ep.team_id=s.team_id AND ep.team_session_id=s.id AND ep.execution_mode='team'
 JOIN team_plans p ON p.id=s.accepted_plan_id AND p.session_id=s.id AND p.workspace_id=s.workspace_id AND p.status='accepted'
 JOIN team_assignment_roots root ON root.session_id=s.id AND root.plan_id=p.id AND root.task_id=parent.id
 JOIN team_session_manifests manifest ON manifest.session_id=s.id AND manifest.workspace_id=s.workspace_id AND manifest.team_id=s.team_id AND manifest.execution_mode='team' AND manifest.research_mode=0
 JOIN workspaces tenant ON tenant.id=s.workspace_id AND tenant.status='active'
 JOIN principals reader ON reader.id=? AND reader.status='active'
 JOIN workspace_memberships membership ON membership.principal_id=reader.id AND membership.workspace_id=s.workspace_id AND membership.status='active'
 LEFT JOIN projects project ON project.id=parent.project_id
 LEFT JOIN project_workspaces named ON named.id=parent.project_workspace_id
 LEFT JOIN artifact_sessions artifact_session ON artifact_session.id=parent.artifact_session_id
 WHERE s.id=? AND s.workspace_id=? AND s.status IN ('plan_accepted','executing','paused_for_deliberation')
 AND (parent.project_id IS NULL OR (project.workspace_id=s.workspace_id AND project.status='active'))
 AND (parent.project_workspace_id IS NULL OR (parent.project_id IS NOT NULL AND named.project_id=parent.project_id AND named.status='active'))
 AND (parent.artifact_session_id IS NULL OR (artifact_session.workspace_id=s.workspace_id AND artifact_session.status IN ('active','published')))
 AND length(CAST(parent.id AS BLOB)) BETWEEN 1 AND 256 AND length(CAST(s.team_id AS BLOB)) BETWEEN 1 AND 256 AND length(CAST(s.accepted_plan_id AS BLOB)) BETWEEN 1 AND 256
 AND length(CAST(parent.completion_json AS BLOB))<=65536 AND length(CAST(root.completion_sha256 AS BLOB))=64
 AND length(CAST(p.plan_json AS BLOB))<=65536 AND length(CAST(manifest.snapshot_json AS BLOB))<=262144 AND length(CAST(manifest.snapshot_sha256 AS BLOB))=64 AND length(CAST(root.prior_dependencies_json AS BLOB))<=16384
 AND parent.objective=json_extract(CASE WHEN length(CAST(manifest.snapshot_json AS BLOB))<=262144 AND json_valid(manifest.snapshot_json) THEN manifest.snapshot_json ELSE '{}' END,'$.task_objective')`

const assistanceDiscoveryMemberSQL = `SELECT config_json,role_name,capability_id,protocol_level,COALESCE(principal_id,''),route_policy_json FROM team_members
 WHERE id=? AND team_id=? AND workspace_id=? AND member_kind='agent' AND status='active'
 AND length(CAST(config_json AS BLOB))<=16384 AND length(CAST(role_name AS BLOB))<=256 AND length(CAST(capability_id AS BLOB))<=256 AND length(CAST(protocol_level AS BLOB))<=256
 AND length(CAST(COALESCE(principal_id,'') AS BLOB))<=256 AND length(CAST(route_policy_json AS BLOB))<=16384`

const assistanceDiscoveryCandidateSQL = `SELECT
 CASE WHEN length(CAST(attempt.id AS BLOB)) BETWEEN 1 AND 256 THEN attempt.id ELSE '' END,
 CASE WHEN r.id IS NULL THEN NULL WHEN length(CAST(r.id AS BLOB)) BETWEEN 1 AND 256 THEN r.id ELSE '' END,r.status,
 CASE WHEN step.id IS NULL THEN NULL WHEN length(CAST(step.id AS BLOB)) BETWEEN 1 AND 256 THEN step.id ELSE '' END,step.step_kind,step.status,
 CASE WHEN h.id IS NULL THEN NULL WHEN length(CAST(h.id AS BLOB)) BETWEEN 1 AND 256 THEN h.id ELSE '' END
 FROM task_attempts attempt
 LEFT JOIN agent_worker_runs r ON r.attempt_id=attempt.id
 LEFT JOIN agent_worker_steps step ON step.run_id=r.id AND step.step_number=(SELECT MAX(step_number) FROM agent_worker_steps WHERE run_id=r.id)
 LEFT JOIN team_assistance_requests h ON h.wait_step_id=step.id
 WHERE attempt.id=(SELECT id FROM task_attempts WHERE task_id=? ORDER BY attempt_number DESC LIMIT 1)`
