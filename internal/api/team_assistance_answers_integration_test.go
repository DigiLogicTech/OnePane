//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"github.com/DigiLogicTech/OnePane/internal/webauth"
)

type beforeAssistanceAnswer struct {
	*team.Service
	before func()
}

func (s *beforeAssistanceAnswer) AnswerAssistance(ctx context.Context, command team.AnswerAssistanceCommand) (team.AssistanceAnswer, error) {
	if s.before != nil {
		s.before()
	}
	return s.Service.AnswerAssistance(ctx, command)
}

func answerHTTPInput(t *testing.T, f *teamAssistanceHTTPFixture) teamAssistanceAnswerInput {
	t.Helper()
	request, err := f.teams.AssistanceRequest(context.Background(), "tenant", f.sessionID, f.requestID, "human")
	if err != nil {
		t.Fatal(err)
	}
	return teamAssistanceAnswerInput{TaskID: request.TaskID, AttemptID: request.AttemptID, RunID: request.RunID, WaitStepID: request.WaitStepID, ExpectedTaskRevision: request.TaskRevision, ExpectedWorkerRevision: request.WorkerRevision, IdempotencyKey: "human-reply-1", Text: "PRIVATE_HUMAN_REPLY <script>authored text</script>"}
}

// Real migrated credentials and production authorizers; waiting Worker rows are
// synthetic as documented by the shared fixture. No model/tool/Continue runs.
func answerHTTPAuth(t *testing.T, f *teamAssistanceHTTPFixture, method string) func(*http.Request) {
	t.Helper()
	now := clock.Real{}.UnixMilli()
	bearer := NewBearerAuthorizer(f.db.SQL(), clock.Real{})
	if method == "bearer" {
		_, err := f.db.SQL().Exec(`INSERT INTO api_credentials(id,principal_id,name,credential_hash,workspace_scope_json,capability_scope_json,created_at) VALUES('answer-api','human','Disposable test',?,'["tenant"]','["team.read","team.write","task.read","project.read"]',?)`, transactionAuthHash("disposable-answer-token"), now)
		if err != nil {
			t.Fatal(err)
		}
		f.server = NewServer(f.projects, fakeEvents{}, bearer)
		f.server.SetTeam(f.teams)
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer disposable-answer-token") }
	}
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO roles(id,role_class,name,definition_json,revision,created_at,updated_at) VALUES('answer-admin','human','Admin','{}',1,?,?)`, []any{now, now}},
		{`INSERT INTO principal_roles(principal_id,role_id,workspace_id,created_at) VALUES('human','answer-admin','tenant',?)`, []any{now}},
		{`INSERT INTO auth_sessions(id,principal_id,token_hash,auth_method,auth_strength,created_at,last_seen_at,idle_expires_at,expires_at,metadata_json) VALUES('answer-session','human',?,'password','single_factor',?,?,?,?,?)`, []any{transactionAuthHash("disposable-session-token"), now, now, now + 600000, now + 3600000, `{"csrf_hash":"` + transactionAuthHash("disposable-csrf") + `"}`}},
	} {
		if _, err := f.db.SQL().Exec(statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	hybrid := NewHybridAuthorizer(bearer, webauth.NewService(f.db.SQL(), f.db, clock.Real{}))
	f.server = NewServer(f.projects, fakeEvents{}, hybrid)
	f.server.SetTeam(f.teams)
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: WebSessionCookie, Value: "disposable-session-token"})
		r.Header.Set("X-OnePane-CSRF", "disposable-csrf")
	}
}

func answerHTTPExecutionState(t *testing.T, f *teamAssistanceHTTPFixture) string {
	t.Helper()
	var state string
	err := f.db.SQL().QueryRow(`SELECT json_array(t.state,t.revision,a.id,a.status,r.status,r.revision,r.continuation_json,
 (SELECT COUNT(*) FROM agent_worker_steps),(SELECT COUNT(*) FROM team_execution_holds),(SELECT COUNT(*) FROM team_execution_held_tasks),
 (SELECT COUNT(*) FROM team_messages),(SELECT COUNT(*) FROM team_assistance_requests),(SELECT COUNT(*) FROM budget_reservations))
 FROM tasks t JOIN task_attempts a ON a.task_id=t.id JOIN agent_worker_runs r ON r.attempt_id=a.id WHERE t.id=?`, f.taskID).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func postAssistanceAnswer(t *testing.T, f *teamAssistanceHTTPFixture, raw string, auth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/team-sessions/"+f.sessionID+"/assistance/"+f.requestID+"/answers", strings.NewReader(raw))
	auth(r)
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	return w
}

func TestTeamAssistanceAnswerHTTPPreservesWaitingOwner(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		for _, held := range []bool{false, true} {
			t.Run(method+map[bool]string{false: "/waiting", true: "/held"}[held], func(t *testing.T) {
				f := newTeamAssistanceHTTPFixture(t, false)
				auth := answerHTTPAuth(t, f, method)
				if held {
					root, err := f.tasks.Get(context.Background(), f.rootID)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = f.teams.PauseExecution(context.Background(), team.ExecutionControlCommand{SessionID: f.sessionID, PlanID: f.planID, ActorPrincipalID: "human", ExpectedTaskRevision: root.Revision, Reason: "Reply while held"}); err != nil {
						t.Fatal(err)
					}
				}
				input := answerHTTPInput(t, f)
				raw, _ := json.Marshal(input)
				before := answerHTTPExecutionState(t, f)
				first := postAssistanceAnswer(t, f, string(raw), auth)
				if first.Code != http.StatusOK {
					t.Fatalf("answer status %d: %s", first.Code, first.Body.String())
				}
				var got team.AssistanceAnswer
				if err := json.Unmarshal(first.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.RequestID != f.requestID || got.Text != input.Text || got.AuthorPrincipalID != "human" || got.AnswerID == "" {
					t.Fatal("wrong persisted answer")
				}
				if first.Header().Get("Cache-Control") != "no-store, private" || first.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("missing private headers")
				}
				second := postAssistanceAnswer(t, f, string(raw), auth)
				if second.Code != http.StatusOK || second.Body.String() != first.Body.String() {
					t.Fatalf("idempotent retry status=%d", second.Code)
				}
				input.Text = "Conflicting later text"
				raw, _ = json.Marshal(input)
				conflict := postAssistanceAnswer(t, f, string(raw), auth)
				if conflict.Code != http.StatusConflict {
					t.Fatalf("conflict status=%d", conflict.Code)
				}
				if after := answerHTTPExecutionState(t, f); after != before {
					t.Fatal("answer changed waiting execution or hold")
				}
				var answers, audits int
				if err := f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM team_assistance_answers),(SELECT COUNT(*) FROM events WHERE event_type='team.assistance_answered')`).Scan(&answers, &audits); err != nil || answers != 1 || audits != 1 {
					t.Fatalf("answer/audit %d/%d %v", answers, audits, err)
				}
			})
		}
	}
}

