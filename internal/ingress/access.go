package ingress

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
)

var ErrPreviewForbidden = errors.New("preview access forbidden")

type AccessChecker interface {
	CheckPreviewAccess(context.Context, string, string, string) error
}

type DBAccessChecker struct {
	db    *sql.DB
	clock clock.Clock
}

func NewDBAccessChecker(db *sql.DB, clk clock.Clock) *DBAccessChecker {
	return &DBAccessChecker{db: db, clock: clk}
}

func (a *DBAccessChecker) CheckPreviewAccess(ctx context.Context, principalID, credentialID, workspaceID string) error {
	if a == nil || a.db == nil || principalID == "" || credentialID == "" || workspaceID == "" {
		return ErrPreviewForbidden
	}
	var pstatus, ptype, wstatus, membership, credentialPrincipal, workspaceScope, capabilityScope string
	var expires, revoked sql.NullInt64
	err := a.db.QueryRowContext(ctx, `
SELECT p.status,p.principal_type,w.status,wm.status,c.principal_id,c.workspace_scope_json,c.capability_scope_json,c.expires_at,c.revoked_at
FROM principals p
JOIN workspaces w ON w.id=?
JOIN workspace_memberships wm ON wm.workspace_id=w.id AND wm.principal_id=p.id
JOIN api_credentials c ON c.id=?
WHERE p.id=?`, workspaceID, credentialID, principalID).Scan(&pstatus, &ptype, &wstatus, &membership, &credentialPrincipal, &workspaceScope, &capabilityScope, &expires, &revoked)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPreviewForbidden
		}
		return err
	}
	now := a.clock.UnixMilli()
	if credentialPrincipal != principalID || pstatus != "active" || wstatus != "active" || membership != "active" || revoked.Valid || (expires.Valid && now >= expires.Int64) {
		return ErrPreviewForbidden
	}
	// Preview grants never outlive the credential scopes that authorized them.
	if !scopeAllows(json.RawMessage(workspaceScope), "workspaces", workspaceID) || !scopeAllows(json.RawMessage(capabilityScope), "capabilities", "project.read") {
		return ErrPreviewForbidden
	}
	_ = ptype // retained for future human/web-session specialization
	return nil
}

func scopeAllows(raw json.RawMessage, objectKey, want string) bool {
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		return stringListAllows(arr, objectKey, want)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return false
	}
	v, ok := obj[objectKey]
	if !ok || json.Unmarshal(v, &arr) != nil {
		return false
	}
	return stringListAllows(arr, objectKey, want)
}

func stringListAllows(arr []string, objectKey, want string) bool {
	for _, x := range arr {
		if x == "*" || x == want || (objectKey == "capabilities" && strings.HasSuffix(x, ".*") && strings.HasPrefix(want, strings.TrimSuffix(x, "*"))) {
			return true
		}
	}
	return false
}
