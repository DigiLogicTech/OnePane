package watchdog

import (
	"context"
	"database/sql"
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

type State struct {
	ID, Name                         string
	StateJSON                        json.RawMessage
	HeartbeatAt, Revision, UpdatedAt int64
}

type Service struct {
	db     *sql.DB
	tx     storage.Transactor
	clock  clock.Clock
	events event.Store
	ids    id.Generator
	name   string
	maxAge time.Duration
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, name string, maxAge time.Duration) *Service {
	if strings.TrimSpace(name) == "" {
		name = "control-plane"
	}
	if maxAge <= 0 {
		maxAge = 15 * time.Second
	}
	return &Service{db: db, tx: tx, clock: clk, events: event.Store{}, ids: id.Generator{}, name: name, maxAge: maxAge}
}

func (s *Service) Heartbeat(ctx context.Context, state map[string]any) error {
	if s == nil || s.db == nil || s.tx == nil {
		return errors.New("watchdog unavailable")
	}
	if state == nil {
		state = map[string]any{}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	now := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var idv string
		var rev int64
		err := tx.QueryRowContext(ctx, `SELECT id,revision FROM watchdog_states WHERE watchdog_name=?`, s.name).Scan(&idv, &rev)
		if errors.Is(err, sql.ErrNoRows) {
			idv, _ = s.ids.New("watchdog")
			if _, err := tx.ExecContext(ctx, `INSERT INTO watchdog_states(id,watchdog_name,state_json,heartbeat_at,revision,updated_at) VALUES(?,?,?,?,1,?)`, idv, s.name, string(raw), now, now); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			res, err := tx.ExecContext(ctx, `UPDATE watchdog_states SET state_json=?,heartbeat_at=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, string(raw), now, now, idv, rev)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return errors.New("watchdog revision conflict")
			}
		}
		return nil
	})
}

func (s *Service) Healthy(ctx context.Context) bool {
	if s == nil || s.db == nil || s.clock == nil {
		return false
	}
	var beat int64
	if err := s.db.QueryRowContext(ctx, `SELECT heartbeat_at FROM watchdog_states WHERE watchdog_name=?`, s.name).Scan(&beat); err != nil {
		return false
	}
	age := s.clock.Now().Sub(time.UnixMilli(beat))
	return age >= 0 && age <= s.maxAge
}

func (s *Service) State(ctx context.Context) (State, error) {
	var x State
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT id,watchdog_name,state_json,heartbeat_at,revision,updated_at FROM watchdog_states WHERE watchdog_name=?`, s.name).Scan(&x.ID, &x.Name, &raw, &x.HeartbeatAt, &x.Revision, &x.UpdatedAt)
	x.StateJSON = json.RawMessage(raw)
	return x, err
}

func (s *Service) Run(ctx context.Context, interval time.Duration, state func() map[string]any) error {
	if interval <= 0 || interval >= s.maxAge {
		return fmt.Errorf("watchdog interval must be positive and less than max age")
	}
	beat := func() error {
		payload := map[string]any{"status": "healthy"}
		if state != nil {
			payload = state()
		}
		return s.Heartbeat(ctx, payload)
	}
	if err := beat(); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := beat(); err != nil {
				return err
			}
		}
	}
}
