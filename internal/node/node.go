package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type Node struct {
	ID                  string
	Name                string
	Local               bool
	IdentityFingerprint string
	TrustState          string
	TrustZone           *string
	Revision            int64
	CreatedAt           int64
	UpdatedAt           int64
}

type Service struct {
	db     *sql.DB
	tx     storage.Transactor
	events event.Store
	ids    id.Generator
	clock  clock.Clock
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{db: db, tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk}
}

func (s *Service) Local(ctx context.Context) (Node, error) {
	var n Node
	var local int
	var trustZone sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id,name,local,identity_fingerprint,trust_state,trust_zone,revision,created_at,updated_at
FROM harness_nodes WHERE local = 1`).Scan(
		&n.ID, &n.Name, &local, &n.IdentityFingerprint, &n.TrustState, &trustZone,
		&n.Revision, &n.CreatedAt, &n.UpdatedAt,
	)
	if err != nil {
		return Node{}, err
	}
	n.Local = local == 1
	if trustZone.Valid {
		n.TrustZone = &trustZone.String
	}
	return n, nil
}

func (s *Service) EnsureLocal(ctx context.Context, name, fingerprint string) (Node, error) {
	if n, err := s.Local(ctx); err == nil {
		if n.IdentityFingerprint != fingerprint {
			return Node{}, fmt.Errorf("local node identity mismatch: database=%s host=%s", n.IdentityFingerprint, fingerprint)
		}
		if strings.TrimSpace(name) != "" && (n.Name == "local" || strings.TrimSpace(n.Name) == "") && n.Name != name {
			now := s.clock.UnixMilli()
			if _, err := s.db.ExecContext(ctx, `UPDATE harness_nodes SET name=?,revision=revision+1,updated_at=? WHERE id=?`, name, now, n.ID); err != nil {
				return Node{}, fmt.Errorf("update local node name: %w", err)
			}
			return s.Local(ctx)
		}
		return n, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Node{}, fmt.Errorf("read local node: %w", err)
	}

	if name == "" || fingerprint == "" {
		return Node{}, fmt.Errorf("local node name and fingerprint are required")
	}
	nodeID, err := s.ids.New("node")
	if err != nil {
		return Node{}, err
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Node{}, err
	}
	now := s.clock.UnixMilli()

	protocol, _ := json.Marshal(map[string]any{"control_plane": "0.1", "inference_mesh": nil})
	caps, _ := json.Marshal(map[string]any{"inference_provider": false, "inference_consumer": true})

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO harness_nodes(
    id,name,local,identity_fingerprint,trust_state,trust_zone,
    endpoint_json,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at
) VALUES(?,?,1,?,'local','LOCAL_TRUSTED',NULL,?,?,?,1,?,?)`,
			nodeID, name, fingerprint, string(protocol), string(caps), now, now, now,
		)
		if err != nil {
			return fmt.Errorf("create local harness node: %w", err)
		}
		payload, _ := json.Marshal(map[string]any{
			"node_id":              nodeID,
			"name":                 name,
			"identity_fingerprint": fingerprint,
		})
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, Type: "node.local_registered", AggregateType: "harness_node", AggregateID: nodeID,
			Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		return Node{}, err
	}
	return s.Local(ctx)
}
