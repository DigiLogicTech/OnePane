package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type invocationStore interface {
	Get(context.Context, string) (Invocation, error)
	Create(context.Context, Invocation, EventMeta) error
	Transition(context.Context, Transition, EventMeta) error
	ValidateExecutionContext(context.Context, string, string, *string, *string) error
}

type sqlInvocationStore struct {
	tx     storage.Transactor
	repo   repository
	events event.Store
	ids    id.Generator
}

func newSQLInvocationStore(db *sql.DB, tx storage.Transactor) invocationStore {
	return &sqlInvocationStore{tx: tx, repo: newSQLRepository(db), events: event.Store{}, ids: id.Generator{}}
}

func (s *sqlInvocationStore) Get(ctx context.Context, id string) (Invocation, error) {
	return s.repo.Get(ctx, id)
}

func (s *sqlInvocationStore) ValidateExecutionContext(ctx context.Context, workspaceID, principalID string, taskID, attemptID *string) error {
	return s.repo.ValidateExecutionContext(ctx, workspaceID, principalID, taskID, attemptID)
}

func (s *sqlInvocationStore) Create(ctx context.Context, inv Invocation, meta EventMeta) error {
	evtID, err := s.ids.New("evt")
	if err != nil {
		return err
	}
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := s.repo.Insert(ctx, tx, inv); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"invocation_id": inv.ID, "tool_id": inv.ToolID, "tool_version": inv.ToolVersion,
			"adapter_id": inv.AdapterID, "adapter_version": inv.AdapterVersion,
			"mode": inv.Mode, "resource_ref": inv.ResourceRef, "input_hash": inv.InputHash,
			"status": inv.Status,
		})
		return s.events.Append(ctx, tx, event.Event{
			ID: evtID, WorkspaceID: &inv.WorkspaceID, Type: "tool_invocation.created",
			AggregateType: "tool_invocation", AggregateID: inv.ID, ActorPrincipalID: meta.ActorPrincipalID,
			RequestID: meta.RequestID, TraceID: meta.TraceID, Payload: payload, OccurredAt: inv.CreatedAt,
		})
	})
}

func (s *sqlInvocationStore) Transition(ctx context.Context, tr Transition, meta EventMeta) error {
	if !CanTransition(tr.From, tr.To) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, tr.From, tr.To)
	}
	inv, err := s.repo.Get(ctx, tr.InvocationID)
	if err != nil {
		return err
	}
	evtID, err := s.ids.New("evt")
	if err != nil {
		return err
	}
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := s.repo.Transition(ctx, tx, tr); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"invocation_id": tr.InvocationID, "from": tr.From, "to": tr.To,
			"error_code": tr.ErrorCode, "summary": tr.Summary, "details": tr.Details,
		})
		return s.events.Append(ctx, tx, event.Event{
			ID: evtID, WorkspaceID: &inv.WorkspaceID, Type: "tool_invocation." + string(tr.To),
			AggregateType: "tool_invocation", AggregateID: tr.InvocationID, ActorPrincipalID: meta.ActorPrincipalID,
			RequestID: meta.RequestID, TraceID: meta.TraceID, Payload: payload, OccurredAt: tr.At,
		})
	})
}
