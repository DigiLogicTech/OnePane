package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/webauth"
)

const WebSessionCookie = "onepane_session"
const WebCSRFCookie = "onepane_csrf"

type HybridAuthorizer struct {
	bearer *BearerAuthorizer
	web    *webauth.Service
}

func NewHybridAuthorizer(bearer *BearerAuthorizer, web *webauth.Service) *HybridAuthorizer {
	return &HybridAuthorizer{bearer: bearer, web: web}
}

func (a *HybridAuthorizer) Authenticate(r *http.Request) (Identity, error) {
	if strings.HasPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ") {
		i, err := a.bearer.Authenticate(r)
		if err == nil {
			i.AuthMethod = "bearer"
		}
		return i, err
	}
	c, err := r.Cookie(WebSessionCookie)
	if err != nil || a.web == nil {
		return Identity{}, ErrUnauthenticated
	}
	unsafe := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
	si, err := a.web.AuthenticateSession(r.Context(), c.Value, r.Header.Get("X-OnePane-CSRF"), unsafe)
	if err != nil {
		if errors.Is(err, webauth.ErrCSRF) {
			return Identity{}, ErrForbidden
		}
		return Identity{}, ErrUnauthenticated
	}
	ws, caps, err := a.web.WorkspaceScopes(r.Context(), si.PrincipalID)
	if err != nil {
		return Identity{}, err
	}
	wsJSON, _ := json.Marshal(ws)
	capJSON, _ := json.Marshal(caps)
	return Identity{PrincipalID: si.PrincipalID, PrincipalType: si.PrincipalType, CredentialID: si.SessionID, WorkspaceScope: wsJSON, CapabilityScope: capJSON, AuthMethod: "session"}, nil
}

func (a *HybridAuthorizer) AuthorizeWorkspace(ctx context.Context, i Identity, workspaceID, capability string) error {
	return a.bearer.AuthorizeWorkspace(ctx, i, workspaceID, capability)
}
