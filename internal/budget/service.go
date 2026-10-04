package budget

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

type Service struct {
	db     *sql.DB
	tx     storage.Transactor
	clock  clock.Clock
	ids    id.Generator
	events event.Store
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{db: db, tx: tx, clock: clk, ids: id.Generator{}, events: event.Store{}}
}

func canonicalPeriod(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		raw = []byte(`{"kind":"lifetime"}`)
	}
	var v struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if strings.ToLower(strings.TrimSpace(v.Kind)) != "lifetime" {
		return nil, fmt.Errorf("only lifetime budget periods are implemented")
	}
	return json.Marshal(map[string]any{"kind": "lifetime"})
}

func (s *Service) CreateAccount(ctx context.Context, cmd CreateAccountCommand) (Account, error) {
	name, unit := strings.TrimSpace(cmd.Name), strings.ToLower(strings.TrimSpace(cmd.Unit))
	if name == "" || unit == "" || cmd.LimitAmount < 0 {
		return Account{}, ErrInvalidCommand
	}
	period, err := canonicalPeriod(cmd.PeriodJSON)
	if err != nil {
		return Account{}, fmt.Errorf("%w: period_json: %v", ErrInvalidCommand, err)
	}
	idv, err := s.ids.New("budget")
	if err != nil {
		return Account{}, err
	}
	now := s.clock.UnixMilli()
	a := Account{ID: idv, WorkspaceID: cmd.WorkspaceID, ParentAccountID: cmd.ParentAccountID, Name: name, Unit: unit, LimitAmount: cmd.LimitAmount, PeriodJSON: period, Revision: 1, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if a.WorkspaceID != nil {
			var status string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM workspaces WHERE id=?`, *a.WorkspaceID).Scan(&status); err != nil {
				return err
			}
			if status != "active" {
				return fmt.Errorf("workspace is not active")
			}
		}
		if a.ParentAccountID != nil {
			var pws sql.NullString
			var punit string
			if err := tx.QueryRowContext(ctx, `SELECT workspace_id,unit FROM budget_accounts WHERE id=?`, *a.ParentAccountID).Scan(&pws, &punit); err != nil {
				return err
			}
			if punit != a.Unit {
				return fmt.Errorf("%w: parent account unit differs", ErrInvalidCommand)
			}
			if (a.WorkspaceID == nil) != !pws.Valid {
				return fmt.Errorf("%w: parent workspace differs", ErrWorkspaceMismatch)
			}
			if a.WorkspaceID != nil && (!pws.Valid || pws.String != *a.WorkspaceID) {
				return ErrWorkspaceMismatch
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_accounts(id,workspace_id,parent_account_id,name,unit,limit_amount,committed_amount,reserved_amount,period_json,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,0,0,?,1,?,?)`, a.ID, a.WorkspaceID, a.ParentAccountID, a.Name, a.Unit, a.LimitAmount, string(a.PeriodJSON), now, now); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"budget_account_id": a.ID, "unit": a.Unit, "limit_amount": a.LimitAmount})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: a.WorkspaceID, Type: "budget_account.created", AggregateType: "budget_account", AggregateID: a.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Account{}, err
	}
	return s.Account(ctx, a.ID)
}

func scanAccount(row interface{ Scan(...any) error }) (Account, error) {
	var a Account
	var ws, parent sql.NullString
	var period string
	if err := row.Scan(&a.ID, &ws, &parent, &a.Name, &a.Unit, &a.LimitAmount, &a.CommittedAmount, &a.ReservedAmount, &period, &a.Revision, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return Account{}, err
	}
	if ws.Valid {
		v := ws.String
		a.WorkspaceID = &v
	}
	if parent.Valid {
		v := parent.String
		a.ParentAccountID = &v
	}
	a.PeriodJSON = []byte(period)
	return a, nil
}

