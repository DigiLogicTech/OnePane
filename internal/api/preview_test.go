package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type fakeIngressRoutes struct {
	route projectworkspace.IngressRoute
	err   error
}

func (f fakeIngressRoutes) ResolveIngressRoute(context.Context, string) (projectworkspace.IngressRoute, error) {
	return f.route, f.err
}

type fakePreviewMinter struct{ endpoint, principal, credential string }

func (f *fakePreviewMinter) MintEndpointGrant(endpointID, principalID, credentialID string) (string, int64, error) {
	f.endpoint, f.principal, f.credential = endpointID, principalID, credentialID
	return "https://preview.example/session/token", 1234, nil
}

func TestPreviewSessionRequiresProjectReadAndUsesCredentialIdentity(t *testing.T) {
	minter := &fakePreviewMinter{}
	s := NewServer(nil, fakeEvents{}, fakeAuth{id: Identity{PrincipalID: "person", CredentialID: "cred"}})
	s.ingressRoutes = fakeIngressRoutes{route: projectworkspace.IngressRoute{WorkspaceID: "ws", Endpoint: projectworkspace.Endpoint{ID: "ep", Protocol: "http"}, Route: projectworkspace.EndpointRoute{Status: "verified"}}}
	s.SetPreviewSessions(minter)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/project-endpoints/ep/preview-session", nil))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if minter.endpoint != "ep" || minter.principal != "person" || minter.credential != "cred" {
		t.Fatalf("mint identity %#v", minter)
	}

	denied := NewServer(nil, fakeEvents{}, fakeAuth{id: Identity{PrincipalID: "person", CredentialID: "cred"}, errorAuthz: ErrForbidden})
	denied.ingressRoutes = s.ingressRoutes
	denied.SetPreviewSessions(&fakePreviewMinter{})
	rr = httptest.NewRecorder()
	denied.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/project-endpoints/ep/preview-session", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("denied=%d", rr.Code)
	}
}
