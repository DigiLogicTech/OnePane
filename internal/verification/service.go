package verification

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

func (s *Service) Get(ctx context.Context, id string) (Verification, error) {
	if strings.TrimSpace(id) == "" {
		return Verification{}, fmt.Errorf("%w: verification id required", ErrInvalidCommand)
	}
	return s.repo.GetVerification(ctx, id)
}
func (s *Service) GetCheckpoint(ctx context.Context, id string) (Checkpoint, error) {
	if strings.TrimSpace(id) == "" {
		return Checkpoint{}, fmt.Errorf("%w: checkpoint id required", ErrInvalidCommand)
	}
	return s.repo.GetCheckpoint(ctx, id)
}
func (s *Service) LatestValidCheckpoint(ctx context.Context, taskID string) (Checkpoint, error) {
	if strings.TrimSpace(taskID) == "" {
		return Checkpoint{}, fmt.Errorf("%w: task id required", ErrInvalidCommand)
	}
	return s.repo.LatestValidCheckpoint(ctx, taskID)
}

func (s *Service) Create(ctx context.Context, cmd CreateCommand) (Verification, error) {
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.SubjectRef) == "" || !ValidLevel(cmd.RequiredLevel) {
		return Verification{}, fmt.Errorf("%w: workspace, subject and required level are required", ErrInvalidCommand)
	}
	if len(cmd.Spec) == 0 {
		cmd.Spec = json.RawMessage(`{}`)
	}
	if !json.Valid(cmd.Spec) {
		return Verification{}, fmt.Errorf("%w: spec must be valid JSON", ErrInvalidCommand)
	}
	if cmd.TaskID == nil && cmd.OperationID == nil {
		return Verification{}, fmt.Errorf("%w: task or operation binding is required", ErrInvalidCommand)
	}
	vid, err := s.ids.New("verification")
	if err != nil {
		return Verification{}, err
	}
	eid, err := s.ids.New("evt")
	if err != nil {
		return Verification{}, err
	}
	now := s.clock.UnixMilli()
	v := Verification{ID: vid, WorkspaceID: cmd.WorkspaceID, TaskID: cmd.TaskID, OperationID: cmd.OperationID, SubjectRef: strings.TrimSpace(cmd.SubjectRef), RequiredLevel: cmd.RequiredLevel, Status: StatusPending, Spec: append([]byte(nil), cmd.Spec...), StartedAt: now, Revision: 1}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if cmd.TaskID != nil {
			ws, e := s.repo.TaskWorkspaceTx(ctx, tx, *cmd.TaskID)
			if e != nil {
				return e
			}
			if ws != cmd.WorkspaceID {
				return ErrWorkspaceMismatch
			}
		}
		if cmd.OperationID != nil {
			ws, e := s.repo.OperationWorkspaceTx(ctx, tx, *cmd.OperationID)
			if e != nil {
				return e
			}
			if ws != cmd.WorkspaceID {
				return ErrWorkspaceMismatch
			}
		}
		if e := s.repo.InsertVerification(ctx, tx, v); e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"verification_id": v.ID, "task_id": v.TaskID, "operation_id": v.OperationID, "subject_ref": v.SubjectRef, "required_level": v.RequiredLevel, "status": v.Status, "revision": v.Revision})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &v.WorkspaceID, Type: "verification.started", AggregateType: "verification", AggregateID: v.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Verification{}, err
	}
	return s.repo.GetVerification(ctx, v.ID)
}