func (s *Service) Account(ctx context.Context, idv string) (Account, error) {
	if strings.TrimSpace(idv) == "" {
		return Account{}, ErrInvalidCommand
	}
	a, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,parent_account_id,name,unit,limit_amount,committed_amount,reserved_amount,period_json,revision,created_at,updated_at FROM budget_accounts WHERE id=?`, idv))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	return a, err
}

func scanReservation(row interface{ Scan(...any) error }) (Reservation, error) {
	var r Reservation
	var taskID, inferID, runtimeID sql.NullString
	var actual, closed sql.NullInt64
	if err := row.Scan(&r.ID, &r.BudgetAccountID, &taskID, &inferID, &runtimeID, &r.Amount, &r.Status, &actual, &r.ReservedAt, &r.ExpiresAt, &closed); err != nil {
		return Reservation{}, err
	}
	if taskID.Valid {
		v := taskID.String
		r.TaskID = &v
	}
	if inferID.Valid {
		v := inferID.String
		r.InferenceRequestID = &v
	}
	if runtimeID.Valid {
		v := runtimeID.String
		r.AgentRuntimeInvocationID = &v
	}
	if actual.Valid {
		v := actual.Int64
		r.ActualAmount = &v
	}
	if closed.Valid {
		v := closed.Int64
		r.ClosedAt = &v
	}
	return r, nil
}
func (s *Service) Reservation(ctx context.Context, idv string) (Reservation, error) {
	r, err := scanReservation(s.db.QueryRowContext(ctx, `SELECT id,budget_account_id,task_id,inference_request_id,agent_runtime_invocation_id,amount,status,actual_amount,reserved_at,expires_at,closed_at FROM budget_reservations WHERE id=?`, idv))
	if errors.Is(err, sql.ErrNoRows) {
		return Reservation{}, ErrReservationNotFound
	}
	return r, err
}

type accountCounter struct {
	ID                         string
	Parent                     *string
	Limit, Committed, Reserved int64
}

func accountChainTx(ctx context.Context, tx storage.Tx, accountID string) ([]accountCounter, error) {
	seen := map[string]struct{}{}
	var out []accountCounter
	cur := accountID
	for cur != "" {
		if _, ok := seen[cur]; ok {
			return nil, fmt.Errorf("budget account parent cycle detected")
		}
		seen[cur] = struct{}{}
		var a accountCounter
		var parent sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT id,parent_account_id,limit_amount,committed_amount,reserved_amount FROM budget_accounts WHERE id=?`, cur).Scan(&a.ID, &parent, &a.Limit, &a.Committed, &a.Reserved); err != nil {
			return nil, err
		}
		if parent.Valid {
			v := parent.String
			a.Parent = &v
			cur = v
		} else {
			cur = ""
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Service) Reserve(ctx context.Context, cmd ReserveCommand) (Reservation, error) {
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.AccountID) == "" || cmd.Amount <= 0 {
		return Reservation{}, ErrInvalidCommand
	}
	ttl := cmd.TTLMillis
	if ttl <= 0 {
		ttl = int64((10 * time.Minute) / time.Millisecond)
	}
	if ttl > int64((24*time.Hour)/time.Millisecond) {
		return Reservation{}, ErrInvalidCommand
	}
	idv, err := s.ids.New("bres")
	if err != nil {
		return Reservation{}, err
	}
	now := s.clock.UnixMilli()
	expires := now + ttl
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var ws sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM budget_accounts WHERE id=?`, cmd.AccountID).Scan(&ws); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrAccountNotFound
			}
			return err
		}
		if !ws.Valid || ws.String != cmd.WorkspaceID {
			return ErrWorkspaceMismatch
		}
		if cmd.TaskID != nil {
			var tws string
			if err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM tasks WHERE id=?`, *cmd.TaskID).Scan(&tws); err != nil {
				return err
			}
			if tws != cmd.WorkspaceID {
				return ErrTaskMismatch
			}
		}
		chain, err := accountChainTx(ctx, tx, cmd.AccountID)
		if err != nil {
			return err
		}
		for _, a := range chain {
			if cmd.Amount > a.Limit-a.Committed-a.Reserved {
				return ErrInsufficientBudget
			}
		}
		for _, a := range chain {
			res, err := tx.ExecContext(ctx, `UPDATE budget_accounts SET reserved_amount=reserved_amount+?,revision=revision+1,updated_at=? WHERE id=? AND committed_amount+reserved_amount+?<=limit_amount`, cmd.Amount, now, a.ID, cmd.Amount)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrInsufficientBudget
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_reservations(id,budget_account_id,task_id,inference_request_id,agent_runtime_invocation_id,amount,status,actual_amount,reserved_at,expires_at,closed_at) VALUES(?,?,?,NULL,NULL,?,'reserved',NULL,?,?,NULL)`, idv, cmd.AccountID, cmd.TaskID, cmd.Amount, now, expires); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"reservation_id": idv, "budget_account_id": cmd.AccountID, "amount": cmd.Amount, "expires_at": expires, "task_id": cmd.TaskID})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &cmd.WorkspaceID, Type: "budget.reserved", AggregateType: "budget_reservation", AggregateID: idv, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Reservation{}, err
	}
	return s.Reservation(ctx, idv)
}

func (s *Service) ValidateReservation(ctx context.Context, reservationID, workspaceID string, taskID *string) (Reservation, error) {
	r, err := s.Reservation(ctx, reservationID)
	if err != nil {
		return Reservation{}, err
	}
	if r.Status != ReservationReserved {
		return Reservation{}, ErrReservationClosed
	}
	if r.ExpiresAt <= s.clock.UnixMilli() {
		return Reservation{}, ErrReservationExpired
	}
	a, err := s.Account(ctx, r.BudgetAccountID)
	if err != nil {
		return Reservation{}, err
	}
	if a.WorkspaceID == nil || *a.WorkspaceID != workspaceID {
		return Reservation{}, ErrWorkspaceMismatch
	}
	if taskID != nil {
		if r.TaskID == nil || *r.TaskID != *taskID {
			return Reservation{}, ErrTaskMismatch
		}
	}
	return r, nil
}

func (s *Service) BindInferenceRequestTx(ctx context.Context, tx storage.Tx, reservationID, requestID, workspaceID string, taskID *string) error {
	var accountID, status string
	var rtask, existing sql.NullString
	var expires int64
	if err := tx.QueryRowContext(ctx, `SELECT budget_account_id,task_id,inference_request_id,status,expires_at FROM budget_reservations WHERE id=?`, reservationID).Scan(&accountID, &rtask, &existing, &status, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReservationNotFound
		}
		return err
	}
	if status != string(ReservationReserved) {
		return ErrReservationClosed
	}
	if expires <= s.clock.UnixMilli() {
		return ErrReservationExpired
	}
	if existing.Valid && existing.String != requestID {
		return ErrInferenceAlreadyBound
	}
	var aws sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM budget_accounts WHERE id=?`, accountID).Scan(&aws); err != nil {
		return err
	}
	if !aws.Valid || aws.String != workspaceID {
		return ErrWorkspaceMismatch
	}
	if taskID != nil {
		if !rtask.Valid || rtask.String != *taskID {
			return ErrTaskMismatch
		}
	}
	if !existing.Valid {
		res, err := tx.ExecContext(ctx, `UPDATE budget_reservations SET inference_request_id=? WHERE id=? AND inference_request_id IS NULL AND status='reserved'`, requestID, reservationID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrReservationAlreadyBound
		}
	}
	return nil
}

