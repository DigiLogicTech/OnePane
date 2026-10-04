package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
)

type Identity struct {
	PrincipalID     string
	PrincipalType   string
	CredentialID    string
	WorkspaceScope  json.RawMessage
	CapabilityScope json.RawMessage
	AuthMethod      string
}

type Authorizer interface {
	Authenticate(*http.Request) (Identity, error)
	AuthorizeWorkspace(context.Context, Identity, string, string) error
}

type BearerAuthorizer struct {
	db    *sql.DB
	clock clock.Clock
}

func NewBearerAuthorizer(db *sql.DB, clk clock.Clock) *BearerAuthorizer {
	return &BearerAuthorizer{db: db, clock: clk}
}

func (a *BearerAuthorizer) Authenticate(r *http.Request) (Identity, error) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(h, "Bearer ") {
		return Identity{}, ErrUnauthenticated
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	if token == "" {
		return Identity{}, ErrUnauthenticated
	}
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	var i Identity
	var ws, cap string
	var expires, revoked sql.NullInt64
	var pstatus string
	err := a.db.QueryRowContext(r.Context(), `SELECT c.id,c.principal_id,p.principal_type,c.workspace_scope_json,c.capability_scope_json,c.expires_at,c.revoked_at,p.status FROM api_credentials c JOIN principals p ON p.id=c.principal_id WHERE c.credential_hash=?`, hash).Scan(&i.CredentialID, &i.PrincipalID, &i.PrincipalType, &ws, &cap, &expires, &revoked, &pstatus)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Identity{}, ErrUnauthenticated
		}
		return Identity{}, err
	}
	now := a.clock.UnixMilli()
	if pstatus != "active" || revoked.Valid || (expires.Valid && now >= expires.Int64) {
		return Identity{}, ErrUnauthenticated
	}
	i.WorkspaceScope = json.RawMessage(ws)
	i.CapabilityScope = json.RawMessage(cap)
	return i, nil
}

func (a *BearerAuthorizer) AuthorizeWorkspace(ctx context.Context, i Identity, workspaceID, capability string) error {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(capability) == "" {
		return ErrForbidden
	}
	if !scopeAllows(i.WorkspaceScope, "workspaces", workspaceID) || !scopeAllows(i.CapabilityScope, "capabilities", capability) {
		return ErrForbidden
	}
	switch i.PrincipalType {
	case "system", "recovery", "watchdog":
		return nil
	}
	var status string
	err := a.db.QueryRowContext(ctx, `SELECT status FROM workspace_memberships WHERE workspace_id=? AND principal_id=?`, workspaceID, i.PrincipalID).Scan(&status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return err
	}
	if status != "active" {
		return ErrForbidden
	}
	return nil
}

func scopeAllows(raw json.RawMessage, objectKey, want string) bool {
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		for _, v := range arr {
			if v == "*" || v == want || (objectKey == "capabilities" && strings.HasSuffix(v, ".*") && strings.HasPrefix(want, strings.TrimSuffix(v, "*"))) {
				return true
			}
		}
		return false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return false
	}
	v, ok := obj[objectKey]
	if !ok {
		return false
	}
	if json.Unmarshal(v, &arr) != nil {
		return false
	}
	for _, x := range arr {
		if x == "*" || x == want || (objectKey == "capabilities" && strings.HasSuffix(x, ".*") && strings.HasPrefix(want, strings.TrimSuffix(x, "*"))) {
			return true
		}
	}
	return false
}

func (i Identity) String() string { return fmt.Sprintf("%s/%s", i.PrincipalType, i.PrincipalID) }