func TestTeamAssistanceAnswerHTTPRejectsChangedAuthority(t *testing.T) {
	for _, scenario := range []string{"credential_revoked", "credential_scope", "credential_workspace", "principal_type", "membership_revoked", "tenant_suspended", "project_archived", "named_archived", "worker_revision", "task_revision", "answer_rejected", "audit_rejected", "missing_verifier"} {
		t.Run(scenario, func(t *testing.T) {
			f := newTeamAssistanceHTTPFixture(t, false)
			auth := answerHTTPAuth(t, f, "bearer")
			input := answerHTTPInput(t, f)
			raw, _ := json.Marshal(input)
			before := answerHTTPExecutionState(t, f)
			queries := map[string]string{
				"credential_revoked":   `UPDATE api_credentials SET revoked_at=1 WHERE id='answer-api'`,
				"credential_scope":     `UPDATE api_credentials SET capability_scope_json='["team.read","task.read","project.read"]' WHERE id='answer-api'`,
				"credential_workspace": `UPDATE api_credentials SET workspace_scope_json='[]' WHERE id='answer-api'`,
				"principal_type":       `UPDATE principals SET principal_type='agent' WHERE id='human'`,
				"membership_revoked":   `UPDATE workspace_memberships SET status='revoked' WHERE principal_id='human'`,
				"tenant_suspended":     `UPDATE workspaces SET status='suspended' WHERE id='tenant'`,
				"project_archived":     `UPDATE projects SET status='archived' WHERE id='` + f.projectID + `'`,
				"named_archived":       `UPDATE project_workspaces SET status='archived' WHERE id='` + f.workspaceID + `'`,
				"worker_revision":      `UPDATE agent_worker_runs SET revision=revision+1 WHERE id='http-help-run'`,
				"task_revision":        `UPDATE tasks SET revision=revision+1 WHERE id='` + f.taskID + `'`,
				"answer_rejected":      `CREATE TRIGGER reject_answer BEFORE INSERT ON team_assistance_answers BEGIN SELECT RAISE(ABORT,'reject'); END`,
				"audit_rejected":       `CREATE TRIGGER reject_answer BEFORE INSERT ON events WHEN NEW.event_type='team.assistance_answered' BEGIN SELECT RAISE(ABORT,'reject'); END`,
			}
			if scenario == "missing_verifier" {
				f.server.auth = f.auth
			} else {
				f.server.SetTeam(&beforeAssistanceAnswer{Service: f.teams, before: func() {
					if _, err := f.db.SQL().Exec(queries[scenario]); err != nil {
						t.Fatal(err)
					}
					before = answerHTTPExecutionState(t, f)
				}})
			}
			response := postAssistanceAnswer(t, f, string(raw), auth)
			if response.Code == http.StatusOK || response.Code == http.StatusCreated {
				t.Fatalf("changed authority accepted %s", scenario)
			}
			if strings.Contains(response.Body.String(), "PRIVATE_HUMAN_REPLY") || strings.Contains(response.Body.String(), "PRIVATE_ASSISTANCE_QUESTION_CANARY") {
				t.Fatal("denial disclosed private text")
			}
			var answers, audits int
			if err := f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM team_assistance_answers),(SELECT COUNT(*) FROM events WHERE event_type='team.assistance_answered')`).Scan(&answers, &audits); err != nil || answers != 0 || audits != 0 {
				t.Fatalf("partial denied answer %d/%d %v", answers, audits, err)
			}
			if after := answerHTTPExecutionState(t, f); after != before {
				t.Fatal("denied answer changed execution")
			}
		})
	}
}

func TestTeamAssistanceAnswerHTTPStrictBoundedInput(t *testing.T) {
	f := newTeamAssistanceHTTPFixture(t, false)
	auth := answerHTTPAuth(t, f, "bearer")
	input := answerHTTPInput(t, f)
	raw, _ := json.Marshal(input)
	for _, body := range []string{
		string(raw[:len(raw)-1]) + `,"author_principal_id":"forged"}`,
		string(raw) + ` {}`, string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}), strings.Repeat(" ", 32<<10) + string(raw),
	} {
		response := postAssistanceAnswer(t, f, body, auth)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("malformed request status=%d", response.Code)
		}
	}
	var count int
	if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM team_assistance_answers`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("malformed answer persisted %d %v", count, err)
	}
}

