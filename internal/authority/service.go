package authority

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
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

func newService(tx storage.Transactor, repo repository, events eventAppender, clk clock.Clock) *Service {
	return &Service{tx: tx, repo: repo, events: events, ids: id.Generator{}, clock: clk}
}

func (s *Service) Get(ctx context.Context, leaseID string) (Lease, error) {
	if strings.TrimSpace(leaseID) == "" {
		return Lease{}, fmt.Errorf("%w: lease id is required", ErrInvalidCommand)
	}
	return s.repo.Get(ctx, leaseID)
}

func (s *Service) Subject(ctx context.Context, workspaceID, principalID string) (Subject, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(principalID) == "" {
		return Subject{}, fmt.Errorf("%w: workspace and principal are required", ErrInvalidCommand)
	}
	return s.repo.Subject(ctx, workspaceID, principalID)
}

func (s *Service) ValidateSubject(ctx context.Context, workspaceID, principalID string) error {
	subject, err := s.Subject(ctx, workspaceID, principalID)
	if err != nil {
		return err
	}
	return validateSubject(subject)
}

func (s *Service) TaskWorkspace(ctx context.Context, taskID string) (string, error) {
	if strings.TrimSpace(taskID) == "" {
		return "", fmt.Errorf("%w: task id is required", ErrInvalidCommand)
	}
	return s.repo.TaskWorkspace(ctx, taskID)
}

func (s *Service) Issue(ctx context.Context, cmd IssueCommand) (Lease, error) {
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.PrincipalID) == "" ||
		strings.TrimSpace(cmd.CapabilityID) == "" || strings.TrimSpace(cmd.IssuedBy) == "" {
		return Lease{}, fmt.Errorf("%w: workspace, principal, capability and issuer are required", ErrInvalidCommand)
	}
	if err := cmd.Scope.Validate(); err != nil {
		return Lease{}, err
	}
	now := s.clock.UnixMilli()
	if cmd.ExpiresAt <= now {
		return Lease{}, fmt.Errorf("%w: expires_at must be in the future", ErrInvalidCommand)
	}
	if cmd.UsageLimit != nil && *cmd.UsageLimit <= 0 {
		return Lease{}, fmt.Errorf("%w: usage_limit must be positive", ErrInvalidCommand)
	}

	leaseID, err := s.ids.New("lease")
	if err != nil {
		return Lease{}, err
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Lease{}, err
	}

	lease := Lease{
		ID: leaseID, WorkspaceID: cmd.WorkspaceID, PrincipalID: cmd.PrincipalID, TaskID: cmd.TaskID,
		CapabilityID: strings.TrimSpace(cmd.CapabilityID), Scope: cmd.Scope, Status: StatusActive,
		IssuedBy: cmd.IssuedBy, IssuedAt: now, ExpiresAt: cmd.ExpiresAt, UsageLimit: cmd.UsageLimit,
		UsageCount: 0, Revision: 1,
	}

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		target, err := s.repo.SubjectTx(ctx, tx, cmd.WorkspaceID, cmd.PrincipalID)
		if err != nil {
			return fmt.Errorf("resolve lease subject: %w", err)
		}
		if err := validateSubject(target); err != nil {
			return err
		}
		issuer, err := s.repo.SubjectTx(ctx, tx, cmd.WorkspaceID, cmd.IssuedBy)
		if err != nil {
			return fmt.Errorf("resolve lease issuer: %w", err)
		}
		if err := validateSubject(issuer); err != nil {
			return fmt.Errorf("issuer: %w", err)
		}
		if cmd.PrincipalID == cmd.IssuedBy && target.PrincipalType == "agent" {
			return ErrSelfGrant
		}
		if cmd.TaskID != nil {
			if strings.TrimSpace(*cmd.TaskID) == "" {
				return fmt.Errorf("%w: task id cannot be blank", ErrInvalidCommand)
			}
			workspaceID, err := s.repo.TaskWorkspaceTx(ctx, tx, *cmd.TaskID)
			if err != nil {
				return fmt.Errorf("resolve task workspace: %w", err)
			}
			if workspaceID != cmd.WorkspaceID {
				return ErrTaskWorkspace
			}
		}
		if err := s.repo.Insert(ctx, tx, lease); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"lease_id": lease.ID, "principal_id": lease.PrincipalID, "task_id": lease.TaskID,
			"capability_id": lease.CapabilityID, "scope": lease.Scope, "expires_at": lease.ExpiresAt,
			"usage_limit": lease.UsageLimit, "revision": lease.Revision,
		})
		actor := cmd.ActorPrincipalID
		if actor == nil {
			actor = &cmd.IssuedBy
		}
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, WorkspaceID: &lease.WorkspaceID, Type: "capability_lease.issued",
			AggregateType: "capability_lease", AggregateID: lease.ID, ActorPrincipalID: actor,
			RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		return Lease{}, err
	}
	return s.repo.Get(ctx, lease.ID)
}

func (s *Service) Revoke(ctx context.Context, cmd TransitionCommand) (Lease, error) {
	return s.transition(ctx, cmd, StatusRevoked, "capability_lease.revoked", false)
}

func (s *Service) Invalidate(ctx context.Context, cmd TransitionCommand) (Lease, error) {
	return s.transition(ctx, cmd, StatusInvalidated, "capability_lease.invalidated", false)
}

