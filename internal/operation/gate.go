package operation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/tool"
)

type permitRecord struct {
	Hash        [32]byte
	OperationID string
	ExpiresAt   time.Time
}

// MutationGate owns ephemeral ExecutionPermits. No permit is durable: process
// loss therefore forces the recovery/reconciliation path before a mutation can
// be dispatched again.
type MutationGate struct {
	db      *sql.DB
	repo    repository
	mu      sync.Mutex
	permits map[string]permitRecord
	ttl     time.Duration
}

func NewMutationGate(db *sql.DB) *MutationGate {
	return &MutationGate{db: db, repo: newSQLRepository(db), permits: map[string]permitRecord{}, ttl: 30 * time.Second}
}

func (g *MutationGate) Mint(ctx context.Context, operationID string) (string, error) {
	if g == nil || g.db == nil || strings.TrimSpace(operationID) == "" {
		return "", ErrExecutionPermit
	}
	o, err := g.repo.Get(ctx, operationID)
	if err != nil {
		return "", err
	}
	if o.State != StateExecuting || o.ResourceLeaseID == nil || o.CapabilityLeaseID == nil {
		return "", ErrExecutionPermit
	}
	var status, workspaceID, resourceRef string
	var expiresAt int64
	err = g.db.QueryRowContext(ctx, `SELECT status,workspace_id,resource_ref,expires_at FROM resource_leases WHERE id=?`, *o.ResourceLeaseID).Scan(&status, &workspaceID, &resourceRef, &expiresAt)
	if err != nil {
		return "", fmt.Errorf("resolve resource lease for permit: %w", err)
	}
	if status != "active" || workspaceID != o.WorkspaceID || resourceRef != o.ResourceRef || time.Now().UTC().UnixMilli() >= expiresAt {
		return "", ErrResourceLeaseInvalid
	}
	unknown, err := g.repo.HasUnknownOutcomeForResource(ctx, o.WorkspaceID, o.ResourceRef, o.ID)
	if err != nil {
		return "", err
	}
	if unknown {
		return "", ErrUnknownOutcome
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("mint execution permit: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	rec := permitRecord{Hash: sha256.Sum256([]byte(token)), OperationID: o.ID, ExpiresAt: time.Now().UTC().Add(g.ttl)}
	g.mu.Lock()
	g.permits[o.ID] = rec
	g.mu.Unlock()
	return token, nil
}

func (g *MutationGate) ValidateAndConsume(ctx context.Context, c tool.MutationPermitCheck) error {
	if g == nil || strings.TrimSpace(c.OperationID) == "" || strings.TrimSpace(c.Permit) == "" {
		return ErrExecutionPermit
	}
	g.mu.Lock()
	rec, ok := g.permits[c.OperationID]
	// Single use regardless of success; a failed pre-dispatch validation must be
	// explicitly retried through the coordinator, never by reusing a token.
	if ok {
		delete(g.permits, c.OperationID)
	}
	g.mu.Unlock()
	if !ok || rec.OperationID != c.OperationID || time.Now().UTC().After(rec.ExpiresAt) {
		return ErrExecutionPermit
	}
	got := sha256.Sum256([]byte(c.Permit))
	if subtle.ConstantTimeCompare(got[:], rec.Hash[:]) != 1 {
		return ErrExecutionPermit
	}

	o, err := g.repo.Get(ctx, c.OperationID)
	if err != nil {
		return err
	}
	if o.State != StateExecuting || o.WorkspaceID != c.WorkspaceID || o.PrincipalID != c.PrincipalID ||
		o.ToolID != c.ToolID || o.ToolVersion != c.ToolVersion || o.AdapterID != c.AdapterID ||
		o.AdapterVersion != c.AdapterVersion || o.ResourceRef != c.ResourceRef || o.InputHash != c.InputHash {
		return ErrExecutionPermit
	}
	if !sameOptionalString(o.TaskID, c.TaskID) || !sameOptionalString(o.AttemptID, c.AttemptID) ||
		o.CapabilityLeaseID == nil || *o.CapabilityLeaseID != c.LeaseID || o.ResourceLeaseID == nil {
		return ErrExecutionPermit
	}

	var status, workspaceID, resourceRef string
	var expiresAt int64
	if err := g.db.QueryRowContext(ctx, `SELECT status,workspace_id,resource_ref,expires_at FROM resource_leases WHERE id=?`, *o.ResourceLeaseID).Scan(&status, &workspaceID, &resourceRef, &expiresAt); err != nil {
		return ErrExecutionPermit
	}
	if status != "active" || workspaceID != o.WorkspaceID || resourceRef != o.ResourceRef || time.Now().UTC().UnixMilli() >= expiresAt {
		return ErrResourceLeaseInvalid
	}
	unknown, err := g.repo.HasUnknownOutcomeForResource(ctx, o.WorkspaceID, o.ResourceRef, o.ID)
	if err != nil {
		return err
	}
	if unknown {
		return ErrUnknownOutcome
	}
	return nil
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
