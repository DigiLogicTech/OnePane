package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/provideroauth"
)

type providerOAuthService interface {
	Configs(context.Context) ([]provideroauth.Config, error)
	UpsertConfig(context.Context, provideroauth.Config) (provideroauth.Config, error)
	Start(context.Context, string, string, string, string) (provideroauth.StartResult, error)
	Complete(context.Context, string, string) (provideroauth.Connection, error)
	Connections(context.Context, string) ([]provideroauth.Connection, error)
	Connection(context.Context, string) (provideroauth.Connection, error)
	Revoke(context.Context, string, string) (provideroauth.Connection, error)
}

func (s *Server) listOAuthConfigs(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r); if !ok { return }
	ws := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if ws == "" { writeError(w, http.StatusBadRequest, "workspace_id is required"); return }
	if !s.authorize(w, r, i, ws, "project.read") { return }
	rows, err := s.providerOAuth.Configs(r.Context())
	respondDomain(w, rows, err, http.StatusOK)
}
func (s *Server) saveOAuthConfig(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r); if !ok { return }
	var in struct {
		WorkspaceID      string   `json:"workspace_id"`
		AuthorizationURL string   `json:"authorization_url"`
		TokenURL         string   `json:"token_url"`
		ClientID         string   `json:"client_id"`
		Scopes           []string `json:"scopes"`
		Enabled          bool     `json:"enabled"`
	}
	if !decodeJSON(w, r, &in) { return }
	if !s.authorize(w, r, i, in.WorkspaceID, "project.write") { return }
	out, err := s.providerOAuth.UpsertConfig(r.Context(), provideroauth.Config{
		PresetID: r.PathValue("presetID"), AuthorizationURL: in.AuthorizationURL, TokenURL: in.TokenURL,
		ClientID: in.ClientID, Scopes: in.Scopes, Enabled: in.Enabled,
	})
	respondDomain(w, out, err, http.StatusOK)
}
func (s *Server) startProviderOAuth(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r); if !ok { return }
	var in struct {
		WorkspaceID string `json:"workspace_id"`
		RedirectURI string `json:"redirect_uri"`
	}
	if !decodeJSON(w, r, &in) { return }
	if !s.authorize(w, r, i, in.WorkspaceID, "provider.write") { return }
	out, err := s.providerOAuth.Start(r.Context(), in.WorkspaceID, r.PathValue("presetID"), in.RedirectURI, i.PrincipalID)
	respondDomain(w, out, err, http.StatusCreated)
}
func (s *Server) completeProviderOAuth(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if state == "" || code == "" { writeError(w, http.StatusBadRequest, "OAuth state and code are required"); return }
	out, err := s.providerOAuth.Complete(r.Context(), state, code)
	if err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	raw, _ := json.Marshal(out)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("<!doctype html><title>OnePane OAuth</title><script>if(window.opener){window.opener.postMessage({type:'onepane-oauth-complete',connection:" + string(raw) + "},window.location.origin)}window.close()</script><p>Account connected. You can close this window.</p>"))
}
func (s *Server) listProviderOAuthConnections(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r); if !ok { return }
	ws := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if ws == "" { writeError(w, http.StatusBadRequest, "workspace_id is required"); return }
	if !s.authorize(w, r, i, ws, "provider.read") { return }
	rows, err := s.providerOAuth.Connections(r.Context(), ws)
	respondDomain(w, rows, err, http.StatusOK)
}
func (s *Server) revokeProviderOAuth(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r); if !ok { return }
	x, err := s.providerOAuth.Connection(r.Context(), r.PathValue("connectionID"))
	if err != nil { respondDomain(w, nil, err, 0); return }
	if !s.authorize(w, r, i, x.WorkspaceID, "provider.write") { return }
	out, err := s.providerOAuth.Revoke(r.Context(), x.ID, i.PrincipalID)
	respondDomain(w, out, err, http.StatusOK)
}
