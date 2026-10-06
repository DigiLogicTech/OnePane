package resourcecoord

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type Owner struct {
	WorkspaceID string  `json:"workspace_id,omitempty"`
	ProjectID   *string `json:"project_id,omitempty"`
	TaskID      *string `json:"task_id,omitempty"`
	ExecutionID *string `json:"execution_id,omitempty"`
	AgentID     *string `json:"agent_id,omitempty"`
	NodeID      *string `json:"node_id,omitempty"`
}

type Lease struct {
	ID                string `json:"id"`
	ResourceUID       string `json:"resource_uid"`
	CanonicalIdentity string `json:"canonical_identity"`
	Mode              string `json:"mode"`
	Status            string `json:"status"`
	ExpiresAt         int64  `json:"expires_at"`
}

type Service struct {
	db    *sql.DB
	tx    storage.Transactor
	clock clock.Clock
	ids   id.Generator
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{db: db, tx: tx, clock: clk, ids: id.Generator{}}
}

// CanonicalIdentity maps obvious filesystem references onto one host-level
// identity so two Projects that reference the same underlying path contend on
// the same resource. Opaque logical resource references are retained verbatim.
func CanonicalIdentity(resourceRef string) (kind, identity string, err error) {
	resourceRef = strings.TrimSpace(resourceRef)
	if resourceRef == "" {
		return "", "", errors.New("resource reference required")
	}
	kind = "logical"
	path := ""
	if u, parseErr := url.Parse(resourceRef); parseErr == nil && strings.EqualFold(u.Scheme, "file") {
		kind = "file"
		path = u.Path
		if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
	} else if filepath.IsAbs(resourceRef) {
		kind = "file"
		path = resourceRef
	}
	if path == "" {
		return kind, resourceRef, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	abs = filepath.Clean(abs)
	if resolved, e := filepath.EvalSymlinks(abs); e == nil {
		abs = filepath.Clean(resolved)
	} else if !errors.Is(e, os.ErrNotExist) {
		return "", "", e
	}
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return kind, "file:" + filepath.ToSlash(abs), nil
}

func uidFor(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return "res_" + hex.EncodeToString(sum[:16])
}

// AcquireWrite waits for the canonical resource to become writable. The queue
// is global: workspace/project identifiers are metadata, never part of the lock
// key. Expired leases are reclaimed before every acquisition attempt.
func (s *Service) AcquireWrite(ctx context.Context, resourceRef string, owner Owner, ttl time.Duration) (Lease, error) {
	var out Lease
	if s == nil || s.db == nil || s.tx == nil || s.clock == nil {
		return out, errors.New("global resource coordinator unavailable")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if ttl < 10*time.Second || ttl > time.Hour {
		return out, errors.New("global resource lease ttl must be 10s..1h")
	}
	kind, identity, err := CanonicalIdentity(resourceRef)
	if err != nil {
		return out, err
	}
	uid := uidFor(identity)
	leaseID, err := s.ids.New("grlease")
	if err != nil {
		return out, err
	}
	ownerJSON, _ := json.Marshal(owner)
	now := s.clock.UnixMilli()
	location, _ := json.Marshal(map[string]any{"resource_ref": resourceRef})
	if _, err := s.db.ExecContext(ctx, `INSERT INTO global_resource_identities(resource_uid,kind,canonical_identity,location_json,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(resource_uid) DO UPDATE SET location_json=excluded.location_json,updated_at=excluded.updated_at`, uid, kind, identity, string(location), now); err != nil {
		return out, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO global_resource_leases(id,resource_uid,lease_mode,status,owner_json,requested_at,revision) VALUES(?,?,'write','queued',?,?,1)`, leaseID, uid, string(ownerJSON), now); err != nil {
		return out, err
	}

	ticker := time.NewTicker(125 * time.Millisecond)
	defer ticker.Stop()
	for {
		acquired := false
		now = s.clock.UnixMilli()
		err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			_, _ = tx.ExecContext(ctx, `UPDATE global_resource_leases SET status='expired',revision=revision+1 WHERE resource_uid=? AND status='active' AND expires_at IS NOT NULL AND expires_at<=?`, uid, now)
			var active int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM global_resource_leases WHERE resource_uid=? AND status='active'`, uid).Scan(&active); err != nil {
				return err
			}
			if active != 0 {
				return nil
			}
			var ahead int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM global_resource_leases WHERE resource_uid=? AND status='queued' AND (requested_at < ? OR (requested_at=? AND id < ?))`, uid, now, now, leaseID).Scan(&ahead); err != nil {
				return err
			}
			// requested_at is normally unique enough, but deterministic ID ordering
			// provides FIFO behavior for ties.
			if ahead != 0 {
				var myRequested int64
				if err := tx.QueryRowContext(ctx, `SELECT requested_at FROM global_resource_leases WHERE id=?`, leaseID).Scan(&myRequested); err != nil {
					return err
				}
				if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM global_resource_leases WHERE resource_uid=? AND status='queued' AND (requested_at < ? OR (requested_at=? AND id < ?))`, uid, myRequested, myRequested, leaseID).Scan(&ahead); err != nil {
					return err
				}
				if ahead != 0 {
					return nil
				}
			}
			expires := now + ttl.Milliseconds()
			res, err := tx.ExecContext(ctx, `UPDATE global_resource_leases SET status='active',acquired_at=?,heartbeat_at=?,expires_at=?,revision=revision+1 WHERE id=? AND status='queued'`, now, now, expires, leaseID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				acquired = true
				out = Lease{ID: leaseID, ResourceUID: uid, CanonicalIdentity: identity, Mode: "write", Status: "active", ExpiresAt: expires}
			}
			return nil
		})
		if err != nil {
			_ = s.Cancel(context.Background(), leaseID)
			return Lease{}, err
		}
		if acquired {
			return out, nil
		}
		select {
		case <-ctx.Done():
			_ = s.Cancel(context.Background(), leaseID)
			return Lease{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) Heartbeat(ctx context.Context, leaseID string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	now := s.clock.UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE global_resource_leases SET heartbeat_at=?,expires_at=?,revision=revision+1 WHERE id=? AND status='active'`, now, now+ttl.Milliseconds(), strings.TrimSpace(leaseID))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Service) Release(ctx context.Context, leaseID string) error {
	now := s.clock.UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE global_resource_leases SET status='released',released_at=?,revision=revision+1 WHERE id=? AND status IN ('active','queued')`, now, strings.TrimSpace(leaseID))
	return err
}

func (s *Service) Cancel(ctx context.Context, leaseID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE global_resource_leases SET status='cancelled',revision=revision+1 WHERE id=? AND status='queued'`, strings.TrimSpace(leaseID))
	return err
}

func (s *Service) Snapshot(ctx context.Context, limit int) ([]Lease, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT l.id,l.resource_uid,i.canonical_identity,l.lease_mode,l.status,COALESCE(l.expires_at,0) FROM global_resource_leases l JOIN global_resource_identities i ON i.resource_uid=l.resource_uid WHERE l.status IN ('queued','active') ORDER BY l.requested_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Lease{}
	for rows.Next() {
		var x Lease
		if err := rows.Scan(&x.ID, &x.ResourceUID, &x.CanonicalIdentity, &x.Mode, &x.Status, &x.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list global resource leases: %w", err)
	}
	return out, nil
}
