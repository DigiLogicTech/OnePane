package webauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

var (
	ErrSetupComplete      = errors.New("first-run setup is already complete")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidSession     = errors.New("invalid session")
	ErrCSRF               = errors.New("csrf validation failed")
)

const localIssuer = "onepane-local"

type Service struct {
	db         *sql.DB
	tx         storage.Transactor
	events     event.Store
	ids        id.Generator
	clock      clock.Clock
	sessionTTL time.Duration
	idleTTL    time.Duration
}

type SetupStatus struct {
	Required              bool `json:"required"`
	HumanPrincipals       int  `json:"human_principals"`
	ActiveLocalIdentities int  `json:"active_local_identities"`
}

type BootstrapAdminCommand struct {
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	Password      string `json:"password"`
	WorkspaceName string `json:"workspace_name"`
}

type SessionResult struct {
	PrincipalID  string `json:"principal_id"`
	DisplayName  string `json:"display_name"`
	Username     string `json:"username"`
	WorkspaceID  string `json:"workspace_id"`
	SessionToken string `json:"-"`
	CSRFToken    string `json:"csrf_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

type SessionIdentity struct {
	SessionID     string
	PrincipalID   string
	PrincipalType string
	DisplayName   string
	Username      string
	CSRFHash      string
	LastSeenAt    int64
	IdleExpiresAt *int64
	ExpiresAt     int64
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{db: db, tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk, sessionTTL: 12 * time.Hour, idleTTL: 30 * time.Minute}
}

func (s *Service) SetupStatus(ctx context.Context) (SetupStatus, error) {
	var st SetupStatus
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM principals WHERE principal_type='human' AND status='active'`).Scan(&st.HumanPrincipals); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_identities WHERE provider='local_password' AND issuer=? AND status='active'`, localIssuer).Scan(&st.ActiveLocalIdentities); err != nil {
		return st, err
	}
	st.Required = st.HumanPrincipals == 0 && st.ActiveLocalIdentities == 0
	return st, nil
}

func (s *Service) BootstrapAdmin(ctx context.Context, cmd BootstrapAdminCommand) (SessionResult, error) {
	username, err := normalizeUsername(cmd.Username)
	if err != nil {
		return SessionResult{}, err
	}
	display := strings.TrimSpace(cmd.DisplayName)
	if display == "" {
		display = username
	}
	if len(display) > 200 {
		return SessionResult{}, fmt.Errorf("display name too long")
	}
	workspaceName := strings.TrimSpace(cmd.WorkspaceName)
	if workspaceName == "" {
		workspaceName = "Default"
	}
	if len(workspaceName) > 200 {
		return SessionResult{}, fmt.Errorf("workspace name too long")
	}
	credential, err := hashPassword(cmd.Password)
	if err != nil {
		return SessionResult{}, err
	}
	principalID, _ := s.ids.New("principal")
	workspaceID, _ := s.ids.New("ws")
	identityID, _ := s.ids.New("auth")
	roleID, _ := s.ids.New("role")
	now := s.clock.UnixMilli()
	result, sessionMeta, err := s.newSession(principalID, display, username, workspaceID, now)
	if err != nil {
		return SessionResult{}, err
	}
	eventID, _ := s.ids.New("evt")

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var humans, identities int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM principals WHERE principal_type='human' AND status='active'`).Scan(&humans); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_identities WHERE status='active'`).Scan(&identities); err != nil {
			return err
		}
		if humans != 0 || identities != 0 {
			return ErrSetupComplete
		}
		var mode string
		var revision int64
		if err := tx.QueryRowContext(ctx, `SELECT mode,revision FROM system_state WHERE singleton_id=1`).Scan(&mode, &revision); err != nil {
			return err
		}
		if mode != "bootstrap" && mode != "commissioning" {
			return ErrSetupComplete
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES(?,?,'active',1,?,?)`, workspaceID, workspaceName, now, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES(?,'human',?,'active',1,?,?)`, principalID, display, now, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES(?,?,'active',?,?)`, workspaceID, principalID, now, now); err != nil {
			return err
		}
		var adminRoleID string
		scanErr := tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE name='Admin' AND role_class='human'`).Scan(&adminRoleID)
		if errors.Is(scanErr, sql.ErrNoRows) {
			adminRoleID = roleID
			if _, err := tx.ExecContext(ctx, `INSERT INTO roles(id,role_class,name,definition_json,revision,created_at,updated_at) VALUES(?,'human','Admin','{"capabilities":["*"],"workspace_scope":"member"}',1,?,?)`, adminRoleID, now, now); err != nil {
				return err
			}
		} else if scanErr != nil {
			return scanErr
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO principal_roles(principal_id,role_id,workspace_id,created_at) VALUES(?,?,?,?)`, principalID, adminRoleID, workspaceID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_identities(id,principal_id,provider,issuer,subject,credential_json,status,created_at,updated_at) VALUES(?,?,'local_password',?,?,?,'active',?,?)`, identityID, principalID, localIssuer, username, string(credential), now, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_sessions(id,principal_id,token_hash,auth_method,auth_strength,created_at,last_seen_at,idle_expires_at,expires_at,metadata_json) VALUES(?, ?, ?, 'local_password', 'password', ?, ?, ?, ?, ?)`, sessionMeta.ID, principalID, sessionMeta.TokenHash, now, now, sessionMeta.IdleExpiresAt, sessionMeta.ExpiresAt, sessionMeta.Metadata); err != nil {
			return err
		}
		if mode == "bootstrap" {
			res, err := tx.ExecContext(ctx, `UPDATE system_state SET mode='commissioning',revision=revision+1,reason='first admin created',updated_at=? WHERE singleton_id=1 AND revision=?`, now, revision)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrSetupComplete
			}
		}
		payload, _ := json.Marshal(map[string]any{"principal_id": principalID, "workspace_id": workspaceID, "username": username})
		return s.events.Append(ctx, tx, event.Event{ID: eventID, WorkspaceID: &workspaceID, Type: "auth.bootstrap_admin_created", AggregateType: "principal", AggregateID: principalID, ActorPrincipalID: &principalID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return SessionResult{}, err
	}
	return result, nil
}

