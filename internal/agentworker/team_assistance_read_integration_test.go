//go:build integration

package agentworker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

func TestTeamAssistanceReadActualHumanWait(t *testing.T) {
	w, ctx, run, teams, session, _ := prepareTeamProposalContinuation(t, "human")
	if result := applyWorkerWait(t, w, ctx, run, "human"); result.Status != "waiting_human" {
		t.Fatalf("wait %+v", result)
	}
	var request string
	if err := w.db.QueryRowContext(ctx, `SELECT id FROM team_assistance_requests WHERE run_id=?`, run.ID).Scan(&request); err != nil {
		t.Fatal(err)
	}
	want, err := teams.AssistanceRequest(ctx, run.WorkspaceID, session, request, "proposal-human")
	if err != nil {
		t.Fatal(err)
	}
	if want.RequestID != request || want.TaskID != run.TaskID || want.AttemptID != run.AttemptID || want.RunID != run.ID || want.Question != "Review the recorded contribution?" || want.TaskState != "waiting_approval" || want.TaskRevision < 1 || want.WorkerRevision < 1 || want.MemberName != "Builder" {
		t.Fatalf("wrong observed request %+v", want)
	}
	// A genuine Team hold does not hide a request and never resumes its owner.
	root, err := w.tasks.Get(ctx, want.ParentTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.PauseExecution(ctx, team.ExecutionControlCommand{SessionID: session, PlanID: want.PlanID, ActorPrincipalID: "proposal-human", ExpectedTaskRevision: root.Revision, Reason: "review while held"}); err != nil {
		t.Fatal(err)
	}
	held, err := teams.AssistanceRequest(ctx, run.WorkspaceID, session, request, "proposal-human")
	if err != nil {
		t.Fatalf("held request unreadable: %v", err)
	}
	if held.Question != want.Question || held.RunID != want.RunID {
		t.Fatal("pause replaced original owner")
	}
	var taskState, attemptState, workerState string
	if err = w.db.QueryRowContext(ctx, `SELECT t.state,a.status,r.status FROM tasks t JOIN task_attempts a ON a.id=? JOIN agent_worker_runs r ON r.id=? WHERE t.id=?`, run.AttemptID, run.ID, run.TaskID).Scan(&taskState, &attemptState, &workerState); err != nil {
		t.Fatal(err)
	}
	if taskState != want.TaskState || attemptState != "waiting" || workerState != "waiting" {
		t.Fatalf("read released hold: %s/%s/%s", taskState, attemptState, workerState)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = teams.AssistanceRequest(canceled, run.WorkspaceID, session, request, "proposal-human"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation concealed: %v", err)
	}
}

func TestTeamAssistanceReadDeniesChangedAuthority(t *testing.T) {
	for _, change := range []string{"foreign_tenant", "foreign_session", "foreign_request", "foreign_reader", "revoked_reader", "reader_membership", "revoked_worker", "worker_membership", "human_worker", "member_disabled", "member_human", "member_profile", "member_role", "member_capability", "member_protocol", "member_configuration", "member_route", "root_seal", "child_seal", "plan_body", "accepted_plan", "snapshot_hash", "assignment_tenant", "changed_parent", "task_complete", "attempt_terminal", "attempt_provenance", "duplicate_active_attempt", "worker_terminal", "worker_continuation", "step_kind", "step_newer", "notice_author", "notice_question", "notice_oversized", "question_oversized", "root_oversized", "plan_oversized", "snapshot_oversized", "member_name_oversized", "dependency_removed"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, run, teams, session, _ := prepareTeamProposalContinuation(t, "human")
			if result := applyWorkerWait(t, w, ctx, run, "human"); result.Status != "waiting_human" {
				t.Fatalf("wait %+v", result)
			}
			var request, root string
			if err := w.db.QueryRowContext(ctx, `SELECT h.id,s.task_id FROM team_assistance_requests h JOIN team_sessions s ON s.id=h.session_id WHERE h.run_id=?`, run.ID).Scan(&request, &root); err != nil {
				t.Fatal(err)
			}
			if _, err := teams.AssistanceRequest(ctx, run.WorkspaceID, session, request, "proposal-human"); err != nil {
				t.Fatalf("healthy read %v", err)
			}
			ws, reader := run.WorkspaceID, "proposal-human"
			statement := ""
			switch change {
			case "foreign_tenant":
				ws = "foreign"
			case "foreign_session":
				session = "foreign"
			case "foreign_request":
				request = "foreign"
			case "foreign_reader":
				reader = "foreign"
			case "revoked_reader":
				statement = `UPDATE principals SET status='revoked' WHERE id='proposal-human'`
			case "reader_membership":
				statement = `UPDATE workspace_memberships SET status='revoked' WHERE principal_id='proposal-human'`
			case "revoked_worker":
				statement = `UPDATE principals SET status='revoked' WHERE id='system:agent-worker'`
			case "worker_membership":
				statement = `UPDATE workspace_memberships SET status='revoked' WHERE principal_id='system:agent-worker'`
			case "human_worker":
				statement = `UPDATE principals SET principal_type='human' WHERE id='system:agent-worker'`
			case "member_disabled":
				statement = `UPDATE team_members SET status='disabled' WHERE id=(SELECT member_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "member_human":
				statement = `UPDATE team_members SET member_kind='human' WHERE id=(SELECT member_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "member_profile":
				statement = `UPDATE team_members SET config_json='{"agent_profile_id":"changed"}' WHERE id=(SELECT member_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "member_role":
				statement = `UPDATE team_members SET role_name='changed' WHERE id=(SELECT member_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "member_capability":
				statement = `UPDATE team_members SET capability_id='changed' WHERE id=(SELECT member_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "member_protocol":
				statement = `UPDATE team_members SET protocol_level='L0' WHERE id=(SELECT member_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "member_configuration":
				statement = `UPDATE team_members SET config_json='{"changed":true}' WHERE id=(SELECT member_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "member_route":
				statement = `UPDATE team_members SET route_policy_json='{"changed":true}' WHERE id=(SELECT member_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "root_seal":
				statement = `UPDATE tasks SET completion_json='{"changed":true}' WHERE id='` + root + `'`
			case "child_seal":
				statement = `UPDATE tasks SET completion_json='{"changed":true}' WHERE id='` + run.TaskID + `'`
			case "plan_body":
				statement = `UPDATE team_plans SET plan_json='{"assignments":[]}' WHERE id=(SELECT plan_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "accepted_plan":
				statement = `UPDATE team_sessions SET accepted_plan_id=NULL WHERE id='` + session + `'`
			case "snapshot_hash":
				statement = `UPDATE team_session_manifests SET snapshot_sha256='` + strings.Repeat("0", 64) + `' WHERE session_id='` + session + `'`
			case "assignment_tenant":
				statement = `DROP TRIGGER team_assignment_immutable; UPDATE team_execution_assignments SET workspace_id='foreign' WHERE task_id='` + run.TaskID + `'`
			case "changed_parent":
				statement = `UPDATE tasks SET parent_task_id=NULL WHERE id='` + run.TaskID + `'`
			case "task_complete":
				statement = `UPDATE tasks SET state='complete' WHERE id='` + run.TaskID + `'`
			case "attempt_terminal":
				statement = `UPDATE task_attempts SET status='succeeded' WHERE id='` + run.AttemptID + `'`
			case "attempt_provenance":
				statement = `UPDATE task_attempts SET metadata_json='{}' WHERE id='` + run.AttemptID + `'`
			case "duplicate_active_attempt":
				statement = `DROP INDEX idx_task_one_active_attempt; UPDATE task_attempts SET attempt_number=2 WHERE id='` + run.AttemptID + `'; INSERT INTO task_attempts(id,task_id,attempt_number,worker_principal_id,status,metadata_json) VALUES('duplicate','` + run.TaskID + `',1,'system:agent-worker','waiting','{"agent_worker":"v1"}')`
			case "worker_terminal":
				statement = `UPDATE agent_worker_runs SET status='succeeded' WHERE id='` + run.ID + `'`
			case "worker_continuation":
				statement = `UPDATE agent_worker_runs SET continuation_json='{}' WHERE id='` + run.ID + `'`
			case "step_kind":
				statement = `UPDATE agent_worker_steps SET step_kind='wait' WHERE id=(SELECT wait_step_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "step_newer":
				statement = `INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,detail_json,started_at,completed_at) SELECT 'later',run_id,step_number+1,'wait','waiting','{}',started_at,completed_at FROM agent_worker_steps WHERE id=(SELECT wait_step_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "notice_author":
				statement = `UPDATE team_messages SET author_principal_id='proposal-human' WHERE id=(SELECT notice_message_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "notice_question":
				statement = `UPDATE team_messages SET content_json='{"assistance_request_id":"foreign"}' WHERE id=(SELECT notice_message_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "notice_oversized":
				statement = `UPDATE team_messages SET content_json=json_object('padding',printf('%65536s','x')) WHERE id=(SELECT notice_message_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "question_oversized":
				statement = `DROP TRIGGER team_assistance_request_immutable; PRAGMA ignore_check_constraints=ON; UPDATE team_assistance_requests SET question=printf('%65536s','x') WHERE id='` + request + `'`
			case "root_oversized":
				statement = `UPDATE tasks SET completion_json=json_object('padding',printf('%65536s','x')) WHERE id='` + root + `'`
			case "plan_oversized":
				statement = `UPDATE team_plans SET plan_json=json_object('padding',printf('%65536s','x')) WHERE id=(SELECT plan_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "snapshot_oversized":
				statement = `UPDATE team_session_manifests SET snapshot_json=json_object('padding',printf('%262144s','x')) WHERE session_id='` + session + `'`
			case "member_name_oversized":
				statement = `UPDATE team_members SET display_name=printf('%65536s','x') WHERE id=(SELECT member_id FROM team_assistance_requests WHERE id='` + request + `')`
			case "dependency_removed":
				statement = `DELETE FROM task_dependencies WHERE task_id='` + root + `'`
			}
			// Corrupt-history negatives use an isolated connection with foreign keys off;
			// the read must refuse independently of database write-time constraints.
			if statement != "" {
				if change == "assignment_tenant" {
					conn, err := w.db.Conn(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
						t.Fatal(err)
					}
					_, err = conn.ExecContext(ctx, statement)
					if _, reset := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); reset != nil {
						t.Fatal(reset)
					}
					conn.Close()
					if err != nil {
						t.Fatal(err)
					}
				} else if _, err := w.db.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := teams.AssistanceRequest(ctx, ws, session, request, reader); !errors.Is(err, team.ErrInvalid) {
				t.Fatalf("changed %s leaked private read or wrong error: %v", change, err)
			}
		})
	}
}

func TestTeamAssistanceReadRejectsOversizedCallerIdentity(t *testing.T) {
	w, ctx, _, teams, session, _ := prepareTeamProposalContinuation(t, "human")
	for _, args := range [][4]string{{strings.Repeat("x", 257), session, "missing", "proposal-human"}, {"tenant", session, strings.Repeat("x", 257), "proposal-human"}, {"tenant", session, "missing", string([]byte{0xff})}} {
		if _, err := teams.AssistanceRequest(ctx, args[0], args[1], args[2], args[3]); err != team.ErrInvalid {
			t.Fatalf("identity %s: %v", fmt.Sprint(args), err)
		}
	}
	_ = w
}

func TestTeamAssistanceReadDeniesCoherentScopeRelocation(t *testing.T) {
	for _, scope := range []string{"project", "named_workspace", "artifact_session"} {
		t.Run(scope, func(t *testing.T) {
			w, ctx, run, teams, session, _ := prepareTeamProposalContinuation(t, "human")
			if result := applyWorkerWait(t, w, ctx, run, "human"); result.Status != "waiting_human" {
				t.Fatalf("wait %+v", result)
			}
			var request, root string
			if err := w.db.QueryRowContext(ctx, `SELECT h.id,s.task_id FROM team_assistance_requests h JOIN team_sessions s ON s.id=h.session_id WHERE h.run_id=?`, run.ID).Scan(&request, &root); err != nil {
				t.Fatal(err)
			}
			for _, statement := range []string{
				`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('relocated-project','tenant','Relocated','active','{}','{}','proposal-human',1,1)`,
				`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('relocated-workspace','relocated-project','Relocated',1,1)`,
				`INSERT INTO artifact_sessions(id,workspace_id,title,format,status,created_by,created_at,updated_at) VALUES('relocated-session','tenant','Relocated','text','active','proposal-human',1,1)`,
			} {
				if _, err := w.db.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			update := `UPDATE tasks SET project_id='relocated-project' WHERE id IN (?,?)`
			if scope == "named_workspace" {
				update = `UPDATE tasks SET project_id='relocated-project',project_workspace_id='relocated-workspace' WHERE id IN (?,?)`
			}
			if scope == "artifact_session" {
				update = `UPDATE tasks SET artifact_session_id='relocated-session' WHERE id IN (?,?)`
			}
			if _, err := w.db.ExecContext(ctx, update, root, run.TaskID); err != nil {
				t.Fatal(err)
			}
			if got, err := teams.AssistanceRequest(ctx, run.WorkspaceID, session, request, "proposal-human"); !errors.Is(err, team.ErrInvalid) {
				t.Fatalf("coherent %s relocation exposed original question: %+v %v", scope, got, err)
			}
		})
	}
}

func TestTeamAssistanceReadScopeCaptureAndMissingHistory(t *testing.T) {
	w, ctx, run, teams, session, _ := prepareTeamProposalContinuation(t, "human")
	var root string
	if err := w.db.QueryRowContext(ctx, `SELECT task_id FROM team_sessions WHERE id=?`, session).Scan(&root); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('request-project','tenant','Original','active','{}','{}','proposal-human',1,1)`,
		`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('request-workspace','request-project','Original',1,1)`,
		`INSERT INTO artifact_sessions(id,workspace_id,project_id,title,format,status,created_by,created_at,updated_at) VALUES('request-session','tenant','request-project','Original','text','active','proposal-human',1,1)`,
	} {
		if _, err := w.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.db.ExecContext(ctx, `UPDATE tasks SET project_id='request-project',project_workspace_id='request-workspace',artifact_session_id='request-session' WHERE id IN (?,?)`, root, run.TaskID); err != nil {
		t.Fatal(err)
	}
	if result := applyWorkerWait(t, w, ctx, run, "human"); result.Status != "waiting_human" {
		t.Fatalf("scoped wait %+v", result)
	}
	var request, project, named, artifact string
	if err := w.db.QueryRowContext(ctx, `SELECT request_id,project_id,project_workspace_id,artifact_session_id FROM team_assistance_request_scopes`).Scan(&request, &project, &named, &artifact); err != nil {
		t.Fatal(err)
	}
	if project != "request-project" || named != "request-workspace" || artifact != "request-session" {
		t.Fatal("scope not captured at committed wait")
	}
	if got, err := teams.AssistanceRequest(ctx, run.WorkspaceID, session, request, "proposal-human"); err != nil || got.ProjectID != project || got.ProjectWorkspaceID != named || got.ArtifactSessionID != artifact {
		t.Fatalf("scoped read %+v %v", got, err)
	}
	for _, change := range []struct{ set, restore string }{
		{`UPDATE projects SET status='archived' WHERE id='request-project'`, `UPDATE projects SET status='active' WHERE id='request-project'`},
		{`UPDATE project_workspaces SET status='archived' WHERE id='request-workspace'`, `UPDATE project_workspaces SET status='active' WHERE id='request-workspace'`},
		{`UPDATE artifact_sessions SET status='archived' WHERE id='request-session'`, `UPDATE artifact_sessions SET status='active' WHERE id='request-session'`},
	} {
		if _, err := w.db.ExecContext(ctx, change.set); err != nil {
			t.Fatal(err)
		}
		if _, err := teams.AssistanceRequest(ctx, run.WorkspaceID, session, request, "proposal-human"); err != team.ErrInvalid {
			t.Fatalf("archived scope read %v", err)
		}
		if _, err := w.db.ExecContext(ctx, change.restore); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate a retained110request that predates scope capture. Reads must not
	// invent its past from mutable current Task data.
	if _, err := w.db.ExecContext(ctx, `DROP TRIGGER team_assistance_request_scope_retained; DELETE FROM team_assistance_request_scopes WHERE request_id=?`, request); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := teams.AssistanceRequest(ctx, run.WorkspaceID, session, request, "proposal-human"); err != team.ErrInvalid {
			t.Fatalf("unattributed historical scope exposed: %v", err)
		}
	}
	var count int
	if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_assistance_request_scopes WHERE request_id=?`, request).Scan(&count); err != nil || count != 0 {
		t.Fatalf("read fabricated scope: %d %v", count, err)
	}
}
