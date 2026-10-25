package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// WorkspaceTransactionAuthorizer revalidates the original credential using only
// the caller's transaction. It neither refreshes sessions nor starts a transaction.
type WorkspaceTransactionAuthorizer interface {
	AuthorizeWorkspaceInTransaction(context.Context, storage.Tx, *http.Request, Identity, string, ...string) error
}

func transactionAuthHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func transactionAuthInputs(tx storage.Tx, r *http.Request, original Identity, workspace string, capabilities []string) bool {
	if tx == nil || r == nil || len(capabilities) == 0 {
		return false
	}
	for _, value := range append([]string{original.PrincipalID, original.PrincipalType, original.CredentialID, workspace}, capabilities...) {
		if strings.TrimSpace(value) == "" || len(value) > 256 || !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func transactionAuthError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthenticated
	}
	return err
}

func transactionActiveMembership(ctx context.Context, tx storage.Tx, principal, workspace string) error {
	var active bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_memberships m JOIN workspaces w ON w.id=m.workspace_id WHERE m.principal_id=? AND m.workspace_id=? AND m.status='active' AND w.status='active')`, principal, workspace).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return ErrForbidden
	}
	return nil
}

func (a *BearerAuthorizer) AuthorizeWorkspaceInTransaction(ctx context.Context, tx storage.Tx, r *http.Request, original Identity, workspace string, capabilities ...string) error {
	if a == nil || a.clock == nil || !transactionAuthInputs(tx, r, original, workspace, capabilities) || original.AuthMethod != "bearer" {
		return ErrUnauthenticated
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") || len(header) > 16384 {
		return ErrUnauthenticated
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" {
		return ErrUnauthenticated
	}
	var ws, caps string
	// Bound JSON before driver materialization. Identity comparisons happen in
	// SQL so malformed/replaced principals never return private metadata.
	err := tx.QueryRowContext(ctx, `SELECT
 CASE WHEN length(CAST(c.workspace_scope_json AS BLOB))<=16384 THEN c.workspace_scope_json ELSE '' END,
 CASE WHEN length(CAST(c.capability_scope_json AS BLOB))<=16384 THEN c.capability_scope_json ELSE '' END
 FROM api_credentials c JOIN principals p ON p.id=c.principal_id
 WHERE c.id=? AND c.credential_hash=? AND c.principal_id=? AND p.principal_type=? AND p.status='active'
 AND c.revoked_at IS NULL AND (c.expires_at IS NULL OR c.expires_at>?)`, original.CredentialID, transactionAuthHash(token), original.PrincipalID, original.PrincipalType, a.clock.UnixMilli()).Scan(&ws, &caps)
	if err != nil {
		return transactionAuthError(err)
	}
	if !utf8.ValidString(ws) || !utf8.ValidString(caps) || !json.Valid([]byte(ws)) || !json.Valid([]byte(caps)) {
		return ErrForbidden
	}
	if !scopeAllows(json.RawMessage(ws), "workspaces", workspace) {
		return ErrForbidden
	}
	for _, capability := range capabilities {
		if !scopeAllows(json.RawMessage(caps), "capabilities", capability) {
			return ErrForbidden
		}
	}
	return transactionActiveMembership(ctx, tx, original.PrincipalID, workspace)
}

func (a *HybridAuthorizer) AuthorizeWorkspaceInTransaction(ctx context.Context, tx storage.Tx, r *http.Request, original Identity, workspace string, capabilities ...string) error {
	if a == nil || a.bearer == nil || a.bearer.clock == nil || !transactionAuthInputs(tx, r, original, workspace, capabilities) {
		return ErrUnauthenticated
	}
	if strings.HasPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ") {
		return a.bearer.AuthorizeWorkspaceInTransaction(ctx, tx, r, original, workspace, capabilities...)
	}
	if original.AuthMethod != "session" {
		return ErrUnauthenticated
	}
	cookie, err := r.Cookie(WebSessionCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" || len(cookie.Value) > 16384 {
		return ErrUnauthenticated
	}
	var metadata string
	now := a.bearer.clock.UnixMilli()
	err = tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(s.metadata_json AS BLOB))<=16384 THEN s.metadata_json ELSE '' END
 FROM auth_sessions s JOIN principals p ON p.id=s.principal_id
 WHERE s.id=? AND s.token_hash=? AND s.principal_id=? AND p.principal_type=? AND p.status='active'
 AND s.revoked_at IS NULL AND s.expires_at>? AND (s.idle_expires_at IS NULL OR s.idle_expires_at>?)`, original.CredentialID, transactionAuthHash(cookie.Value), original.PrincipalID, original.PrincipalType, now, now).Scan(&metadata)
	if err != nil {
		return transactionAuthError(err)
	}
	var meta map[string]json.RawMessage
	if !utf8.ValidString(metadata) || json.Unmarshal([]byte(metadata), &meta) != nil || meta == nil {
		return ErrUnauthenticated
	}
	unsafe := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
	if unsafe {
		var csrfHash string
		csrf := r.Header.Get("X-OnePane-CSRF")
		if len(csrf) > 16384 || json.Unmarshal(meta["csrf_hash"], &csrfHash) != nil || len(csrfHash) != 64 || subtle.ConstantTimeCompare([]byte(transactionAuthHash(csrf)), []byte(csrfHash)) != 1 {
			return ErrForbidden
		}
	}
	if err = transactionActiveMembership(ctx, tx, original.PrincipalID, workspace); err != nil {
		return err
	}
	// WorkspaceScopes intentionally combines roles over ALL active memberships.
	// Preserve that behavior without collecting an unbounded roster into memory.
	var admin, approver bool
	err = tx.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM workspace_memberships m JOIN principal_roles pr ON pr.principal_id=m.principal_id AND (pr.workspace_id=m.workspace_id OR pr.workspace_id IS NULL) JOIN roles role ON role.id=pr.role_id WHERE m.principal_id=? AND m.status='active' AND role.name='Admin' COLLATE NOCASE),
 EXISTS(SELECT 1 FROM workspace_memberships m JOIN principal_roles pr ON pr.principal_id=m.principal_id AND (pr.workspace_id=m.workspace_id OR pr.workspace_id IS NULL) JOIN roles role ON role.id=pr.role_id WHERE m.principal_id=? AND m.status='active' AND role.name='Approver' COLLATE NOCASE)`, original.PrincipalID, original.PrincipalID).Scan(&admin, &approver)
	if err != nil {
		return err
	}
	for _, capability := range capabilities {
		if !admin && capability != "project.read" && capability != "events.read" && !(approver && capability == "verification.accept") {
			return ErrForbidden
		}
	}
	return nil
}
