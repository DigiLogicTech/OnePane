package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/outbox"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type Mode string

const (
	ModeUninitialized Mode = "uninitialized"
	ModeBootstrap     Mode = "bootstrap"
	ModeCommissioning Mode = "commissioning"
	ModeRecovery      Mode = "recovery"
	ModeNormal        Mode = "normal"
	ModeSafe          Mode = "safe"
	ModeReadOnly      Mode = "read_only"
	ModeMaintenance   Mode = "maintenance"
	ModeDegraded      Mode = "degraded"
)

type State struct {
	Mode      Mode
	Revision  int64
	Reason    *string
	UpdatedAt int64
}

type Service struct {
	db     *sql.DB
	tx     storage.Transactor
	events event.Store
	outbox outbox.Store
	ids    id.Generator
	clock  clock.Clock
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{db: db, tx: tx, events: event.Store{}, outbox: outbox.Store{}, ids: id.Generator{}, clock: clk}
}

func (s *Service) Get(ctx context.Context) (State, error) {
	var st State
	var reason sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT mode, revision, reason, updated_at FROM system_state WHERE singleton_id = 1`,
	).Scan(&st.Mode, &st.Revision, &reason, &st.UpdatedAt)
	if err != nil {
		return State{}, err
	}
	if reason.Valid {
		st.Reason = &reason.String
	}
	return st, nil
}

// EnsureBootstrap creates the first SystemState and its causal Event/Outbox
// record atomically. Re-running it is safe.
func (s *Service) EnsureBootstrap(ctx context.Context) (State, error) {
	if st, err := s.Get(ctx); err == nil {
		return st, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return State{}, fmt.Errorf("read system state: %w", err)
	}

	now := s.clock.UnixMilli()
	eventID, err := s.ids.New("evt")
	if err != nil {
		return State{}, err
	}
	jobID, err := s.ids.New("job")
	if err != nil {
		return State{}, err
	}

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO system_state(singleton_id, mode, revision, reason, updated_at)
VALUES(1, 'bootstrap', 1, 'initial bootstrap', ?)`, now); err != nil {
			return fmt.Errorf("create system state: %w", err)
		}

		payload, _ := json.Marshal(map[string]any{"mode": ModeBootstrap, "revision": 1})
		if err := s.events.Append(ctx, tx, event.Event{
			ID: eventID, Type: "system.bootstrap_started", AggregateType: "system", AggregateID: "system",
			Payload: payload, OccurredAt: now,
		}); err != nil {
			return err
		}

		jobPayload, _ := json.Marshal(map[string]any{"reason": "initial_bootstrap"})
		return s.outbox.Enqueue(ctx, tx, outbox.Job{
			ID: jobID, Type: "system.bootstrap.continue", Payload: jobPayload,
			AvailableAt: now, MaxAttempts: 10, CreatedAt: now,
		})
	})
	if err != nil {
		return State{}, err
	}
	return s.Get(ctx)
}

func (s *Service) SetMode(ctx context.Context, expectedRevision int64, next Mode, reason string, actor *string) (State, error) {
	if !validMode(next) {
		return State{}, fmt.Errorf("invalid system mode %q", next)
	}

	now := s.clock.UnixMilli()
	eventID, err := s.ids.New("evt")
	if err != nil {
		return State{}, err
	}

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var current Mode
		if err := tx.QueryRowContext(ctx,
			`SELECT mode FROM system_state WHERE singleton_id = 1 AND revision = ?`, expectedRevision,
		).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrRevisionConflict
			}
			return err
		}
		if !CanTransition(current, next) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current, next)
		}

		res, err := tx.ExecContext(ctx, `
UPDATE system_state
SET mode = ?, revision = revision + 1, reason = ?, updated_at = ?
WHERE singleton_id = 1 AND revision = ?`, next, reason, now, expectedRevision)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrRevisionConflict
		}

		payload, _ := json.Marshal(map[string]any{
			"from": current, "to": next, "reason": reason,
			"revision": expectedRevision + 1,
		})
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, Type: "system.mode_changed", AggregateType: "system", AggregateID: "system",
			ActorPrincipalID: actor, Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		return State{}, err
	}
	return s.Get(ctx)
}

var (
	ErrRevisionConflict  = errors.New("revision conflict")
	ErrInvalidTransition = errors.New("invalid state transition")
)

func validMode(m Mode) bool {
	switch m {
	case ModeUninitialized, ModeBootstrap, ModeCommissioning, ModeRecovery, ModeNormal, ModeSafe, ModeReadOnly, ModeMaintenance, ModeDegraded:
		return true
	default:
		return false
	}
}

func CanTransition(from, to Mode) bool {
	if from == to {
		return false
	}
	if to == ModeSafe && from != ModeUninitialized {
		return true // emergency stop is always reachable from an initialized mode.
	}
	switch from {
	case ModeUninitialized:
		return to == ModeBootstrap
	case ModeBootstrap:
		return to == ModeCommissioning || to == ModeRecovery || to == ModeDegraded
	case ModeCommissioning:
		return to == ModeNormal || to == ModeDegraded || to == ModeRecovery
	case ModeRecovery:
		return to == ModeCommissioning || to == ModeNormal || to == ModeDegraded || to == ModeReadOnly
	case ModeNormal:
		return to == ModeDegraded || to == ModeReadOnly || to == ModeMaintenance || to == ModeRecovery
	case ModeDegraded:
		return to == ModeNormal || to == ModeReadOnly || to == ModeMaintenance || to == ModeRecovery
	case ModeReadOnly:
		return to == ModeNormal || to == ModeDegraded || to == ModeMaintenance || to == ModeRecovery
	case ModeMaintenance:
		return to == ModeCommissioning || to == ModeNormal || to == ModeDegraded || to == ModeRecovery
	case ModeSafe:
		return to == ModeRecovery || to == ModeCommissioning
	default:
		return false
	}
}