func (s *Service) Resolve(ctx context.Context, cmd ResolveCommand) (Verification, error) {
	if strings.TrimSpace(cmd.VerificationID) == "" || cmd.ExpectedRevision < 1 || (cmd.Status != StatusPass && cmd.Status != StatusFail && cmd.Status != StatusInconclusive) {
		return Verification{}, fmt.Errorf("%w: id, revision and terminal status are required", ErrInvalidCommand)
	}
	if len(cmd.Result) == 0 {
		cmd.Result = json.RawMessage(`{}`)
	}
	if !json.Valid(cmd.Result) {
		return Verification{}, fmt.Errorf("%w: result must be valid JSON", ErrInvalidCommand)
	}
	if strings.TrimSpace(cmd.VerifiedBy) == "" {
		return Verification{}, fmt.Errorf("%w: verified_by is required", ErrInvalidCommand)
	}
	if cmd.AchievedLevel != nil && !ValidLevel(*cmd.AchievedLevel) {
		return Verification{}, fmt.Errorf("%w: invalid achieved level", ErrInvalidCommand)
	}
	eid, err := s.ids.New("evt")
	if err != nil {
		return Verification{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		v, e := s.repo.GetVerificationTx(ctx, tx, cmd.VerificationID)
		if e != nil {
			return e
		}
		if v.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if !CanResolve(v.Status, cmd.Status) {
			return ErrInvalidTransition
		}
		eligible, e := s.repo.VerifierEligibleTx(ctx, tx, v.WorkspaceID, cmd.VerifiedBy)
		if e != nil {
			return e
		}
		if !eligible {
			return ErrVerifierIneligible
		}
		if cmd.Status == StatusPass {
			if cmd.AchievedLevel == nil || LevelRank(*cmd.AchievedLevel) < LevelRank(v.RequiredLevel) {
				return ErrInsufficientLevel
			}
		}
		var achieved *string
		if cmd.AchievedLevel != nil {
			x := string(*cmd.AchievedLevel)
			achieved = &x
		}
		result := any(string(cmd.Result))
		verified := any(cmd.VerifiedBy)
		completed := now
		if e := s.repo.ResolveVerification(ctx, tx, v.ID, v.Revision, v.Status, cmd.Status, achieved, &completed, result, verified); e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"verification_id": v.ID, "from": v.Status, "to": cmd.Status, "required_level": v.RequiredLevel, "achieved_level": cmd.AchievedLevel, "verified_by": cmd.VerifiedBy, "revision": v.Revision + 1})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &v.WorkspaceID, Type: "verification." + string(cmd.Status), AggregateType: "verification", AggregateID: v.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Verification{}, err
	}
	return s.repo.GetVerification(ctx, cmd.VerificationID)
}

func (s *Service) MarkStale(ctx context.Context, cmd StaleCommand) (Verification, error) {
	if strings.TrimSpace(cmd.VerificationID) == "" || cmd.ExpectedRevision < 1 {
		return Verification{}, fmt.Errorf("%w: id and revision required", ErrInvalidCommand)
	}
	eid, err := s.ids.New("evt")
	if err != nil {
		return Verification{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		v, e := s.repo.GetVerificationTx(ctx, tx, cmd.VerificationID)
		if e != nil {
			return e
		}
		if v.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if !CanResolve(v.Status, StatusStale) {
			return ErrInvalidTransition
		}
		var achieved *string
		if v.AchievedLevel != nil {
			x := string(*v.AchievedLevel)
			achieved = &x
		}
		var result any
		if len(v.Result) > 0 {
			result = string(v.Result)
		}
		var verified any
		if v.VerifiedBy != nil {
			verified = *v.VerifiedBy
		}
		completed := v.CompletedAt
		if e := s.repo.ResolveVerification(ctx, tx, v.ID, v.Revision, v.Status, StatusStale, achieved, completed, result, verified); e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"verification_id": v.ID, "from": v.Status, "to": StatusStale, "reason": strings.TrimSpace(cmd.Reason), "revision": v.Revision + 1})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &v.WorkspaceID, Type: "verification.stale", AggregateType: "verification", AggregateID: v.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Verification{}, err
	}
	return s.repo.GetVerification(ctx, cmd.VerificationID)
}

