//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentworker"
	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type beforeAssistanceContinuation struct {
	*team.Service
	before func()
	after  func()
}

func (s *beforeAssistanceContinuation) ContinueAssistance(ctx context.Context, c team.ContinueAssistanceCommand) (team.AssistanceContinuation, error) {
	if s.before != nil {
		s.before()
	}
	out, err := s.Service.ContinueAssistance(ctx, c)
	if err == nil && s.after != nil {
		s.after()
	}
	return out, err
}

// This extends the documented synthetic retained-wait fixture with a real
// retained domain answer, production credentials and actual Task/Worker
// continuation collaborators. It does not run a model or validate installed AI.
func newAssistanceContinueHTTPFixture(t *testing.T, method string, noProject ...bool) (*teamAssistanceHTTPFixture, func(*http.Request), teamAssistanceContinueInput) {
	t.Helper()
	f := newTeamAssistanceHTTPFixture(t, false, noProject...)
	worker := agentworker.New(f.db.SQL(), f.db, clock.Real{}, "local-node", f.tasks, authority.NewService(f.db.SQL(), f.db, clock.Real{}), nil, nil, nil, nil, nil, nil, nil, nil, nil)
	for _, principal := range []string{agentworker.AuthorityPrincipal, agentworker.VerifierPrincipal} {
		if _, err := f.db.SQL().Exec(`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant',?,'active',1,1)`, principal); err != nil {
			t.Fatal(err)
		}
	}
	f.teams.SetAssistanceContinuationWorker(worker)
	input := answerHTTPInput(t, f)
	answer, err := f.teams.AnswerAssistance(context.Background(), team.AnswerAssistanceCommand{
		WorkspaceID: "tenant", SessionID: f.sessionID, RequestID: f.requestID,
		TaskID: input.TaskID, AttemptID: input.AttemptID, RunID: input.RunID, WaitStepID: input.WaitStepID,
		ExpectedTaskRevision: input.ExpectedTaskRevision, ExpectedWorkerRevision: input.ExpectedWorkerRevision,
		ActorPrincipalID: "human", IdempotencyKey: "continue-http-answer", Text: input.Text,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := answerHTTPAuth(t, f, method)
	if method == "bearer" {
		if _, err = f.db.SQL().Exec(`UPDATE api_credentials SET capability_scope_json='["team.read","team.write","task.read","task.write","project.read","project.write"]' WHERE id='answer-api'`); err != nil {
			t.Fatal(err)
		}
	}
	return f, auth, teamAssistanceContinueInput{AnswerID: answer.AnswerID, TaskID: input.TaskID, AttemptID: input.AttemptID, RunID: input.RunID, WaitStepID: input.WaitStepID, ExpectedTaskRevision: input.ExpectedTaskRevision, ExpectedWorkerRevision: input.ExpectedWorkerRevision}
}

func postAssistanceContinuation(t *testing.T, f *teamAssistanceHTTPFixture, raw string, auth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/team-sessions/"+f.sessionID+"/assistance/"+f.requestID+"/continue", strings.NewReader(raw))
	auth(r)
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	return w
}

func continuationHTTPState(t *testing.T, f *teamAssistanceHTTPFixture) string {
	t.Helper()
	return answerHTTPExecutionState(t, f)
}

func requireNoHTTPContinuation(t *testing.T, f *teamAssistanceHTTPFixture, before string) {
	t.Helper()
	if after := continuationHTTPState(t, f); after != before {
		t.Fatalf("denied continuation changed execution: before=%s after=%s", before, after)
	}
	var receipts, audits int
	if err := f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM team_assistance_continuations),(SELECT COUNT(*) FROM events WHERE event_type='team.assistance_continued')`).Scan(&receipts, &audits); err != nil || receipts != 0 || audits != 0 {
		t.Fatalf("denial retained receipt/audit=%d/%d err=%v", receipts, audits, err)
	}
}

func TestTeamAssistanceContinueHTTPOriginalOwnerAndDuplicate(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			f, auth, input := newAssistanceContinueHTTPFixture(t, method)
			raw, _ := json.Marshal(input)
			response := postAssistanceContinuation(t, f, string(raw), auth)
			if response.Code != http.StatusOK {
				t.Fatalf("Continue status=%d: %s", response.Code, response.Body.String())
			}
			var receipt team.AssistanceContinuation
			if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil || receipt.AnswerID != input.AnswerID || receipt.RequestID != f.requestID || receipt.ActorPrincipalID != "human" || receipt.ContinuationID == "" || receipt.ContinueStepID == "" {
				t.Fatalf("wrong continuation receipt: %+v err=%v", receipt, err)
			}
			if response.Header().Get("Cache-Control") != "no-store, private" || response.Header().Get("X-Content-Type-Options") != "nosniff" || strings.Contains(response.Body.String(), "PRIVATE_") {
				t.Fatal("unsafe continuation response")
			}
			var attempts, matched, receipts, audits int
			if err := f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM task_attempts WHERE task_id=?),
 (SELECT COUNT(*) FROM tasks t JOIN task_attempts a ON a.task_id=t.id JOIN agent_worker_runs r ON r.attempt_id=a.id WHERE t.id=? AND t.state='running' AND t.revision=? AND a.id=? AND a.status='running' AND r.id=? AND r.status='running' AND r.revision=?),
 (SELECT COUNT(*) FROM team_assistance_continuations),(SELECT COUNT(*) FROM events WHERE event_type='team.assistance_continued')`, input.TaskID, input.TaskID, input.ExpectedTaskRevision+1, input.AttemptID, input.RunID, input.ExpectedWorkerRevision+1).Scan(&attempts, &matched, &receipts, &audits); err != nil || attempts != 1 || matched != 1 || receipts != 1 || audits != 1 {
				t.Fatalf("original execution/receipt: %d/%d/%d/%d err=%v", attempts, matched, receipts, audits, err)
			}
			beforeDuplicate := continuationHTTPState(t, f)
			if duplicate := postAssistanceContinuation(t, f, string(raw), auth); duplicate.Code != http.StatusConflict {
				t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
			}
			if after := continuationHTTPState(t, f); after != beforeDuplicate {
				t.Fatal("duplicate approval reexecuted original owner")
			}
		})
	}
}

