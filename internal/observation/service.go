package observation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type eventAppender interface {
	Append(context.Context, storage.Tx, event.Event) error
}

type Service struct {
	tx     storage.Transactor
	repo   repository
	events eventAppender
	ids    id.Generator
	clock  clock.Clock
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{tx: tx, repo: newSQLRepository(db), events: event.Store{}, ids: id.Generator{}, clock: clk}
}

func (s *Service) Get(ctx context.Context, observationID string) (Observation, error) {
	if strings.TrimSpace(observationID) == "" {
		return Observation{}, fmt.Errorf("%w: observation id is required", ErrInvalidCommand)
	}
	return s.repo.Get(ctx, observationID)
}

func (s *Service) Latest(ctx context.Context, workspaceID, subjectRef, observationType string) (Observation, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(subjectRef) == "" {
		return Observation{}, fmt.Errorf("%w: workspace and subject are required", ErrInvalidCommand)
	}
	return s.repo.Latest(ctx, workspaceID, subjectRef, strings.TrimSpace(observationType))
}

func (s *Service) Record(ctx context.Context, cmd RecordCommand) (Observation, error) {
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.SubjectRef) == "" ||
		strings.TrimSpace(cmd.ObservationType) == "" || strings.TrimSpace(cmd.ProbeToolID) == "" ||
		strings.TrimSpace(cmd.ProbeToolVersion) == "" {
		return Observation{}, fmt.Errorf("%w: workspace, subject, observation type and probe identity are required", ErrInvalidCommand)
	}
	if len(cmd.Value) == 0 || !json.Valid(cmd.Value) {
		return Observation{}, fmt.Errorf("%w: value must be valid JSON", ErrInvalidCommand)
	}
	if cmd.Label.WorkspaceID != cmd.WorkspaceID {
		return Observation{}, fmt.Errorf("%w: data label workspace mismatch", ErrInvalidCommand)
	}
	if err := policy.ValidateDataLabel(cmd.Label); err != nil {
		return Observation{}, fmt.Errorf("%w: %v", ErrInvalidCommand, err)
	}
	if cmd.SourcePrincipalID != nil && cmd.ActorPrincipalID != nil && strings.TrimSpace(*cmd.SourcePrincipalID) != strings.TrimSpace(*cmd.ActorPrincipalID) {
		return Observation{}, ErrSourceActorMismatch
	}
	if (cmd.AdapterID == nil) != (cmd.AdapterVersion == nil) {
		return Observation{}, fmt.Errorf("%w: adapter id and version must be supplied together", ErrInvalidCommand)
	}
	if cmd.AdapterID != nil && (strings.TrimSpace(*cmd.AdapterID) == "" || strings.TrimSpace(*cmd.AdapterVersion) == "") {
		return Observation{}, fmt.Errorf("%w: adapter id/version cannot be blank", ErrInvalidCommand)
	}

	now := s.clock.UnixMilli()
	observedAt := cmd.ObservedAt
	if observedAt == 0 {
		observedAt = now
	}
	if observedAt < 1 || observedAt > now {
		return Observation{}, fmt.Errorf("%w: observed_at must not be in the future", ErrInvalidCommand)
	}
	observationID, err := s.ids.New("obs")
	if err != nil {
		return Observation{}, err
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Observation{}, err
	}
	o := Observation{
		ID: observationID, WorkspaceID: cmd.WorkspaceID, SubjectRef: strings.TrimSpace(cmd.SubjectRef),
		ObservationType: strings.TrimSpace(cmd.ObservationType), ProbeToolID: strings.TrimSpace(cmd.ProbeToolID),
		ProbeToolVersion: strings.TrimSpace(cmd.ProbeToolVersion), SourcePrincipalID: cmd.SourcePrincipalID,
		AdapterID: cmd.AdapterID, AdapterVersion: cmd.AdapterVersion, Value: append(json.RawMessage(nil), cmd.Value...),
		Label: cmd.Label, ObservedAt: observedAt, CreatedAt: now,
	}
	o.IntegrityHash, err = IntegrityHash(o)
	if err != nil {
		return Observation{}, err
	}

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		status, err := s.repo.WorkspaceStatus(ctx, tx, o.WorkspaceID)
		if err != nil {
			return fmt.Errorf("resolve observation workspace: %w", err)
		}
		if status != "active" {
			return ErrWorkspaceInactive
		}
		if o.SourcePrincipalID != nil {
			if strings.TrimSpace(*o.SourcePrincipalID) == "" {
				return fmt.Errorf("%w: source principal cannot be blank", ErrInvalidCommand)
			}
			eligible, err := s.repo.SourceEligible(ctx, tx, o.WorkspaceID, *o.SourcePrincipalID)
			if err != nil {
				return fmt.Errorf("resolve observation source principal: %w", err)
			}
			if !eligible {
				return ErrSourceIneligible
			}
		}
		if err := s.repo.Insert(ctx, tx, o); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"observation_id": o.ID, "subject_ref": o.SubjectRef, "observation_type": o.ObservationType,
			"probe_tool_id": o.ProbeToolID, "probe_tool_version": o.ProbeToolVersion,
			"integrity_hash": o.IntegrityHash, "observed_at": o.ObservedAt,
		})
		actor := cmd.ActorPrincipalID
		if actor == nil {
			actor = cmd.SourcePrincipalID
		}
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, WorkspaceID: &o.WorkspaceID, Type: "observation.recorded",
			AggregateType: "observation", AggregateID: o.ID, ActorPrincipalID: actor,
			RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		return Observation{}, err
	}
	return s.repo.Get(ctx, o.ID)
}

func (s *Service) VerifyIntegrity(ctx context.Context, observationID string) error {
	o, err := s.Get(ctx, observationID)
	if err != nil {
		return err
	}
	hash, err := IntegrityHash(o)
	if err != nil {
		return err
	}
	if hash != o.IntegrityHash {
		return fmt.Errorf("observation integrity hash mismatch")
	}
	return nil
}