func (s *Service) BindAgentRuntimeInvocationTx(ctx context.Context, tx storage.Tx, reservationID, invocationID, workspaceID string, taskID *string) error {
	var accountID, status string
	var rtask, inferenceID, existing sql.NullString
	var expires int64
	if err := tx.QueryRowContext(ctx, `SELECT budget_account_id,task_id,inference_request_id,agent_runtime_invocation_id,status,expires_at FROM budget_reservations WHERE id=?`, reservationID).Scan(&accountID, &rtask, &inferenceID, &existing, &status, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReservationNotFound
		}
		return err
	}
	if status != string(ReservationReserved) {
		return ErrReservationClosed
	}
	if expires <= s.clock.UnixMilli() {
		return ErrReservationExpired
	}
	if inferenceID.Valid {
		return ErrReservationAlreadyBound
	}
	if existing.Valid && existing.String != invocationID {
		return ErrReservationAlreadyBound
	}
	var aws sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM budget_accounts WHERE id=?`, accountID).Scan(&aws); err != nil {
		return err
	}
	if !aws.Valid || aws.String != workspaceID {
		return ErrWorkspaceMismatch
	}
	if taskID != nil {
		if !rtask.Valid || rtask.String != *taskID {
			return ErrTaskMismatch
		}
	}
	if !existing.Valid {
		res, err := tx.ExecContext(ctx, `UPDATE budget_reservations SET agent_runtime_invocation_id=? WHERE id=? AND agent_runtime_invocation_id IS NULL AND status='reserved'`, invocationID, reservationID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrReservationAlreadyBound
		}
	}
	return nil
}

func (s *Service) Commit(ctx context.Context, cmd CloseCommand) (Reservation, error) {
	return s.close(ctx, cmd, ReservationCommitted)
}
func (s *Service) Release(ctx context.Context, cmd CloseCommand) (Reservation, error) {
	return s.close(ctx, cmd, ReservationReleased)
}
func (s *Service) close(ctx context.Context, cmd CloseCommand, target ReservationStatus) (Reservation, error) {
	if strings.TrimSpace(cmd.ReservationID) == "" {
		return Reservation{}, ErrInvalidCommand
	}
	now := s.clock.UnixMilli()
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := scanReservation(tx.QueryRowContext(ctx, `SELECT id,budget_account_id,task_id,inference_request_id,agent_runtime_invocation_id,amount,status,actual_amount,reserved_at,expires_at,closed_at FROM budget_reservations WHERE id=?`, cmd.ReservationID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrReservationNotFound
			}
			return err
		}
		if r.Status != ReservationReserved {
			return ErrReservationClosed
		}
		actual := int64(0)
		if target == ReservationCommitted {
			actual = r.Amount
			if cmd.ActualAmount != nil {
				actual = *cmd.ActualAmount
			}
			if actual < 0 || actual > r.Amount {
				return ErrActualExceedsReserve
			}
		}
		chain, err := accountChainTx(ctx, tx, r.BudgetAccountID)
		if err != nil {
			return err
		}
		for _, a := range chain {
			res, err := tx.ExecContext(ctx, `UPDATE budget_accounts SET reserved_amount=reserved_amount-?,committed_amount=committed_amount+?,revision=revision+1,updated_at=? WHERE id=? AND reserved_amount>=? AND committed_amount+reserved_amount-?+?<=limit_amount`, r.Amount, actual, now, a.ID, r.Amount, r.Amount, actual)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrInsufficientBudget
			}
		}
		res, err := tx.ExecContext(ctx, `UPDATE budget_reservations SET status=?,actual_amount=?,closed_at=? WHERE id=? AND status='reserved'`, target, func() any {
			if target == ReservationCommitted {
				return actual
			}
			return nil
		}(), now, r.ID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrReservationClosed
		}
		var ws sql.NullString
		_ = tx.QueryRowContext(ctx, `SELECT workspace_id FROM budget_accounts WHERE id=?`, r.BudgetAccountID).Scan(&ws)
		var wsp *string
		if ws.Valid {
			v := ws.String
			wsp = &v
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"reservation_id": r.ID, "budget_account_id": r.BudgetAccountID, "reserved_amount": r.Amount, "actual_amount": actual, "status": target})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: wsp, Type: "budget." + string(target), AggregateType: "budget_reservation", AggregateID: r.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Reservation{}, err
	}
	return s.Reservation(ctx, cmd.ReservationID)
}

func (s *Service) CommitFromUsage(ctx context.Context, reservationID string, usage json.RawMessage, actor *string) (Reservation, error) {
	r, err := s.Reservation(ctx, reservationID)
	if err != nil {
		return Reservation{}, err
	}
	a, err := s.Account(ctx, r.BudgetAccountID)
	if err != nil {
		return Reservation{}, err
	}
	actual := r.Amount
	switch strings.ToLower(a.Unit) {
	case "request", "requests", "subscription_request", "subscription_requests":
		actual = 1
	case "token", "tokens":
		var u struct {
			TotalTokens      int64 `json:"total_tokens"`
			InputTokens      int64 `json:"input_tokens"`
			OutputTokens     int64 `json:"output_tokens"`
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		}
		if json.Unmarshal(usage, &u) == nil {
			actual = u.TotalTokens
			if actual == 0 {
				actual = u.InputTokens + u.OutputTokens
			}
			if actual == 0 {
				actual = u.PromptTokens + u.CompletionTokens
			}
			if actual == 0 {
				actual = r.Amount
			}
		}
	}
	if actual > r.Amount {
		return Reservation{}, ErrActualExceedsReserve
	}
	return s.Commit(ctx, CloseCommand{ReservationID: reservationID, ActualAmount: &actual, ActorPrincipalID: actor})
}

func (s *Service) ExpireDue(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT br.id FROM budget_reservations br WHERE br.status='reserved' AND br.expires_at<=?
AND (br.task_id IS NULL OR NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id=br.task_id AND t.state IN ('running','completion_requested','verifying')))
AND (br.inference_request_id IS NULL OR NOT EXISTS (SELECT 1 FROM inference_requests ir WHERE ir.id=br.inference_request_id AND ir.status IN ('created','routed','budget_reserved','dispatched','executing')))
AND (br.agent_runtime_invocation_id IS NULL OR NOT EXISTS (SELECT 1 FROM agent_runtime_invocations ai WHERE ai.id=br.agent_runtime_invocation_id AND ai.status IN ('created','authorized','dispatched','executing')))
ORDER BY br.expires_at LIMIT ?`, s.clock.UnixMilli(), limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var idv string
		if err := rows.Scan(&idv); err != nil {
			return 0, err
		}
		ids = append(ids, idv)
	}
	n := 0
	for _, idv := range ids {
		if err := s.expire(ctx, idv); err == nil {
			n++
		} else if !errors.Is(err, ErrReservationClosed) && !errors.Is(err, ErrReservationActive) {
			return n, err
		}
	}
	return n, rows.Err()
}
func (s *Service) expire(ctx context.Context, idv string) error {
	now := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := scanReservation(tx.QueryRowContext(ctx, `SELECT id,budget_account_id,task_id,inference_request_id,agent_runtime_invocation_id,amount,status,actual_amount,reserved_at,expires_at,closed_at FROM budget_reservations WHERE id=?`, idv))
		if err != nil {
			return err
		}
		if r.Status != ReservationReserved {
			return ErrReservationClosed
		}
		if r.ExpiresAt > now {
			return ErrInvalidCommand
		}
		// Re-check active bindings inside the expiry transaction. ExpireDue's
		// candidate query is only advisory; work may become active after that
		// query but before this transaction acquires the write lock.
		var active int
		if r.TaskID != nil {
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id=? AND state IN ('running','completion_requested','verifying')`, *r.TaskID).Scan(&active); err != nil {
				return err
			}
			if active > 0 {
				return ErrReservationActive
			}
		}
		if r.InferenceRequestID != nil {
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inference_requests WHERE id=? AND status IN ('created','routed','budget_reserved','dispatched','executing')`, *r.InferenceRequestID).Scan(&active); err != nil {
				return err
			}
			if active > 0 {
				return ErrReservationActive
			}
		}
		if r.AgentRuntimeInvocationID != nil {
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runtime_invocations WHERE id=? AND status IN ('created','authorized','dispatched','executing')`, *r.AgentRuntimeInvocationID).Scan(&active); err != nil {
				return err
			}
			if active > 0 {
				return ErrReservationActive
			}
		}
		chain, err := accountChainTx(ctx, tx, r.BudgetAccountID)
		if err != nil {
			return err
		}
		for _, a := range chain {
			res, err := tx.ExecContext(ctx, `UPDATE budget_accounts SET reserved_amount=reserved_amount-?,revision=revision+1,updated_at=? WHERE id=? AND reserved_amount>=?`, r.Amount, now, a.ID, r.Amount)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrInsufficientBudget
			}
		}
		res, err := tx.ExecContext(ctx, `UPDATE budget_reservations SET status='expired',closed_at=? WHERE id=? AND status='reserved'`, now, idv)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrReservationClosed
		}
		var ws sql.NullString
		_ = tx.QueryRowContext(ctx, `SELECT workspace_id FROM budget_accounts WHERE id=?`, r.BudgetAccountID).Scan(&ws)
		var wsp *string
		if ws.Valid {
			v := ws.String
			wsp = &v
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"reservation_id": idv, "budget_account_id": r.BudgetAccountID, "amount": r.Amount})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: wsp, Type: "budget.expired", AggregateType: "budget_reservation", AggregateID: idv, Payload: payload, OccurredAt: now})
	})
}
