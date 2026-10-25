//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

type observedAssistanceDiscovery struct {
	*team.Service
	reads   int
	after   func(int)
	replace func(int, []team.AssistanceRequest) []team.AssistanceRequest
}

func (s *observedAssistanceDiscovery) AssistanceRequests(ctx context.Context, workspace, session, reader string) ([]team.AssistanceRequest, error) {
	out, err := any(s.Service).(teamAssistanceRequestsReader).AssistanceRequests(ctx, workspace, session, reader)
	s.reads++
	if err == nil && s.replace != nil {
		out = s.replace(s.reads, out)
	}
	if err == nil && s.after != nil {
		s.after(s.reads)
	}
	return out, err
}

func getAssistanceDiscovery(t *testing.T, f *teamAssistanceHTTPFixture, auth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/v1/team-sessions/"+f.sessionID+"/assistance", nil)
	if auth != nil {
		auth(r)
	}
	w := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(w, r)
	return w
}

func TestTeamAssistanceDiscoveryHTTPCurrentPrivateRequests(t *testing.T) {
	for _, method := range []string{"bearer", "session"} {
		t.Run(method, func(t *testing.T) {
			f := newTeamAssistanceHTTPFixture(t, false)
			auth := answerHTTPAuth(t, f, method)
			f.server.SetTasks(f.tasks)
			before := assistanceHTTPReadState(t, f)
			response := getAssistanceDiscovery(t, f, auth)
			if response.Code != http.StatusOK {
				t.Fatalf("discovery status=%d: %s", response.Code, response.Body.String())
			}
			var out struct {
				Requests []team.AssistanceRequest `json:"requests"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil || len(out.Requests) != 1 || out.Requests[0].RequestID != f.requestID || out.Requests[0].Question == "" {
				t.Fatalf("wrong requests %+v err=%v", out, err)
			}
			if response.Header().Get("Cache-Control") != "no-store, private" || response.Header().Get("X-Content-Type-Options") != "nosniff" || len(response.Body.Bytes()) > 128<<10 {
				t.Fatal("unsafe discovery response")
			}
			if after := assistanceHTTPReadState(t, f); after != before {
				t.Fatal("discovery changed execution or history")
			}
		})
	}
}

func TestTeamAssistanceDiscoveryHTTPRejectsChangedAuthority(t *testing.T) {
	for _, change := range []string{"credential_between", "credential_after_final", "capability_between", "capability_after_final", "membership_between", "project_after_final", "named_after_final", "root_scope_after_final", "worker_between"} {
		t.Run(change, func(t *testing.T) {
			f := newTeamAssistanceHTTPFixture(t, false)
			auth := answerHTTPAuth(t, f, "bearer")
			f.server.SetTasks(f.tasks)
			before := assistanceHTTPReadState(t, f)
			observer := &observedAssistanceDiscovery{Service: f.teams}
			observer.after = func(read int) {
				wanted := 1
				if strings.Contains(change, "after_final") {
					wanted = 2
				}
				if read != wanted {
					return
				}
				query := map[string]string{
					"credential_between":     `UPDATE api_credentials SET revoked_at=1 WHERE id='answer-api'`,
					"credential_after_final": `UPDATE api_credentials SET revoked_at=1 WHERE id='answer-api'`,
					"capability_between":     `UPDATE api_credentials SET capability_scope_json='["team.read","task.read"]' WHERE id='answer-api'`,
					"capability_after_final": `UPDATE api_credentials SET capability_scope_json='["team.read","task.read"]' WHERE id='answer-api'`,
					"membership_between":     `UPDATE workspace_memberships SET status='revoked' WHERE principal_id='human'`,
					"project_after_final":    `UPDATE projects SET status='archived' WHERE id='` + f.projectID + `'`,
					"named_after_final":      `UPDATE project_workspaces SET status='archived' WHERE id='` + f.workspaceID + `'`,
					"root_scope_after_final": `UPDATE tasks SET artifact_session_id='library-session' WHERE id='` + f.rootID + `'`,
					"worker_between":         `UPDATE agent_worker_runs SET revision=revision+1 WHERE id='http-help-run'`,
				}[change]
				if change == "root_scope_after_final" {
					query = `UPDATE tasks SET revision=revision+1 WHERE id='` + f.rootID + `'`
				}
				if _, err := f.db.SQL().Exec(query); err != nil {
					t.Fatal(err)
				}
				before = assistanceHTTPReadState(t, f)
			}
			f.server.SetTeam(observer)
			response := getAssistanceDiscovery(t, f, auth)
			if response.Code != http.StatusConflict && response.Code != http.StatusForbidden && response.Code != http.StatusUnauthorized {
				t.Fatalf("changed authority disclosed discovery %s", change)
			}
			if strings.Contains(response.Body.String(), "PRIVATE_") {
				t.Fatal("denial disclosed private question")
			}
			if after := assistanceHTTPReadState(t, f); after != before {
				t.Fatal("denied discovery changed history")
			}
		})
	}
}

func TestTeamAssistanceDiscoveryHTTPRejectsMalformedOrChangedCollections(t *testing.T) {
	for _, change := range []string{"oversized", "aggregate_bytes", "duplicate", "foreign_session", "foreign_scope", "invalid_utf8", "unsafe_revision", "changed_second", "disappeared_second"} {
		t.Run(change, func(t *testing.T) {
			f := newTeamAssistanceHTTPFixture(t, false)
			f.server.SetTasks(f.tasks)
			before := assistanceHTTPReadState(t, f)
			f.server.SetTeam(&observedAssistanceDiscovery{Service: f.teams, replace: func(read int, out []team.AssistanceRequest) []team.AssistanceRequest {
				if len(out) != 1 {
					t.Fatalf("fixture requests=%d", len(out))
				}
				switch change {
				case "oversized":
					return append(out, make([]team.AssistanceRequest, 8)...)
				case "aggregate_bytes":
					large := make([]team.AssistanceRequest, 8)
					for i := range large {
						large[i] = out[0]
						v := strings.Repeat("<", 256)
						large[i].RequestID = strings.Repeat("<", 255) + strconv.Itoa(i)
						large[i].AssignmentKey, large[i].MemberID, large[i].TaskID, large[i].AttemptID, large[i].RunID, large[i].WaitStepID, large[i].NoticeMessageID = v, v, v, v, v, v, v
						large[i].Question = strings.Repeat("\x00", 511) + "q"
						large[i].MemberName = strings.Repeat("\x00", 256)
					}
					return large
				case "duplicate":
					return append(out, out[0])
				case "foreign_session":
					out[0].SessionID = "foreign"
				case "foreign_scope":
					out[0].ProjectID = "foreign"
				case "invalid_utf8":
					out[0].Question = string([]byte{0xff})
				case "unsafe_revision":
					out[0].WorkerRevision = 1 << 53
				case "changed_second":
					if read == 2 {
						out[0].Question = "replacement private question"
					}
				case "disappeared_second":
					if read == 2 {
						return nil
					}
				}
				return out
			}})
			response := getAssistanceDiscovery(t, f, nil)
			if (response.Code != http.StatusForbidden && response.Code != http.StatusConflict) || strings.Contains(response.Body.String(), "PRIVATE_") {
				t.Fatalf("unsafe collection status=%d: %s", response.Code, response.Body.String())
			}
			if after := assistanceHTTPReadState(t, f); after != before {
				t.Fatal("malformed discovery changed history")
			}
		})
	}
}

func TestTeamAssistanceDiscoveryHTTPEmptyRequiresCanonicalProjectPermission(t *testing.T) {
	for _, empty := range []bool{false, true} {
		for _, noProject := range []bool{false, true} {
			f := newTeamAssistanceHTTPFixture(t, false, noProject)
			auth := answerHTTPAuth(t, f, "bearer")
			f.server.SetTasks(f.tasks)
			if _, err := f.db.SQL().Exec(`UPDATE api_credentials SET capability_scope_json='["team.read","task.read"]' WHERE id='answer-api'`); err != nil {
				t.Fatal(err)
			}
			if empty {
				f.server.SetTeam(&observedAssistanceDiscovery{Service: f.teams, replace: func(int, []team.AssistanceRequest) []team.AssistanceRequest { return nil }})
			}
			before := assistanceHTTPReadState(t, f)
			response := getAssistanceDiscovery(t, f, auth)
			want := http.StatusForbidden
			if noProject {
				want = http.StatusOK
			}
			if response.Code != want {
				t.Fatalf("empty=%v noProject=%v status=%d want=%d: %s", empty, noProject, response.Code, want, response.Body.String())
			}
			if empty && noProject && response.Body.String() != `{"requests":[]}` {
				t.Fatalf("empty shape=%s", response.Body.String())
			}
			if after := assistanceHTTPReadState(t, f); after != before {
				t.Fatal("empty discovery changed history")
			}
		}
	}
}
