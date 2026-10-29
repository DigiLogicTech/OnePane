package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

type pageTeamService struct {
	teamService
	calls   int
	onPage  func()
	options team.MessagePageOptions
}

func (f *pageTeamService) Session(context.Context, string) (team.Session, error) {
	return team.Session{ID: "room", WorkspaceID: "tenant"}, nil
}
func (f *pageTeamService) PageMessages(_ context.Context, id string, o team.MessagePageOptions) (team.MessagePage, error) {
	f.calls++
	f.options = o
	if f.onPage != nil {
		f.onPage()
	}
	if id != "room" {
		return team.MessagePage{}, errors.New("wrong room")
	}
	return team.MessagePage{Messages: []team.Message{}}, nil
}

type pageAuth struct {
	revoked bool
	caps    []string
}

func (a *pageAuth) Authenticate(*http.Request) (Identity, error) {
	return Identity{PrincipalID: "reader"}, nil
}
func (a *pageAuth) AuthorizeWorkspace(_ context.Context, _ Identity, ws, cap string) error {
	a.caps = append(a.caps, cap)
	if a.revoked || ws != "tenant" || cap != "team.read" {
		return ErrForbidden
	}
	return nil
}

func TestTeamMessagePageAuthorizationRevocationAndInputBounds(t *testing.T) {
	for _, tc := range []struct {
		name, query    string
		denied, revoke bool
		status, calls  int
	}{
		{"latest", "", false, false, 200, 1}, {"older", "?before=m1&limit=20", false, false, 200, 1},
		{"denied", "", true, false, 403, 0}, {"revoked_during_read", "", false, true, 403, 1},
		{"oversized", "?limit=51", false, false, 400, 0}, {"invalid", "?limit=nope", false, false, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := &pageAuth{revoked: tc.denied}
			svc := &pageTeamService{}
			if tc.revoke {
				svc.onPage = func() { auth.revoked = true }
			}
			s := NewServer(nil, fakeEvents{}, auth)
			s.SetTeam(svc)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/team-sessions/room/messages/page"+tc.query, nil))
			if w.Code != tc.status || svc.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, svc.calls, w.Body.String())
			}
			if w.Code == 200 && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("history must not be cached")
			}
			if tc.name == "older" && (svc.options.BeforeID != "m1" || svc.options.Limit != 20) {
				t.Fatal("cursor lost")
			}
		})
	}
}