func (s *Service) CreateCheckpoint(ctx context.Context, cmd CheckpointCommand) (Checkpoint, error) {
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.TaskID) == "" || strings.TrimSpace(cmd.VerificationID) == "" {
		return Checkpoint{}, fmt.Errorf("%w: workspace, task and verification required", ErrInvalidCommand)
	}
	if len(cmd.State) == 0 || !json.Valid(cmd.State) {
		return Checkpoint{}, fmt.Errorf("%w: checkpoint state must be valid JSON", ErrInvalidCommand)
	}
	cid, err := s.ids.New("checkpoint")
	if err != nil {
		return Checkpoint{}, err
	}
	eid, err := s.ids.New("evt")
	if err != nil {
		return Checkpoint{}, err
	}
	now := s.clock.UnixMilli()
	c := Checkpoint{ID: cid, WorkspaceID: cmd.WorkspaceID, TaskID: cmd.TaskID, VerificationID: cmd.VerificationID, State: append([]byte(nil), cmd.State...), Status: CheckpointValid, CreatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		ws, e := s.repo.TaskWorkspaceTx(ctx, tx, cmd.TaskID)
		if e != nil {
			return e
		}
		if ws != cmd.WorkspaceID {
			return ErrWorkspaceMismatch
		}
		v, e := s.repo.GetVerificationTx(ctx, tx, cmd.VerificationID)
		if e != nil {
			return e
		}
		if v.WorkspaceID != cmd.WorkspaceID || v.TaskID == nil || *v.TaskID != cmd.TaskID {
			return ErrWorkspaceMismatch
		}
		if v.Status != StatusPass || v.AchievedLevel == nil || LevelRank(*v.AchievedLevel) < LevelRank(v.RequiredLevel) {
			return ErrVerificationNotPassed
		}
		unknown, e := s.repo.TaskHasUnknownMutationTx(ctx, tx, cmd.TaskID)
		if e != nil {
			return e
		}
		if unknown {
			return ErrUnknownMutationOutcome
		}
		if e := s.repo.InsertCheckpoint(ctx, tx, c); e != nil {
			return e
		}
		superseded, e := s.repo.SupersedeValidCheckpoints(ctx, tx, c.TaskID, c.ID, now)
		if e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"checkpoint_id": c.ID, "task_id": c.TaskID, "verification_id": c.VerificationID, "status": c.Status, "superseded_checkpoint_ids": superseded})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &c.WorkspaceID, Type: "checkpoint.created", AggregateType: "checkpoint", AggregateID: c.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Checkpoint{}, err
	}
	return s.repo.GetCheckpoint(ctx, c.ID)
}

func (s *Service) InvalidateCheckpoint(ctx context.Context, cmd InvalidateCheckpointCommand) (Checkpoint, error) {
	if strings.TrimSpace(cmd.CheckpointID) == "" {
		return Checkpoint{}, fmt.Errorf("%w: checkpoint id required", ErrInvalidCommand)
	}
	c, err := s.repo.GetCheckpoint(ctx, cmd.CheckpointID)
	if err != nil {
		return Checkpoint{}, err
	}
	if c.Status != CheckpointValid {
		return Checkpoint{}, ErrCheckpointInvalid
	}
	eid, err := s.ids.New("evt")
	if err != nil {
		return Checkpoint{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if e := s.repo.InvalidateCheckpoint(ctx, tx, c.ID, CheckpointValid, CheckpointStale, now); e != nil {
			return e
		}
		payload, _ := json.Marshal(map[string]any{"checkpoint_id": c.ID, "task_id": c.TaskID, "reason": strings.TrimSpace(cmd.Reason), "status": CheckpointStale})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &c.WorkspaceID, Type: "checkpoint.stale", AggregateType: "checkpoint", AggregateID: c.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Checkpoint{}, err
	}
	return s.repo.GetCheckpoint(ctx, c.ID)
}

func Meets(required, achieved policy.VerificationLevel) bool {
	return ValidLevel(required) && ValidLevel(achieved) && LevelRank(achieved) >= LevelRank(required)
}
