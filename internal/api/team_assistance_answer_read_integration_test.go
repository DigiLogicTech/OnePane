//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

type observedAssistanceAnswerReader struct {
	*team.Service
	afterRead func(int)
	reads     int
}

func (s *observedAssistanceAnswerReader) AssistanceAnswer(ctx context.Context, workspace, session, request, principal string) (team.AssistanceAnswerObservation, error) {
	out, err := s.Service.AssistanceAnswer(ctx, workspace, session, request, principal)
	s.reads++
	if err == nil && s.afterRead != nil {
		s.afterRead(s.reads)
	}
	return out, err
}

func TestTeamAssistanceAnswerReadHTTPCurrentAuthority(t *testing.T) {
	for _, scenario := range []string{"allowed", "held", "no_project", "escaped", "missing", "team_read", "task_read", "project_read", "final_credential", "final_principal", "final_membership", "final_tenant", "final_project", "final_named", "changed_owner", "malformed_answer"} {
		t.Run(scenario, func(t *testing.T) {
			f := newTeamAssistanceHTTPFixture(t, false, scenario == "no_project")
			auth := answerHTTPAuth(t, f, "bearer")
			if scenario == "held" {
				root, err := f.tasks.Get(context.Background(), f.rootID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.teams.PauseExecution(context.Background(), team.ExecutionControlCommand{SessionID: f.sessionID, PlanID: f.planID, ActorPrincipalID: "human", ExpectedTaskRevision: root.Revision, Reason: "Read retained answer while held"}); err != nil {
					t.Fatal(err)
				}
			}
			input := answerHTTPInput(t, f)
			if scenario == "escaped" {
				input.Text = strings.Repeat("\x00", 4096)
			}
			if scenario != "missing" {
				raw, _ := json.Marshal(input)
				if response := postAssistanceAnswer(t, f, string(raw), auth); response.Code != http.StatusOK {
					t.Fatalf("prepare answer=%d", response.Code)
				}
			}
			query := ""
			switch scenario {
			case "team_read":
				query = `UPDATE api_credentials SET capability_scope_json='["task.read","project.read"]' WHERE id='answer-api'`
			case "no_project":
				query = `UPDATE api_credentials SET capability_scope_json='["team.read","task.read"]' WHERE id='answer-api'`
			case "task_read":
				query = `UPDATE api_credentials SET capability_scope_json='["team.read","project.read"]' WHERE id='answer-api'`
			case "project_read":
				query = `UPDATE api_credentials SET capability_scope_json='["team.read","task.read"]' WHERE id='answer-api'`
			case "changed_owner":
				query = `UPDATE agent_worker_runs SET status='running' WHERE id='http-help-run'`
			case "malformed_answer":
				query = `DROP TRIGGER team_assistance_answer_immutable; UPDATE team_assistance_answers SET id='';`
			}
			if query != "" {
				if _, err := f.db.SQL().Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			before := answerHTTPExecutionState(t, f)
			var beforeEvents int
			if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&beforeEvents); err != nil {
				t.Fatal(err)
			}
			reader := &observedAssistanceAnswerReader{Service: f.teams}
			reader.afterRead = func(read int) {
				if read != 2 {
					return
				}
				mutation := map[string]string{
					"final_credential": `UPDATE api_credentials SET revoked_at=1 WHERE id='answer-api'`,
					"final_principal":  `UPDATE principals SET status='revoked' WHERE id='human'`,
					"final_membership": `UPDATE workspace_memberships SET status='revoked' WHERE principal_id='human'`,
					"final_tenant":     `UPDATE workspaces SET status='suspended' WHERE id='tenant'`,
					"final_project":    `UPDATE projects SET status='archived' WHERE id='` + f.projectID + `'`,
					"final_named":      `UPDATE project_workspaces SET status='archived' WHERE id='` + f.workspaceID + `'`,
				}[scenario]
				if mutation != "" {
					if _, err := f.db.SQL().Exec(mutation); err != nil {
						t.Fatal(err)
					}
				}
			}
			f.server.SetTeam(reader)
			r := httptest.NewRequest(http.MethodGet, "/v1/team-sessions/"+f.sessionID+"/assistance/"+f.requestID+"/answer", nil)
			auth(r)
			response := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(response, r)
			if scenario == "allowed" || scenario == "held" || scenario == "no_project" || scenario == "escaped" {
				if response.Code != http.StatusOK {
					t.Fatalf("read=%d %s", response.Code, response.Body.String())
				}
				var got team.AssistanceAnswerObservation
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.Request.RequestID != f.requestID || got.Answer.Text != input.Text || got.Answer.AuthorPrincipalID != "human" {
					t.Fatal("wrong retained authored answer")
				}
				if response.Header().Get("Cache-Control") != "no-store, private" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("missing private read headers")
				}
			} else {
				if response.Code == http.StatusOK {
					t.Fatalf("unauthorized answer disclosed for %s", scenario)
				}
				for _, private := range []string{"PRIVATE_HUMAN_REPLY", "PRIVATE_ASSISTANCE_QUESTION_CANARY", f.runID, f.taskID} {
					if strings.Contains(response.Body.String(), private) {
						t.Fatalf("denied answer leaked %q", private)
					}
				}
			}
			if after := answerHTTPExecutionState(t, f); after != before {
				t.Fatal("answer read changed waiting owner/history")
			}
			var events int
			if err := f.db.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&events); err != nil || events != beforeEvents {
				t.Fatal("read wrote audit/history")
			}
		})
	}
}