// The hook mutates persisted authority after the handler has authenticated and
// collected the session, immediately before the actual continuation transaction.
// A preflight permission decision must never authorize the subsequent write.
func TestTeamAssistanceContinueHTTPAuthorityAtWrite(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		changes := map[string]string{
			"human_disabled":     `UPDATE principals SET status='disabled' WHERE id='human'`,
			"membership_revoked": `UPDATE workspace_memberships SET status='revoked' WHERE principal_id='human'`,
			"tenant_suspended":   `UPDATE workspaces SET status='suspended' WHERE id='tenant'`,
		}
		if method == "bearer" {
			for name, query := range map[string]string{
				"revoked":               `UPDATE api_credentials SET revoked_at=1 WHERE id='answer-api'`,
				"expired":               `UPDATE api_credentials SET created_at=1,expires_at=2 WHERE id='answer-api'`,
				"task_write_revoked":    `UPDATE api_credentials SET capability_scope_json='["team.read","team.write","task.read","project.read","project.write"]' WHERE id='answer-api'`,
				"project_write_revoked": `UPDATE api_credentials SET capability_scope_json='["team.read","team.write","task.read","task.write","project.read"]' WHERE id='answer-api'`,
				"workspace_revoked":     `UPDATE api_credentials SET workspace_scope_json='[]' WHERE id='answer-api'`,
				"identity_replaced":     `UPDATE api_credentials SET principal_id='system:agent-worker' WHERE id='answer-api'`,
			} {
				changes[name] = query
			}
		} else {
			for name, query := range map[string]string{
				"revoked":           `UPDATE auth_sessions SET revoked_at=1 WHERE id='answer-session'`,
				"expired":           `UPDATE auth_sessions SET expires_at=1 WHERE id='answer-session'`,
				"idle_expired":      `UPDATE auth_sessions SET idle_expires_at=1 WHERE id='answer-session'`,
				"role_revoked":      `DELETE FROM principal_roles WHERE role_id='answer-admin'`,
				"csrf_replaced":     `UPDATE auth_sessions SET metadata_json='{}' WHERE id='answer-session'`,
				"identity_replaced": `UPDATE auth_sessions SET principal_id='system:agent-worker' WHERE id='answer-session'`,
			} {
				changes[name] = query
			}
		}
		for change, query := range changes {
			t.Run(method+"/"+change, func(t *testing.T) {
				f, auth, input := newAssistanceContinueHTTPFixture(t, method)
				raw, _ := json.Marshal(input)
				before := continuationHTTPState(t, f)
				f.server.SetTeam(&beforeAssistanceContinuation{Service: f.teams, before: func() {
					if _, err := f.db.SQL().Exec(query); err != nil {
						t.Fatal(err)
					}
				}})
				response := postAssistanceContinuation(t, f, string(raw), auth)
				if response.Code != http.StatusForbidden && response.Code != http.StatusUnauthorized && response.Code != http.StatusConflict {
					t.Fatalf("changed authority status=%d: %s", response.Code, response.Body.String())
				}
				if strings.Contains(response.Body.String(), "PRIVATE_") {
					t.Fatal("denial disclosed private text")
				}
				requireNoHTTPContinuation(t, f, before)
			})
		}
	}
}

