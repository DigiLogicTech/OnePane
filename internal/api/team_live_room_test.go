package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

type liveRoomService struct {
	teamService
	room   team.LiveRoom
	onRead func()
}

func (f *liveRoomService) Session(context.Context, string) (team.Session, error) {
	return team.Session{ID: "room", WorkspaceID: "tenant", TaskID: "task"}, nil
}
func (f *liveRoomService) LiveRoom(context.Context, string) (team.LiveRoom, error) {
	if f.onRead != nil {
		f.onRead()
	}
	return f.room, nil
}

func TestTeamLiveRoomReauthorizesAndRejectsForeignProjection(t *testing.T) {
	for _, name := range []string{"allowed", "revoked", "foreign", "task_permission_denied"} {
		t.Run(name, func(t *testing.T) {
			auth := &liveRoomAuth{denyTask: name == "task_permission_denied"}
			svc := &liveRoomService{room: team.LiveRoom{ID: "room", WorkspaceID: "tenant", TaskID: "task", Members: []team.LiveMember{}}}
			if name == "revoked" {
				svc.onRead = func() { auth.revoked = true }
			}
			if name == "foreign" {
				svc.room.TaskID = "other-task"
			}
			s := NewServer(nil, fakeEvents{}, auth)
			s.SetTeam(svc)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/team-sessions/room/live", nil))
			want := 200
			if name != "allowed" {
				want = 403
			}
			if w.Code != want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

type liveRoomAuth struct{ revoked, denyTask bool }

func (a *liveRoomAuth) Authenticate(*http.Request) (Identity, error) {
	return Identity{PrincipalID: "human"}, nil
}
func (a *liveRoomAuth) AuthorizeWorkspace(_ context.Context, _ Identity, ws, cap string) error {
	if a.revoked || ws != "tenant" || (a.denyTask && cap == "task.read") {
		return ErrForbidden
	}
	return nil
}
