//go:build integration

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentworker"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type assistanceHTTPAuth struct {
	inspectedTaskAuth
	db             *sql.DB
	denyCapability string
}

func (a *assistanceHTTPAuth) AuthorizeWorkspace(ctx context.Context, identity Identity, workspace, capability string) error {
	if err := a.inspectedTaskAuth.AuthorizeWorkspace(ctx, identity, workspace, capability); err != nil {
		return err
	}
	if capability == a.denyCapability {
		return ErrForbidden
	}
	var authorized bool
	if err := a.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM principals p
	 JOIN workspace_memberships m ON m.principal_id=p.id JOIN workspaces w ON w.id=m.workspace_id
	 WHERE p.id=? AND p.principal_type=? AND p.status='active' AND w.id=? AND w.status='active' AND m.status='active')`, identity.PrincipalID, identity.PrincipalType, workspace).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return ErrForbidden
	}
	return nil
}

type teamAssistanceHTTPFixture struct {
	*publishedLibraryFixture
	teams                                               *team.Service
	tasks                                               *task.Service
	auth                                                *assistanceHTTPAuth
	sessionID, rootID, taskID, planID, requestID, runID string
}

// Actual migrated schema and accepted Team/Task services, with explicitly
// synthetic recorded wait/Worker/journal/notice/request rows. The Worker human
// handler's materialization is tested separately. This fixture runs no model,
// tool, OCI process or installed application and cannot prove those acceptances.
func newTeamAssistanceHTTPFixture(t *testing.T, omitScope bool, noProject ...bool) *teamAssistanceHTTPFixture {
	t.Helper()
	f := publishedLibraryHTTPFixture(t, nil)
	ctx := context.Background()
	tasks := task.NewService(f.db.SQL(), f.db, clock.Real{})
	worker := agentworker.New(f.db.SQL(), f.db, clock.Real{}, "local-node", tasks, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if err := worker.EnsureSystemPrincipals(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.SQL().Exec(`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant',?,'active',1,1)`, agentworker.WorkerPrincipal); err != nil {
		t.Fatal(err)
	}
	command := task.CreateCommand{WorkspaceID: "tenant", ProjectID: &f.projectID, ProjectWorkspaceID: &f.workspaceID, Objective: "Review waiting Team contribution"}
	if len(noProject) > 0 && noProject[0] {
		command.ProjectID, command.ProjectWorkspaceID = nil, nil
	}
	root, err := tasks.Create(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	teams := team.NewService(f.db.SQL(), f.db, clock.Real{}, tasks)
	group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: "tenant", Name: "Assistance readers", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Builder <untrusted>", RoleName: "Build", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	ss, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: root.ID, TeamID: group.ID, CreatedBy: "human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"assignments": []team.AssignmentSpec{{Key: "build", MemberID: member.ID, Objective: "Wait for a specific human answer"}}})
	plan, err := teams.ProposePlan(ctx, team.ProposePlanCommand{SessionID: ss.ID, ProposedByPrincipalID: "human", Plan: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.AcceptPlan(ctx, team.AcceptPlanCommand{SessionID: ss.ID, PlanID: plan.ID, AcceptedByPrincipalID: "human"}); err != nil {
		t.Fatal(err)
	}
	tasks.SetAdmissionGuard(teams)
	var childID string
	if err = f.db.SQL().QueryRow(`SELECT task_id FROM team_execution_assignments WHERE session_id=? AND assignment_key='build'`, ss.ID).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	child, err := tasks.Get(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	child, err = tasks.MarkReady(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentworker.WorkerPrincipal
	child, attempt, err := tasks.Start(ctx, task.StartCommand{TaskID: child.ID, ExpectedRevision: child.Revision, WorkerPrincipalID: &principal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tasks.WaitApproval(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision}); err != nil {
		t.Fatal(err)
	}
	const question = "PRIVATE_ASSISTANCE_QUESTION_CANARY <script>untrusted</script>"
	continuation, _ := json.Marshal(map[string]string{"human_input_required": question})
	notice, _ := json.Marshal(map[string]string{"text": "Assignment build needs human input.", "assistance_request_id": "http-help-request", "task_id": child.ID, "target_member_id": member.ID})
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`UPDATE task_attempts SET metadata_json='{"agent_worker":"v1"}' WHERE id=?`, []any{attempt.ID}},
		{`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) VALUES('http-help-run','tenant',?,? ,?,'waiting','Build','inference.general','L1',8,2,2,'{}',?,1,1)`, []any{child.ID, attempt.ID, principal, string(continuation)}},
		{`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) VALUES('http-help-step','http-help-run',1,'human','waiting','human','{}',1,1)`, nil},
		{`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) VALUES('http-help-notice','tenant',?, ?,?,'system',?,0,1)`, []any{ss.ID, member.ID, principal, string(notice)}},
		{`INSERT INTO team_assistance_requests(id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at) VALUES('http-help-request','tenant',?,?,'build',?,?,?,'http-help-run','http-help-step','http-help-notice',?,?,1)`, []any{ss.ID, plan.ID, member.ID, child.ID, attempt.ID, principal, question}},
	} {
		if _, err := f.db.SQL().Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if !omitScope {
		if _, err := f.db.SQL().Exec(`INSERT INTO team_assistance_request_scopes(request_id,project_id,project_workspace_id,artifact_session_id) VALUES('http-help-request',?,?,NULL)`, command.ProjectID, command.ProjectWorkspaceID); err != nil {
			t.Fatal(err)
		}
	}
	auth := &assistanceHTTPAuth{inspectedTaskAuth: inspectedTaskAuth{identity: Identity{PrincipalID: "human", PrincipalType: "human", CredentialID: "original"}}, db: f.db.SQL()}
	f.server = NewServer(f.projects, fakeEvents{}, auth)
	f.server.SetTeam(teams)
	return &teamAssistanceHTTPFixture{publishedLibraryFixture: f, teams: teams, tasks: tasks, auth: auth, sessionID: ss.ID, rootID: root.ID, taskID: child.ID, planID: plan.ID, requestID: "http-help-request", runID: "http-help-run"}
}

type observedAssistanceReader struct {
	*team.Service
	afterRead func(int)
	reads     int
}

func (s *observedAssistanceReader) AssistanceRequest(ctx context.Context, workspace, session, request, principal string) (team.AssistanceRequest, error) {
	out, err := s.Service.AssistanceRequest(ctx, workspace, session, request, principal)
	s.reads++
	if err == nil && s.afterRead != nil {
		s.afterRead(s.reads)
	}
	return out, err
}

func assistanceHTTPReadState(t *testing.T, f *teamAssistanceHTTPFixture) string {
	t.Helper()
	var state string
	if err := f.db.SQL().QueryRow(`SELECT json_array((SELECT COUNT(*) FROM events),(SELECT COUNT(*) FROM task_attempts),(SELECT COUNT(*) FROM inference_requests),(SELECT COUNT(*) FROM tool_invocations),(SELECT COUNT(*) FROM agent_runtime_invocations),(SELECT COUNT(*) FROM agent_worker_steps),(SELECT COUNT(*) FROM team_messages),(SELECT COUNT(*) FROM team_assistance_requests),(SELECT state FROM tasks WHERE id=?),(SELECT status FROM agent_worker_runs WHERE id=?))`, f.taskID, f.runID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestTeamAssistanceReadCurrentReadFences(t *testing.T) {
	for _, scenario := range []string{"allowed", "paused", "credential_revoked", "principal_changed", "credential_changed", "principal_type_changed", "team_read_revoked", "task_read_revoked", "project_read_revoked", "reader_membership_revoked", "reader_principal_revoked", "project_archived", "project_archived_after_final_collection", "named_workspace_archived", "root_criteria_changed", "worker_changed", "foreign_request", "foreign_session", "final_credential_revoked", "final_principal_changed", "final_credential_changed", "final_principal_type_changed", "final_team_read_revoked", "final_task_read_revoked", "final_project_read_revoked", "final_reader_membership_revoked", "final_reader_principal_revoked", "final_named_workspace_archived", "final_named_workspace_foreign_project", "final_project_foreign_tenant", "missing_request_scope", "coherent_root_child_move"} {
		t.Run(scenario, func(t *testing.T) {
			f := newTeamAssistanceHTTPFixture(t, scenario == "missing_request_scope")
			var foreignProjectID string
			if scenario == "final_named_workspace_foreign_project" || scenario == "coherent_root_child_move" {
				foreign, err := f.projects.CreateProject(context.Background(), projectworkspace.CreateProjectCommand{WorkspaceID: "tenant", Name: "Independent Project", CreatedBy: "human"})
				if err != nil {
					t.Fatal(err)
				}
				foreignProjectID = foreign.ID
			}
			if scenario == "coherent_root_child_move" {
				if _, err := f.db.SQL().Exec(`UPDATE tasks SET project_id=?,project_workspace_id=NULL WHERE id IN (?,?)`, foreignProjectID, f.rootID, f.taskID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "final_project_foreign_tenant" {
				if _, err := f.db.SQL().Exec(`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('other-tenant','Other tenant','active',1,1,1)`); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "paused" {
				root, err := f.tasks.Get(context.Background(), f.rootID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.teams.PauseExecution(context.Background(), team.ExecutionControlCommand{SessionID: f.sessionID, PlanID: f.planID, ActorPrincipalID: "human", ExpectedTaskRevision: root.Revision, Reason: "Read while held"}); err != nil {
					t.Fatal(err)
				}
			}
			before := assistanceHTTPReadState(t, f)
			reader := &observedAssistanceReader{Service: f.teams}
			reader.afterRead = func(n int) {
				if scenario == "project_archived_after_final_collection" && n == 2 {
					if _, err := f.db.SQL().Exec(`UPDATE projects SET status='archived' WHERE id=?`, f.projectID); err != nil {
						t.Fatal(err)
					}
					return
				}
				mutationStage := 1
				if strings.HasPrefix(scenario, "final_") {
					mutationStage = 2
				}
				if n != mutationStage {
					return
				}
				query := ""
				var args []any
				switch strings.TrimPrefix(scenario, "final_") {
				case "credential_revoked":
					f.auth.err = ErrUnauthenticated
				case "principal_changed":
					f.auth.identity.PrincipalID = "replacement"
				case "credential_changed":
					f.auth.identity.CredentialID = "replacement"
				case "principal_type_changed":
					f.auth.identity.PrincipalType = "agent"
				case "team_read_revoked":
					f.auth.denyCapability = "team.read"
				case "task_read_revoked":
					f.auth.denyCapability = "task.read"
				case "project_read_revoked":
					f.auth.denyCapability = "project.read"
				case "reader_membership_revoked":
					query = `UPDATE workspace_memberships SET status='revoked' WHERE workspace_id='tenant' AND principal_id='human'`
				case "reader_principal_revoked":
					query = `UPDATE principals SET status='revoked' WHERE id='human'`
				case "project_archived":
					query = `UPDATE projects SET status='archived' WHERE id=?`
					args = []any{f.projectID}
				case "named_workspace_archived":
					query = `UPDATE project_workspaces SET status='archived' WHERE id=?`
					args = []any{f.workspaceID}
				case "named_workspace_foreign_project":
					query = `UPDATE project_workspaces SET project_id=? WHERE id=?`
					args = []any{foreignProjectID, f.workspaceID}
				case "project_foreign_tenant":
					query = `UPDATE projects SET workspace_id='other-tenant' WHERE id=?`
					args = []any{f.projectID}
				case "root_criteria_changed":
					query = `UPDATE tasks SET completion_json='{"changed":true}' WHERE id=?`
					args = []any{f.rootID}
				case "worker_changed":
					query = `UPDATE agent_worker_runs SET revision=revision+1 WHERE id=?`
					args = []any{f.runID}
				}
				if query != "" {
					if _, err := f.db.SQL().Exec(query, args...); err != nil {
						t.Fatal(err)
					}
				}
			}
			f.server.SetTeam(reader)
			path := "/v1/team-sessions/" + f.sessionID + "/assistance/" + f.requestID
			if scenario == "foreign_request" {
				path = "/v1/team-sessions/" + f.sessionID + "/assistance/foreign-private-request"
			}
			if scenario == "foreign_session" {
				path = "/v1/team-sessions/foreign-private-session/assistance/" + f.requestID
			}
			w := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			allowed := scenario == "allowed" || scenario == "paused"
			if allowed {
				if w.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				}
				var out team.AssistanceRequest
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if out.RequestID != f.requestID || out.TaskID != f.taskID || out.RunID != f.runID || out.ProjectID != f.projectID || !strings.Contains(out.Question, "PRIVATE_ASSISTANCE_QUESTION_CANARY") {
					t.Fatalf("wrong exact waiting question: %+v", out)
				}
				if w.Header().Get("Cache-Control") != "no-store, private" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("unsafe headers %v", w.Header())
				}
			} else {
				want := http.StatusForbidden
				switch scenario {
				case "credential_revoked", "final_credential_revoked":
					want = http.StatusUnauthorized
				case "project_archived", "named_workspace_archived", "root_criteria_changed", "worker_changed", "foreign_request", "missing_request_scope", "coherent_root_child_move":
					want = http.StatusConflict
				case "foreign_session":
					want = http.StatusNotFound
				}
				if w.Code != want {
					t.Fatalf("denied read status=%d want=%d body=%s", w.Code, want, w.Body.String())
				}
				for _, private := range []string{"PRIVATE_ASSISTANCE_QUESTION_CANARY", f.taskID, f.runID, "http-help-step", "Builder <untrusted>"} {
					if strings.Contains(w.Body.String(), private) {
						t.Fatalf("denied read disclosed %q: %s", private, w.Body.String())
					}
				}
			}
			if after := assistanceHTTPReadState(t, f); after != before {
				t.Fatalf("read changed execution/history before=%s after=%s", before, after)
			}
		})
	}
}

func TestTeamAssistanceReadMetadataNumericBounds(t *testing.T) {
	base := team.AssistanceRequest{RequestID: "request", WorkspaceID: "tenant", SessionID: "session", PlanID: "plan", AssignmentKey: "build", MemberID: "builder", MemberName: "Builder", ParentTaskID: "root", TaskID: "task", AttemptID: "attempt", RunID: "run", WaitStepID: "step", NoticeMessageID: "notice", AuthorPrincipalID: agentworker.WorkerPrincipal, Question: "What should I verify?", TaskState: "waiting_approval", TaskRevision: 1, WorkerRevision: 1, CreatedAt: 1}
	if !validTeamAssistanceMetadata(base) {
		t.Fatal("healthy bounded metadata denied")
	}
	for _, field := range []string{"task_revision", "worker_revision", "created_at"} {
		for _, value := range []int64{-1, 0, 1 << 53, 1<<63 - 1} {
			t.Run(field+"/"+strconv.FormatInt(value, 10), func(t *testing.T) {
				request := base
				switch field {
				case "task_revision":
					request.TaskRevision = value
				case "worker_revision":
					request.WorkerRevision = value
				case "created_at":
					request.CreatedAt = value
				}
				if validTeamAssistanceMetadata(request) {
					t.Fatalf("unsafe %s=%d accepted", field, value)
				}
			})
		}
	}
	base.TaskRevision = 1<<53 - 1
	base.WorkerRevision = 1<<53 - 1
	base.CreatedAt = 1<<53 - 1
	if !validTeamAssistanceMetadata(base) {
		t.Fatal("largest exact JSON integer denied")
	}
}

func TestTeamAssistanceReadWithoutProjectPermission(t *testing.T) {
	f := newTeamAssistanceHTTPFixture(t, false, true)
	f.server.projects = nil
	f.auth.denyCapability = "project.read"
	before := assistanceHTTPReadState(t, f)
	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/team-sessions/"+f.sessionID+"/assistance/"+f.requestID, nil))
		return w
	}
	w := get()
	if w.Code != http.StatusOK {
		t.Fatalf("no-Project request required unrelated permission: %d %s", w.Code, w.Body.String())
	}
	var out team.AssistanceRequest
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ProjectID != "" || out.ProjectWorkspaceID != "" || out.RequestID != f.requestID {
		t.Fatalf("wrong no-Project scope: %+v", out)
	}
	for _, capability := range []string{"team.read", "task.read"} {
		f.auth.denyCapability = capability
		w = get()
		if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "PRIVATE_ASSISTANCE_QUESTION_CANARY") {
			t.Fatalf("missing %s disclosed request: %d %s", capability, w.Code, w.Body.String())
		}
	}
	if after := assistanceHTTPReadState(t, f); after != before {
		t.Fatal("no-Project reads changed execution")
	}
}
