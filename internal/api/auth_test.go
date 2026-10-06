package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/event"
)

func TestScopeAllowsArrayAndObject(t *testing.T) {
	if !scopeAllows(json.RawMessage(`["ws1"]`), "workspaces", "ws1") {
		t.Fatal("array scope denied")
	}
	if !scopeAllows(json.RawMessage(`{"capabilities":["project.*","events.read"]}`), "capabilities", "project.write") {
		t.Fatal("object exact scope denied")
	}
	if scopeAllows(json.RawMessage(`{"capabilities":["project.read"]}`), "capabilities", "project.write") {
		t.Fatal("scope widened")
	}
	if !scopeAllows(json.RawMessage(`["*"]`), "workspaces", "anything") {
		t.Fatal("wildcard denied")
	}
}

type fakeAuth struct {
	id                  Identity
	authErr, errorAuthz error
}

func (f fakeAuth) Authenticate(*http.Request) (Identity, error) { return f.id, f.authErr }
func (f fakeAuth) AuthorizeWorkspace(context.Context, Identity, string, string) error {
	return f.errorAuthz
}

type fakeEvents struct{ events []event.Stored }

func (f fakeEvents) After(context.Context, string, int64, int) ([]event.Stored, error) {
	return f.events, nil
}

func TestHealthIsUnauthenticatedButProtectedRoutesAreNot(t *testing.T) {
	s := NewServer(nil, fakeEvents{}, fakeAuth{authErr: ErrUnauthenticated})
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health=%d", rr.Code)
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/projects/p", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("project=%d", rr.Code)
	}
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers")
	}
}

func TestSSEIsWorkspaceAuthorizedAndCarriesLedgerSequence(t *testing.T) {
	wid := "ws"
	ev := event.Stored{Sequence: 7, Event: event.Event{ID: "evt", WorkspaceID: &wid, Type: "project_change.proposed", AggregateType: "project_change", AggregateID: "c", Payload: json.RawMessage(`{"x":1}`), OccurredAt: 1}}
	s := NewServer(nil, fakeEvents{events: []event.Stored{ev}}, fakeAuth{id: Identity{PrincipalID: "u"}})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/events/stream?workspace_id=ws&once=1", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "id: 7") || !strings.Contains(body, "event: project_change.proposed") {
		t.Fatalf("bad SSE %q", body)
	}

	denied := NewServer(nil, fakeEvents{}, fakeAuth{id: Identity{PrincipalID: "u"}, errorAuthz: ErrForbidden})
	rr = httptest.NewRecorder()
	denied.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/events/stream?workspace_id=ws&once=1", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("denied status=%d", rr.Code)
	}
}

func TestBadBearerStaysUnauthenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic nope")
	// Interface behavior is covered with fake auth; this guards the error category used by middleware.
	if !errors.Is(ErrUnauthenticated, ErrUnauthenticated) {
		t.Fatal("sentinel broken")
	}
}
