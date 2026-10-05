package provideroauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/vault"
)

type Config struct {
	PresetID         string   `json:"preset_id"`
	AuthorizationURL string   `json:"authorization_url"`
	TokenURL         string   `json:"token_url"`
	ClientID         string   `json:"client_id"`
	Scopes           []string `json:"scopes"`
	Enabled          bool     `json:"enabled"`
	Revision         int64    `json:"revision"`
	UpdatedAt        int64    `json:"updated_at"`
}
type StartResult struct {
	FlowID           string `json:"flow_id"`
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
	ExpiresAt        int64  `json:"expires_at"`
}
type Connection struct {
	ID               string          `json:"id"`
	WorkspaceID      string          `json:"workspace_id"`
	PresetID         string          `json:"preset_id"`
	AccessSecretRef  string          `json:"access_secret_ref"`
	RefreshSecretRef *string         `json:"refresh_secret_ref,omitempty"`
	Scopes           []string        `json:"scopes"`
	Account          json.RawMessage `json:"account"`
	ExpiresAt        *int64          `json:"expires_at,omitempty"`
	Status           string          `json:"status"`
	Revision         int64           `json:"revision"`
	CreatedAt        int64           `json:"created_at"`
	UpdatedAt        int64           `json:"updated_at"`
}
type Service struct {
	db     *sql.DB
	tx     storage.Transactor
	clock  clock.Clock
	ids    id.Generator
	vault  *vault.Service
	client *http.Client
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, v *vault.Service) *Service {
	return &Service{db: db, tx: tx, clock: clk, ids: id.Generator{}, vault: v, client: &http.Client{Timeout: 20 * time.Second}}
}
func secureToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil { return "", err }
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func validHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" { return false }
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))
}
func (s *Service) Configs(ctx context.Context) ([]Config, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT preset_id,authorization_url,token_url,client_id,scopes_json,enabled,revision,updated_at FROM provider_oauth_configs ORDER BY preset_id`)
	if err != nil { return nil, err }
	defer rows.Close()
	out := []Config{}
	for rows.Next() {
		var x Config
		var scopes string
		var enabled int
		if err := rows.Scan(&x.PresetID, &x.AuthorizationURL, &x.TokenURL, &x.ClientID, &scopes, &enabled, &x.Revision, &x.UpdatedAt); err != nil { return nil, err }
		_ = json.Unmarshal([]byte(scopes), &x.Scopes)
		x.Enabled = enabled != 0
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Service) UpsertConfig(ctx context.Context, c Config) (Config, error) {
	c.PresetID = strings.TrimSpace(c.PresetID)
	c.AuthorizationURL = strings.TrimSpace(c.AuthorizationURL)
	c.TokenURL = strings.TrimSpace(c.TokenURL)
	c.ClientID = strings.TrimSpace(c.ClientID)
	if c.PresetID == "" || c.ClientID == "" || !validHTTPS(c.AuthorizationURL) || !validHTTPS(c.TokenURL) {
		return Config{}, errors.New("OAuth config requires preset, valid authorization/token URLs and client ID")
	}
	scopes, _ := json.Marshal(c.Scopes)
	now := s.clock.UnixMilli()
	enabled := 0
	if c.Enabled { enabled = 1 }
	_, err := s.db.ExecContext(ctx, `INSERT INTO provider_oauth_configs(preset_id,authorization_url,token_url,client_id,scopes_json,extra_auth_json,enabled,revision,updated_at)
		VALUES(?,?,?,?,?,'{}',?,1,?)
		ON CONFLICT(preset_id) DO UPDATE SET authorization_url=excluded.authorization_url,token_url=excluded.token_url,client_id=excluded.client_id,
		scopes_json=excluded.scopes_json,enabled=excluded.enabled,revision=provider_oauth_configs.revision+1,updated_at=excluded.updated_at`,
		c.PresetID, c.AuthorizationURL, c.TokenURL, c.ClientID, string(scopes), enabled, now)
	if err != nil { return Config{}, err }
	var out Config
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT preset_id,authorization_url,token_url,client_id,scopes_json,enabled,revision,updated_at FROM provider_oauth_configs WHERE preset_id=?`, c.PresetID).
		Scan(&out.PresetID, &out.AuthorizationURL, &out.TokenURL, &out.ClientID, &raw, &enabled, &out.Revision, &out.UpdatedAt); err != nil { return Config{}, err }
	_ = json.Unmarshal([]byte(raw), &out.Scopes)
	out.Enabled = enabled != 0
	return out, nil
}
func (s *Service) config(ctx context.Context, preset string) (Config, error) {
	var x Config
	var scopes string
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT preset_id,authorization_url,token_url,client_id,scopes_json,enabled,revision,updated_at FROM provider_oauth_configs WHERE preset_id=?`, preset).
		Scan(&x.PresetID, &x.AuthorizationURL, &x.TokenURL, &x.ClientID, &scopes, &enabled, &x.Revision, &x.UpdatedAt)
	_ = json.Unmarshal([]byte(scopes), &x.Scopes)
	x.Enabled = enabled != 0
	return x, err
}
func (s *Service) Start(ctx context.Context, workspaceID, preset, redirectURI, actor string) (StartResult, error) {
	cfg, err := s.config(ctx, preset)
	if err != nil { return StartResult{}, errors.New("OAuth is not configured for this provider") }
	if !cfg.Enabled { return StartResult{}, errors.New("OAuth is disabled for this provider") }
	u, err := url.Parse(redirectURI)
	if err != nil || u.Scheme == "" || u.Host == "" { return StartResult{}, errors.New("valid OAuth redirect URI required") }
	state, err := secureToken(24); if err != nil { return StartResult{}, err }
	verifier, err := secureToken(48); if err != nil { return StartResult{}, err }
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	fid, _ := s.ids.New("oauthflow")
	now := s.clock.UnixMilli()
	expires := now + 10*60*1000
	_, err = s.db.ExecContext(ctx, `INSERT INTO provider_oauth_flows(id,workspace_id,preset_id,state,code_verifier,redirect_uri,status,requested_by,expires_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,'pending',?,?,?,?)`, fid, workspaceID, preset, state, verifier, redirectURI, actor, expires, now, now)
	if err != nil { return StartResult{}, err }
	q := url.Values{
		"response_type": {"code"}, "client_id": {cfg.ClientID}, "redirect_uri": {redirectURI}, "state": {state},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	if len(cfg.Scopes) > 0 { q.Set("scope", strings.Join(cfg.Scopes, " ")) }
	auth := cfg.AuthorizationURL
	if strings.Contains(auth, "?") { auth += "&" + q.Encode() } else { auth += "?" + q.Encode() }
	return StartResult{FlowID: fid, AuthorizationURL: auth, State: state, ExpiresAt: expires}, nil
}
func nullableString(v *string) any { if v == nil { return nil }; return *v }
func nullableInt64(v *int64) any { if v == nil { return nil }; return *v }

func (s *Service) Complete(ctx context.Context, state, code string) (Connection, error) {
	var fid, ws, preset, verifier, redirect, actor, status string
	var expires int64
	if err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,preset_id,code_verifier,redirect_uri,requested_by,status,expires_at FROM provider_oauth_flows WHERE state=?`, state).
		Scan(&fid, &ws, &preset, &verifier, &redirect, &actor, &status, &expires); err != nil { return Connection{}, err }
	now := s.clock.UnixMilli()
	if status != "pending" || expires < now { return Connection{}, errors.New("OAuth flow expired or is no longer pending") }
	cfg, err := s.config(ctx, preset)
	if err != nil { return Connection{}, err }
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "client_id": {cfg.ClientID}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil { return Connection{}, err }
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil { return Connection{}, err }
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("OAuth token exchange returned HTTP %d", resp.StatusCode)
		_, _ = s.db.ExecContext(ctx, `UPDATE provider_oauth_flows SET status='failed',failure_reason=?,updated_at=? WHERE id=?`, msg, now, fid)
		return Connection{}, errors.New(msg)
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || strings.TrimSpace(tok.AccessToken) == "" {
		return Connection{}, errors.New("OAuth token response did not contain an access token")
	}
	wsCopy := ws
	accessRec, err := s.vault.CreateProviderCredential(ctx, vault.ProviderCredentialCommand{
		WorkspaceID: &wsCopy, Scope: vault.ScopeDirectProvider, UpstreamProvider: preset, Kind: vault.CredentialAccessToken,
		Value: []byte(tok.AccessToken), DisplayLabel: preset + " OAuth access token", CreatedBy: actor,
	})
	if err != nil { return Connection{}, err }
	accessRef := "vault:" + accessRec.ID
	var refreshRef *string
	if tok.RefreshToken != "" {
		meta, _ := json.Marshal(map[string]any{"credential_scope": "provider", "upstream_provider": preset, "credential_kind": "refresh-token", "display_label": preset + " OAuth refresh token"})
		r, err := s.vault.Create(ctx, vault.CreateCommand{
			WorkspaceID: &wsCopy, LogicalName: "provider/" + preset + "/oauth-refresh-token", ProviderType: "provider-oauth:" + preset,
			Value: []byte(tok.RefreshToken), Metadata: meta, CreatedBy: actor,
		})
		if err != nil { return Connection{}, err }
		v := "vault:" + r.ID
		refreshRef = &v
	}
	scopes := cfg.Scopes
	if tok.Scope != "" { scopes = strings.Fields(tok.Scope) }
	scopesRaw, _ := json.Marshal(scopes)
	cid, _ := s.ids.New("oauthconn")
	var exp *int64
	if tok.ExpiresIn > 0 { v := now + tok.ExpiresIn*1000; exp = &v }
	_, err = s.db.ExecContext(ctx, `INSERT INTO provider_oauth_connections(id,workspace_id,preset_id,access_secret_ref,refresh_secret_ref,scopes_json,account_json,expires_at,status,revision,created_at,updated_at)
		VALUES(?,?,?,?,?,?,'{}',?,'connected',1,?,?)`, cid, ws, preset, accessRef, nullableString(refreshRef), string(scopesRaw), nullableInt64(exp), now, now)
	if err != nil { return Connection{}, err }
	_, _ = s.db.ExecContext(ctx, `UPDATE provider_oauth_flows SET status='completed',updated_at=? WHERE id=?`, now, fid)
	return s.Connection(ctx, cid)
}
func scanConnection(row interface{ Scan(...any) error }) (Connection, error) {
	var x Connection
	var refresh sql.NullString
	var scopes, account string
	var exp sql.NullInt64
	err := row.Scan(&x.ID, &x.WorkspaceID, &x.PresetID, &x.AccessSecretRef, &refresh, &scopes, &account, &exp, &x.Status, &x.Revision, &x.CreatedAt, &x.UpdatedAt)
	if refresh.Valid { v := refresh.String; x.RefreshSecretRef = &v }
	if exp.Valid { v := exp.Int64; x.ExpiresAt = &v }
	_ = json.Unmarshal([]byte(scopes), &x.Scopes)
	x.Account = json.RawMessage(account)
	return x, err
}
func (s *Service) Connection(ctx context.Context, idv string) (Connection, error) {
	return scanConnection(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,preset_id,access_secret_ref,refresh_secret_ref,scopes_json,account_json,expires_at,status,revision,created_at,updated_at FROM provider_oauth_connections WHERE id=?`, idv))
}
func (s *Service) Connections(ctx context.Context, workspaceID string) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,preset_id,access_secret_ref,refresh_secret_ref,scopes_json,account_json,expires_at,status,revision,created_at,updated_at FROM provider_oauth_connections WHERE workspace_id=? ORDER BY preset_id,updated_at DESC`, workspaceID)
	if err != nil { return nil, err }
	defer rows.Close()
	out := []Connection{}
	for rows.Next() { x, e := scanConnection(rows); if e != nil { return nil, e }; out = append(out, x) }
	return out, rows.Err()
}
func secretID(ref string) string { return strings.TrimPrefix(strings.TrimSpace(ref), "vault:") }
func (s *Service) Revoke(ctx context.Context, idv, actor string) (Connection, error) {
	x, err := s.Connection(ctx, idv)
	if err != nil { return x, err }
	if x.Status == "revoked" { return x, nil }
	_ = s.vault.Revoke(ctx, secretID(x.AccessSecretRef), actor)
	if x.RefreshSecretRef != nil { _ = s.vault.Revoke(ctx, secretID(*x.RefreshSecretRef), actor) }
	now := s.clock.UnixMilli()
	if _, err := s.db.ExecContext(ctx, `UPDATE provider_oauth_connections SET status='revoked',revision=revision+1,updated_at=? WHERE id=?`, now, idv); err != nil { return x, err }
	return s.Connection(ctx, idv)
}