func (s *Service) Expire(ctx context.Context, cmd TransitionCommand) (Lease, error) {
	return s.transition(ctx, cmd, StatusExpired, "capability_lease.expired", true)
}

func (s *Service) transition(ctx context.Context, cmd TransitionCommand, to Status, eventType string, requireDue bool) (Lease, error) {
	if strings.TrimSpace(cmd.LeaseID) == "" || cmd.ExpectedRevision < 1 {
		return Lease{}, fmt.Errorf("%w: lease id and expected revision are required", ErrInvalidCommand)
	}
	if !ValidStatus(to) || to == StatusActive {
		return Lease{}, fmt.Errorf("%w: invalid target status %q", ErrInvalidCommand, to)
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Lease{}, err
	}
	now := s.clock.UnixMilli()

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		lease, err := s.repo.GetForUpdate(ctx, tx, cmd.LeaseID)
		if err != nil {
			return err
		}
		if lease.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if !CanTransition(lease.Status, to) {
			return statusTransitionError(lease.Status)
		}
		if requireDue && now < lease.ExpiresAt {
			return fmt.Errorf("%w: lease is not due to expire", ErrInvalidCommand)
		}
		if err := s.repo.Transition(ctx, tx, lease.ID, lease.Revision, lease.Status, to); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"lease_id": lease.ID, "from": lease.Status, "to": to,
			"reason": strings.TrimSpace(cmd.Reason), "revision": lease.Revision + 1,
		})
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, WorkspaceID: &lease.WorkspaceID, Type: eventType,
			AggregateType: "capability_lease", AggregateID: lease.ID, ActorPrincipalID: cmd.ActorPrincipalID,
			RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		return Lease{}, err
	}
	return s.repo.Get(ctx, cmd.LeaseID)
}

func (s *Service) Consume(ctx context.Context, cmd ConsumeCommand) (Lease, error) {
	if strings.TrimSpace(cmd.LeaseID) == "" || cmd.ExpectedRevision < 1 || cmd.Uses <= 0 {
		return Lease{}, fmt.Errorf("%w: lease id, expected revision and positive uses are required", ErrInvalidCommand)
	}
	usedEventID, err := s.ids.New("evt")
	if err != nil {
		return Lease{}, err
	}
	exhaustedEventID, err := s.ids.New("evt")
	if err != nil {
		return Lease{}, err
	}
	now := s.clock.UnixMilli()

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		lease, err := s.repo.GetForUpdate(ctx, tx, cmd.LeaseID)
		if err != nil {
			return err
		}
		if lease.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if lease.Status != StatusActive {
			return statusTransitionError(lease.Status)
		}
		if now >= lease.ExpiresAt {
			return ErrExpired
		}
		if lease.UsageLimit != nil && lease.UsageCount+cmd.Uses > *lease.UsageLimit {
			return ErrExhausted
		}
		next := StatusActive
		if lease.UsageLimit != nil && lease.UsageCount+cmd.Uses == *lease.UsageLimit {
			next = StatusExhausted
		}

		// Repository UPDATE repeats the limit/status predicates as the durable
		// concurrency backstop. The service checks above provide precise errors.
		if err := s.repo.ConsumeAt(ctx, tx, lease, cmd.Uses, next, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"lease_id": lease.ID, "uses": cmd.Uses, "usage_count": lease.UsageCount + cmd.Uses,
			"status": next, "revision": lease.Revision + 1,
		})
		if err := s.events.Append(ctx, tx, event.Event{
			ID: usedEventID, WorkspaceID: &lease.WorkspaceID, Type: "capability_lease.used",
			AggregateType: "capability_lease", AggregateID: lease.ID, ActorPrincipalID: cmd.ActorPrincipalID,
			RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
		}); err != nil {
			return err
		}
		if next == StatusExhausted {
			exhaustedPayload, _ := json.Marshal(map[string]any{
				"lease_id": lease.ID, "usage_limit": lease.UsageLimit, "revision": lease.Revision + 1,
			})
			if err := s.events.Append(ctx, tx, event.Event{
				ID: exhaustedEventID, WorkspaceID: &lease.WorkspaceID, Type: "capability_lease.exhausted",
				AggregateType: "capability_lease", AggregateID: lease.ID, ActorPrincipalID: cmd.ActorPrincipalID,
				RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: exhaustedPayload, OccurredAt: now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Lease{}, err
	}
	return s.repo.Get(ctx, cmd.LeaseID)
}

func validateSubject(s Subject) error {
	if s.WorkspaceStatus != "active" {
		return ErrWorkspaceInactive
	}
	if s.PrincipalStatus != "active" {
		return ErrPrincipalInactive
	}
	switch s.PrincipalType {
	case "system", "recovery", "watchdog":
		return nil
	case "human", "agent", "service":
		if s.MembershipStatus == nil || *s.MembershipStatus != "active" {
			return ErrMembershipInactive
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown principal type %q", ErrPrincipalInactive, s.PrincipalType)
	}
}

func statusTransitionError(status Status) error {
	switch status {
	case StatusExpired:
		return ErrExpired
	case StatusRevoked:
		return ErrRevoked
	case StatusExhausted:
		return ErrExhausted
	case StatusInvalidated:
		return ErrInvalidated
	default:
		return fmt.Errorf("%w: from %s", ErrInvalidTransition, status)
	}
}