type sessionRecord struct {
	ID, TokenHash, Metadata  string
	IdleExpiresAt, ExpiresAt int64
}

func (s *Service) newSession(principalID, display, username, workspaceID string, now int64) (SessionResult, sessionRecord, error) {
	sid, _ := s.ids.New("session")
	token, err := randomToken(32)
	if err != nil {
		return SessionResult{}, sessionRecord{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return SessionResult{}, sessionRecord{}, err
	}
	tokenHash := hashToken(token)
	csrfHash := hashToken(csrf)
	idle := now + s.idleTTL.Milliseconds()
	exp := now + s.sessionTTL.Milliseconds()
	meta, _ := json.Marshal(map[string]any{"csrf_hash": csrfHash, "username": username})
	return SessionResult{PrincipalID: principalID, DisplayName: display, Username: username, WorkspaceID: workspaceID, SessionToken: token, CSRFToken: csrf, ExpiresAt: exp}, sessionRecord{ID: sid, TokenHash: tokenHash, Metadata: string(meta), IdleExpiresAt: idle, ExpiresAt: exp}, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (SessionResult, error) {
	normalized, err := normalizeUsername(username)
	if err != nil {
		return SessionResult{}, ErrInvalidCredentials
	}
	var principalID, display, credentialRaw string
	err = s.db.QueryRowContext(ctx, `SELECT p.id,p.display_name,a.credential_json FROM auth_identities a JOIN principals p ON p.id=a.principal_id WHERE a.provider='local_password' AND a.issuer=? AND a.subject=? AND a.status='active' AND p.status='active'`, localIssuer, normalized).Scan(&principalID, &display, &credentialRaw)
	if err != nil {
		return SessionResult{}, ErrInvalidCredentials
	}
	if !verifyPassword(json.RawMessage(credentialRaw), password) {
		return SessionResult{}, ErrInvalidCredentials
	}
	var workspaceID string
	if err := s.db.QueryRowContext(ctx, `SELECT workspace_id FROM workspace_memberships WHERE principal_id=? AND status='active' ORDER BY created_at LIMIT 1`, principalID).Scan(&workspaceID); err != nil {
		return SessionResult{}, ErrInvalidCredentials
	}
	now := s.clock.UnixMilli()
	result, rec, err := s.newSession(principalID, display, normalized, workspaceID, now)
	if err != nil {
		return SessionResult{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO auth_sessions(id,principal_id,token_hash,auth_method,auth_strength,created_at,last_seen_at,idle_expires_at,expires_at,metadata_json) VALUES(?, ?, ?, 'local_password', 'password', ?, ?, ?, ?, ?)`, rec.ID, principalID, rec.TokenHash, now, now, rec.IdleExpiresAt, rec.ExpiresAt, rec.Metadata); err != nil {
		return SessionResult{}, err
	}
	return result, nil
}

func (s *Service) AuthenticateSession(ctx context.Context, token, csrf string, requireCSRF bool) (SessionIdentity, error) {
	if strings.TrimSpace(token) == "" {
		return SessionIdentity{}, ErrInvalidSession
	}
	var out SessionIdentity
	var meta string
	var idle sql.NullInt64
	var revoked sql.NullInt64
	var pstatus string
	err := s.db.QueryRowContext(ctx, `SELECT s.id,s.principal_id,p.principal_type,p.display_name,s.last_seen_at,s.idle_expires_at,s.expires_at,s.revoked_at,s.metadata_json,p.status FROM auth_sessions s JOIN principals p ON p.id=s.principal_id WHERE s.token_hash=?`, hashToken(token)).Scan(&out.SessionID, &out.PrincipalID, &out.PrincipalType, &out.DisplayName, &out.LastSeenAt, &idle, &out.ExpiresAt, &revoked, &meta, &pstatus)
	if err != nil || pstatus != "active" || revoked.Valid {
		return SessionIdentity{}, ErrInvalidSession
	}
	now := s.clock.UnixMilli()
	if now >= out.ExpiresAt || (idle.Valid && now >= idle.Int64) {
		return SessionIdentity{}, ErrInvalidSession
	}
	if idle.Valid {
		v := idle.Int64
		out.IdleExpiresAt = &v
	}
	var m map[string]any
	if json.Unmarshal([]byte(meta), &m) != nil {
		return SessionIdentity{}, ErrInvalidSession
	}
	out.Username, _ = m["username"].(string)
	out.CSRFHash, _ = m["csrf_hash"].(string)
	if requireCSRF {
		got := hashToken(csrf)
		if out.CSRFHash == "" || subtle.ConstantTimeCompare([]byte(got), []byte(out.CSRFHash)) != 1 {
			return SessionIdentity{}, ErrCSRF
		}
	}
	newIdle := now + s.idleTTL.Milliseconds()
	if newIdle > out.ExpiresAt {
		newIdle = out.ExpiresAt
	}
	if now-out.LastSeenAt >= 60000 {
		_, _ = s.db.ExecContext(ctx, `UPDATE auth_sessions SET last_seen_at=?,idle_expires_at=? WHERE id=? AND revoked_at IS NULL`, now, newIdle, out.SessionID)
	}
	return out, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, s.clock.UnixMilli(), hashToken(token))
	return err
}

func (s *Service) WorkspaceScopes(ctx context.Context, principalID string) ([]string, []string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT wm.workspace_id,r.name FROM workspace_memberships wm LEFT JOIN principal_roles pr ON pr.principal_id=wm.principal_id AND (pr.workspace_id=wm.workspace_id OR pr.workspace_id IS NULL) LEFT JOIN roles r ON r.id=pr.role_id WHERE wm.principal_id=? AND wm.status='active'`, principalID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	wsSet := map[string]bool{}
	admin := false
	approver := false
	for rows.Next() {
		var ws string
		var role sql.NullString
		if err := rows.Scan(&ws, &role); err != nil {
			return nil, nil, err
		}
		wsSet[ws] = true
		if role.Valid {
			switch {
			case strings.EqualFold(role.String, "Admin"):
				admin = true
			case strings.EqualFold(role.String, "Approver"):
				approver = true
			}
		}
	}
	var workspaces []string
	for w := range wsSet {
		workspaces = append(workspaces, w)
	}
	caps := []string{"project.read", "events.read"}
	if approver {
		caps = append(caps, "verification.accept")
	}
	if admin {
		caps = []string{"*"}
	}
	return workspaces, caps, nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func hashToken(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
