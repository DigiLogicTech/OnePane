package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type Service struct {
	catalog *Catalog
	tx      storage.Transactor
	events  event.Store
	ids     id.Generator
	clock   clock.Clock
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{catalog: NewCatalog(db), tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk}
}
func (s *Service) Candidates(ctx context.Context, workspaceID, capabilityID, roleName string) ([]Candidate, error) {
	return s.catalog.Candidates(ctx, workspaceID, capabilityID, roleName)
}

func (s *Service) Route(ctx context.Context, req RouteRequest) (Decision, error) {
	candidates, err := s.catalog.Candidates(ctx, req.WorkspaceID, req.CapabilityID, req.RoleName)
	if err != nil {
		return Decision{}, err
	}
	decision, routeErr := Route(req, candidates)
	evtID, err := s.ids.New("evt")
	if err != nil {
		return decision, err
	}
	decisionID, err := s.ids.New("route")
	if err != nil {
		return decision, err
	}
	payload, _ := json.Marshal(map[string]any{"decision_id": decisionID, "request": req, "decision": decision, "error": errorString(routeErr)})
	if err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		return s.events.Append(ctx, tx, event.Event{ID: evtID, WorkspaceID: &req.WorkspaceID, Type: "scheduler.route_decided", AggregateType: "scheduler_route", AggregateID: decisionID, Payload: payload, OccurredAt: s.clock.UnixMilli()})
	}); err != nil {
		return decision, fmt.Errorf("record scheduler decision: %w", err)
	}
	return decision, routeErr
}
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