func TestTeamAssistanceAnswerHTTPSessionAuthorityAtWrite(t *testing.T) {
	for _, change := range []string{"revoked", "expired", "idle_expired", "role_revoked", "csrf_replaced", "identity_replaced"} {
		t.Run(change, func(t *testing.T) {
			f := newTeamAssistanceHTTPFixture(t, false)
			auth := answerHTTPAuth(t, f, "session")
			raw, _ := json.Marshal(answerHTTPInput(t, f))
			before := answerHTTPExecutionState(t, f)
			f.server.SetTeam(&beforeAssistanceAnswer{Service: f.teams, before: func() {
				query := map[string]string{
					"revoked":           `UPDATE auth_sessions SET revoked_at=1 WHERE id='answer-session'`,
					"expired":           `UPDATE auth_sessions SET expires_at=1 WHERE id='answer-session'`,
					"idle_expired":      `UPDATE auth_sessions SET idle_expires_at=1 WHERE id='answer-session'`,
					"role_revoked":      `DELETE FROM principal_roles WHERE role_id='answer-admin'`,
					"csrf_replaced":     `UPDATE auth_sessions SET metadata_json='{}' WHERE id='answer-session'`,
					"identity_replaced": `UPDATE auth_sessions SET principal_id='system:agent-worker' WHERE id='answer-session'`,
				}[change]
				if _, err := f.db.SQL().Exec(query); err != nil {
					t.Fatal(err)
				}
			}})
			response := postAssistanceAnswer(t, f, string(raw), auth)
			if response.Code != http.StatusForbidden && response.Code != http.StatusUnauthorized {
				t.Fatalf("session change %s status=%d", change, response.Code)
			}
			var answers, audits int
			if err := f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM team_assistance_answers),(SELECT COUNT(*) FROM events WHERE event_type='team.assistance_answered')`).Scan(&answers, &audits); err != nil || answers != 0 || audits != 0 {
				t.Fatalf("denied session persisted %d/%d %v", answers, audits, err)
			}
			if after := answerHTTPExecutionState(t, f); after != before {
				t.Fatal("session denial changed execution")
			}
		})
	}
}

func TestTeamAssistanceAnswerHTTPConditionalProjectPermission(t *testing.T) {
	for _, noProject := range []bool{false, true} {
		f := newTeamAssistanceHTTPFixture(t, false, noProject)
		auth := answerHTTPAuth(t, f, "bearer")
		if _, err := f.db.SQL().Exec(`UPDATE api_credentials SET capability_scope_json='["team.read","task.read","team.write"]' WHERE id='answer-api'`); err != nil {
			t.Fatal(err)
		}
		input := answerHTTPInput(t, f)
		// The request limit accommodates a complete valid 4KiB answer even when
		// JSON encodes each authored byte as six bytes of escape syntax.
		input.Text = strings.Repeat("\x00", 4096)
		raw, _ := json.Marshal(input)
		response := postAssistanceAnswer(t, f, string(raw), auth)
		want := http.StatusForbidden
		if noProject {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("conditional Project permission noProject=%v status=%d want=%d", noProject, response.Code, want)
		}
	}
}
