package event

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type Event struct {
	ID               string          `json:"id"`
	WorkspaceID      *string         `json:"workspace_id,omitempty"`
	Type             string          `json:"event_type"`
	AggregateType    string          `json:"aggregate_type"`
	AggregateID      string          `json:"aggregate_id"`
	ActorPrincipalID *string         `json:"actor_principal_id,omitempty"`
	RequestID        *string         `json:"request_id,omitempty"`
	TraceID          *string         `json:"trace_id,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	OccurredAt       int64           `json:"occurred_at"`
}

type Stored struct {
	Sequence int64 `json:"sequence"`
	Event
}

type Store struct{}

func (Store) Append(ctx context.Context, tx storage.Tx, e Event) error {
	if e.ID == "" || e.Type == "" || e.AggregateType == "" || e.AggregateID == "" {
		return fmt.Errorf("event identity/type/aggregate fields are required")
	}
	if len(e.Payload) == 0 || !json.Valid(e.Payload) {
		return fmt.Errorf("event payload must be valid JSON")
	}
	if e.OccurredAt <= 0 {
		return fmt.Errorf("event occurred_at must be set")
	}

	_, err := tx.ExecContext(ctx, `
INSERT INTO events(
    id, workspace_id, event_type, aggregate_type, aggregate_id,
    actor_principal_id, request_id, trace_id, payload_json, occurred_at
) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.WorkspaceID, e.Type, e.AggregateType, e.AggregateID,
		e.ActorPrincipalID, e.RequestID, e.TraceID, string(e.Payload), e.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf("append event %s: %w", e.Type, err)
	}
	return nil
}