func TestTeamAssistanceContinueHTTPStrictBoundedInput(t *testing.T) {
	f, auth, input := newAssistanceContinueHTTPFixture(t, "bearer")
	raw, _ := json.Marshal(input)
	before := continuationHTTPState(t, f)
	for _, body := range []string{
		string(raw[:len(raw)-1]) + `,"actor_principal_id":"forged"}`,
		string(raw) + ` {}`, string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}),
		strings.Repeat(" ", 8<<10) + string(raw), `null`, `{}`,
	} {
		response := postAssistanceContinuation(t, f, body, auth)
		if response.Code != http.StatusBadRequest && response.Code != http.StatusConflict {
			t.Fatalf("malformed continuation status=%d: %s", response.Code, response.Body.String())
		}
		requireNoHTTPContinuation(t, f, before)
	}
	for _, change := range []string{"answer", "task", "attempt", "run", "step", "task_revision", "worker_revision", "unsafe_revision"} {
		t.Run(change, func(t *testing.T) {
			changed := input
			switch change {
			case "answer":
				changed.AnswerID = "foreign"
			case "task":
				changed.TaskID = "foreign"
			case "attempt":
				changed.AttemptID = "foreign"
			case "run":
				changed.RunID = "foreign"
			case "step":
				changed.WaitStepID = "foreign"
			case "task_revision":
				changed.ExpectedTaskRevision++
			case "worker_revision":
				changed.ExpectedWorkerRevision++
			case "unsafe_revision":
				changed.ExpectedWorkerRevision = 1 << 53
			}
			body, _ := json.Marshal(changed)
			response := postAssistanceContinuation(t, f, string(body), auth)
			if response.Code != http.StatusConflict {
				t.Fatalf("stale continuation status=%d: %s", response.Code, response.Body.String())
			}
			requireNoHTTPContinuation(t, f, before)
		})
	}
}

func TestTeamAssistanceContinueHTTPConditionalProjectPermission(t *testing.T) {
	for _, noProject := range []bool{false, true} {
		f, auth, input := newAssistanceContinueHTTPFixture(t, "bearer", noProject)
		if _, err := f.db.SQL().Exec(`UPDATE api_credentials SET capability_scope_json='["team.read","task.read","team.write","task.write"]' WHERE id='answer-api'`); err != nil {
			t.Fatal(err)
		}
		before := continuationHTTPState(t, f)
		raw, _ := json.Marshal(input)
		response := postAssistanceContinuation(t, f, string(raw), auth)
		want := http.StatusForbidden
		if noProject {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("noProject=%v status=%d want=%d body=%s", noProject, response.Code, want, response.Body.String())
		}
		if !noProject {
			requireNoHTTPContinuation(t, f, before)
		}
	}
}

