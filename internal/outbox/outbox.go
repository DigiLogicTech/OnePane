package outbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type Job struct {
	ID          string
	WorkspaceID *string
	Type        string
	Payload     json.RawMessage
	AvailableAt int64
	MaxAttempts int
	CreatedAt   int64
}

type Store struct{}

func (Store) Enqueue(ctx context.Context, tx storage.Tx, j Job) error {
	if j.ID == "" || j.Type == "" {
		return fmt.Errorf("outbox id and type are required")
	}
	if len(j.Payload) == 0 || !json.Valid(j.Payload) {
		return fmt.Errorf("outbox payload must be valid JSON")
	}
	if j.AvailableAt <= 0 || j.CreatedAt <= 0 {
		return fmt.Errorf("outbox timestamps must be set")
	}
	if j.MaxAttempts <= 0 {
		j.MaxAttempts = 10
	}

	_, err := tx.ExecContext(ctx, `
INSERT INTO outbox_jobs(
    id, workspace_id, job_type, payload_json, status,
    available_at, attempts, max_attempts, created_at, updated_at
) VALUES(?,?,?,?, 'pending', ?,0,?,?,?)`,
		j.ID, j.WorkspaceID, j.Type, string(j.Payload),
		j.AvailableAt, j.MaxAttempts, j.CreatedAt, j.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("enqueue outbox job %s: %w", j.Type, err)
	}
	return nil
}
