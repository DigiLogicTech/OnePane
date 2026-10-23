package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type cancelTeamService struct {
	teamService
	calls   int
	command team.CancelExecutionCommand
	err     error
}

func (f *cancelTeamService) Session(context.Context, string) (team.Session, error) {
	return team.Session{ID: "room", TaskID: "root", WorkspaceID: "tenant"}, nil
}
func (f *cancelTeamService) RequestExecutionCancellation(_ context.Context, c team.CancelExecutionCommand) (team.Session, error) {
	f.calls++
	f.command = c
	return team.Session{ID: "room", TaskID: "root", WorkspaceID: "tenant"}, f.err
}

type cancelTeamAuth struct{ denied string }

func (a cancelTeamAuth) Authenticate(*http.Request) (Identity, error) {
	return Identity{PrincipalID: "human"}, nil
}
func (a cancelTeamAuth) AuthorizeWorkspace(_ context.Context, _ Identity, ws, cap string) error {
	if ws != "tenant" || a.denied == cap {
		return ErrForbidden
	}
	return nil
}

func TestTeamCancellationAPIRequiresBothWriteScopesAndBindsHumanAndPlan(t *testing.T) {
	for _, tc := range []struct {
		name, denied  string
		err           error
		status, calls int
	}{
		{"accepted", "", nil, http.StatusAccepted, 1},
		{"team_denied", "team.write", nil, http.StatusForbidden, 0},
		{"task_denied", "task.write", nil, http.StatusForbidden, 0},
		{"stale_task", "", task.ErrRevisionConflict, http.StatusConflict, 1},
		{"not_human", "", team.ErrHumanRequired, http.StatusForbidden, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &cancelTeamService{err: tc.err}
			server := NewServer(nil, fakeEvents{}, cancelTeamAuth{denied: tc.denied})
			server.SetTeam(svc)
			req := httptest.NewRequest(http.MethodPost, "/v1/team-sessions/room/execution/cancel", strings.NewReader(`{"plan_id":"accepted","expected_task_revision":7,"reason":"Stop this Team"}`))
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, req)
			if w.Code != tc.status || svc.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, svc.calls, w.Body.String())
			}
			if svc.calls > 0 && (svc.command.ActorPrincipalID != "human" || svc.command.SessionID != "room" || svc.command.PlanID != "accepted" || svc.command.ExpectedTaskRevision != 7) {
				t.Fatalf("request scope drift: %+v", svc.command)
			}
		})
	}
}