func TestTeamAssistanceContinueHTTPPreservesTeamHold(t *testing.T) {
	f, auth, input := newAssistanceContinueHTTPFixture(t, "bearer")
	root, err := f.tasks.Get(context.Background(), f.rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.teams.PauseExecution(context.Background(), team.ExecutionControlCommand{SessionID: f.sessionID, PlanID: f.planID, ActorPrincipalID: "human", ExpectedTaskRevision: root.Revision, Reason: "An answer must not release this hold"}); err != nil {
		t.Fatal(err)
	}
	before := continuationHTTPState(t, f)
	raw, _ := json.Marshal(input)
	response := postAssistanceContinuation(t, f, string(raw), auth)
	if response.Code != http.StatusConflict {
		t.Fatalf("held Continue status=%d: %s", response.Code, response.Body.String())
	}
	requireNoHTTPContinuation(t, f, before)
	var holds int
	if err = f.db.SQL().QueryRow(`SELECT COUNT(*) FROM team_execution_holds WHERE session_id=? AND released_at IS NULL`, f.sessionID).Scan(&holds); err != nil || holds != 1 {
		t.Fatalf("hold=%d err=%v", holds, err)
	}
}

func TestTeamAssistanceContinueHTTPWithholdsRevokedPostCommitResponse(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			f, auth, input := newAssistanceContinueHTTPFixture(t, method)
			raw, _ := json.Marshal(input)
			query := `UPDATE api_credentials SET revoked_at=1 WHERE id='answer-api'`
			if method == "session" {
				query = `UPDATE auth_sessions SET revoked_at=1 WHERE id='answer-session'`
			}
			f.server.SetTeam(&beforeAssistanceContinuation{Service: f.teams, after: func() {
				if _, err := f.db.SQL().Exec(query); err != nil {
					t.Fatal(err)
				}
			}})
			response := postAssistanceContinuation(t, f, string(raw), auth)
			if response.Code != http.StatusUnauthorized && response.Code != http.StatusForbidden {
				t.Fatalf("postcommit revoke status=%d: %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "continuation_id") || strings.Contains(response.Body.String(), "PRIVATE_") {
				t.Fatal("revoked response disclosed receipt or private text")
			}
			var receipts, running int
			if err := f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM team_assistance_continuations),(SELECT COUNT(*) FROM agent_worker_runs WHERE id=? AND status='running')`, input.RunID).Scan(&receipts, &running); err != nil || receipts != 1 || running != 1 {
				t.Fatalf("committed continuation=%d/%d err=%v", receipts, running, err)
			}
			before := continuationHTTPState(t, f)
			query = `UPDATE api_credentials SET revoked_at=NULL WHERE id='answer-api'`
			if method == "session" {
				query = `UPDATE auth_sessions SET revoked_at=NULL WHERE id='answer-session'`
			}
			if _, err := f.db.SQL().Exec(query); err != nil {
				t.Fatal(err)
			}
			f.server.SetTeam(f.teams)
			if retry := postAssistanceContinuation(t, f, string(raw), auth); retry.Code != http.StatusConflict {
				t.Fatalf("retry status=%d: %s", retry.Code, retry.Body.String())
			}
			if continuationHTTPState(t, f) != before {
				t.Fatal("retry reexecuted after withheld response")
			}
		})
	}
}

func TestTeamAssistanceContinueHTTPRequiresTransactionalVerifierAndCSRF(t *testing.T) {
	for _, change := range []string{"missing_verifier", "missing_csrf", "wrong_csrf"} {
		t.Run(change, func(t *testing.T) {
			method := "session"
			if change == "missing_verifier" {
				method = "bearer"
			}
			f, auth, input := newAssistanceContinueHTTPFixture(t, method)
			raw, _ := json.Marshal(input)
			before := continuationHTTPState(t, f)
			if change == "missing_verifier" {
				f.server.auth = f.auth
			} else {
				original := auth
				auth = func(r *http.Request) {
					original(r)
					r.Header.Del("X-OnePane-CSRF")
					if change == "wrong_csrf" {
						r.Header.Set("X-OnePane-CSRF", "wrong")
					}
				}
			}
			response := postAssistanceContinuation(t, f, string(raw), auth)
			want := http.StatusForbidden
			if change == "missing_verifier" {
				want = http.StatusServiceUnavailable
			}
			if response.Code != want {
				t.Fatalf("status=%d want=%d: %s", response.Code, want, response.Body.String())
			}
			requireNoHTTPContinuation(t, f, before)
		})
	}
}
