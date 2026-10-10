package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/outbox"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type eventAppender interface {
	Append(context.Context, storage.Tx, event.Event) error
}

type outboxEnqueuer interface {
	Enqueue(context.Context, storage.Tx, outbox.Job) error
}

type admissionGuard interface {
	AllowReady(context.Context, Task) error
	AllowStart(context.Context, Task) error
}

type Service struct {
	tx     storage.Transactor
	repo   repository
	events eventAppender
	outbox outboxEnqueuer
	ids    id.Generator
	clock  clock.Clock
	guard  admissionGuard
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{
		tx: tx, repo: newSQLRepository(db), events: event.Store{}, outbox: outbox.Store{},
		ids: id.Generator{}, clock: clk,
	}
}

func newService(tx storage.Transactor, repo repository, events eventAppender, outbox outboxEnqueuer, clk clock.Clock) *Service {
	return &Service{tx: tx, repo: repo, events: events, outbox: outbox, ids: id.Generator{}, clock: clk}
}

// SetAdmissionGuard lets another trusted control-plane subsystem impose an
// additional admission invariant without moving authority into the caller.
func (s *Service) SetAdmissionGuard(g admissionGuard) { s.guard = g }

func (s *Service) Get(ctx context.Context, taskID string) (Task, error) {
	if strings.TrimSpace(taskID) == "" {
		return Task{}, fmt.Errorf("%w: task id is required", ErrInvalidCommand)
	}
	return s.repo.Get(ctx, taskID)
}

func (s *Service) List(ctx context.Context, workspaceID string, limit int) ([]Task, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidCommand)
	}
	return s.repo.List(ctx, workspaceID, limit)
}

// ListProjectWorkspace exposes the authorised backend-scoped Task inventory.
// The API independently authenticates the tenancy, Project and Workspace
// relationships. This selector cannot accidentally return other Workspaces.
func (s *Service) ListProjectWorkspace(ctx context.Context, tenancyID, projectID, projectWorkspaceID string, limit int) ([]Task,error) {
 if strings.TrimSpace(tenancyID)==""||strings.TrimSpace(projectID)==""||strings.TrimSpace(projectWorkspaceID)==""{
  return nil,fmt.Errorf("%w: tenancy, Project and Workspace required",ErrInvalidCommand)
 }
 reader,ok:=s.repo.(interface{
  ListProjectWorkspace(context.Context,string,string,string,int)([]Task,error)
 })
 if !ok{return nil,fmt.Errorf("%w: scoped Task repository is unavailable",ErrInvalidCommand)}
 return reader.ListProjectWorkspace(ctx,tenancyID,projectID,projectWorkspaceID,limit)
}

func (s *Service) ListArchived(ctx context.Context, workspaceID string, limit int) ([]Task, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidCommand)
	}
	return s.repo.ListArchived(ctx, workspaceID, limit)
}

func (s *Service) Create(ctx context.Context, cmd CreateCommand) (Task, error) {
	var created Task
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		created, err = s.CreateInTransaction(ctx, tx, cmd)
		return err
	})
	if err != nil {
		return Task{}, err
	}
	return s.repo.Get(ctx, created.ID)
}

// CreateInTransaction creates a Task using a caller-owned transaction. It exists
// for trusted control-plane components (for example the Routine Engine) that
// must atomically bind another durable object to the Task. It preserves the
// same Task/Event/Outbox invariants as Create and is not an API surface for
// models or adapters.
func (s *Service) CreateInTransaction(ctx context.Context, tx storage.Tx, cmd CreateCommand) (Task, error) {
	if tx == nil {
		return Task{}, fmt.Errorf("%w: transaction is required", ErrInvalidCommand)
	}
	if strings.TrimSpace(cmd.WorkspaceID) == "" {
		return Task{}, fmt.Errorf("%w: workspace id is required", ErrInvalidCommand)
	}
	if strings.TrimSpace(cmd.Objective) == "" {
		return Task{}, fmt.Errorf("%w: objective is required", ErrInvalidCommand)
	}
	if cmd.SchedulingClass == "" {
		cmd.SchedulingClass = ClassNormal
	}
	if !ValidSchedulingClass(cmd.SchedulingClass) {
		return Task{}, fmt.Errorf("%w: invalid scheduling class %q", ErrInvalidCommand, cmd.SchedulingClass)
	}
	if len(cmd.Completion) == 0 {
		cmd.Completion = json.RawMessage(`{}`)
	}
	if !json.Valid(cmd.Completion) {
		return Task{}, fmt.Errorf("%w: completion criteria must be valid JSON", ErrInvalidCommand)
	}
	if cmd.ProjectWorkspaceID != nil {
		if cmd.ProjectID == nil || strings.TrimSpace(*cmd.ProjectID)=="" {
			return Task{}, fmt.Errorf("%w: named Workspace Tasks require a Project", ErrInvalidCommand)
		}
		var err error
		cmd.Completion, err = scopeWorkspaceCompletion(cmd.Completion,*cmd.ProjectWorkspaceID)
		if err!=nil {return Task{},err}
	}
	taskID, err := s.ids.New("task")
	if err != nil {
		return Task{}, err
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Task{}, err
	}
	jobID, err := s.ids.New("job")
	if err != nil {
		return Task{}, err
	}
	now := s.clock.UnixMilli()
	t := Task{ID: taskID, WorkspaceID: cmd.WorkspaceID, ProjectID: cmd.ProjectID, ProjectWorkspaceID: cmd.ProjectWorkspaceID,
		ArtifactSessionID: cmd.ArtifactSessionID, PlanID: cmd.PlanID, ParentTaskID: cmd.ParentTaskID,
		Objective: strings.TrimSpace(cmd.Objective), State: StateCreated, SchedulingClass: cmd.SchedulingClass,
		Priority: cmd.Priority, Completion: cloneJSON(cmd.Completion), Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Insert(ctx, tx, t); err != nil {
		return Task{}, err
	}
	payload, _ := json.Marshal(map[string]any{"task_id": taskID, "objective": t.Objective, "state": t.State, "revision": t.Revision, "scheduling_class": t.SchedulingClass, "project_id": t.ProjectID, "project_workspace_id": t.ProjectWorkspaceID})
	if err := s.events.Append(ctx, tx, event.Event{ID: eventID, WorkspaceID: &cmd.WorkspaceID, Type: "task.created", AggregateType: "task", AggregateID: taskID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now}); err != nil {
		return Task{}, err
	}
	jobPayload, _ := json.Marshal(map[string]any{"task_id": taskID, "expected_revision": 1})
	if err := s.outbox.Enqueue(ctx, tx, outbox.Job{ID: jobID, WorkspaceID: &cmd.WorkspaceID, Type: "task.admission.evaluate", Payload: jobPayload, AvailableAt: now, MaxAttempts: 10, CreatedAt: now}); err != nil {
		return Task{}, err
	}
	return t, nil
}


func (s *Service) Archive(ctx context.Context, cmd ArchiveCommand) (Task, error) {
	return s.setArchived(ctx, cmd, true)
}

func (s *Service) Unarchive(ctx context.Context, cmd ArchiveCommand) (Task, error) {
	return s.setArchived(ctx, cmd, false)
}

func (s *Service) setArchived(ctx context.Context, cmd ArchiveCommand, archived bool) (Task, error) {
	if strings.TrimSpace(cmd.TaskID) == "" || cmd.ExpectedRevision < 1 {
		return Task{}, fmt.Errorf("%w: task id and expected revision are required", ErrInvalidCommand)
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Task{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
		if err != nil {
			return err
		}
		if t.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if archived && t.ArchivedAt != nil {
			return nil
		}
		if !archived && t.ArchivedAt == nil {
			return nil
		}
		var at *int64
		eventType := "task.unarchived"
		if archived {
			v := now
			at = &v
			eventType = "task.archived"
		}
		if err := s.repo.SetArchived(ctx, tx, t.ID, t.Revision, at, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"task_id": t.ID, "archived": archived, "revision": t.Revision + 1})
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, WorkspaceID: &t.WorkspaceID, Type: eventType,
			AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID,
			RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		return Task{}, err
	}
	return s.repo.Get(ctx, cmd.TaskID)
}

func (s *Service) MarkReady(ctx context.Context, cmd TransitionCommand) (Task, error) {
	if s.guard != nil {
		t, err := s.repo.Get(ctx, cmd.TaskID)
		if err != nil {
			return Task{}, err
		}
		if err := s.guard.AllowReady(ctx, t); err != nil {
			return Task{}, err
		}
	}
	return s.transition(ctx, cmd, StateReady, "task.ready", func(tr *transitionRecord, now int64) { tr.ReadyAt = &now })
}

func (s *Service) Start(ctx context.Context, cmd StartCommand) (Task, Attempt, error) {
	if strings.TrimSpace(cmd.TaskID) == "" || cmd.ExpectedRevision < 1 {
		return Task{}, Attempt{}, fmt.Errorf("%w: task id and expected revision are required", ErrInvalidCommand)
	}
	if s.guard != nil {
		t, err := s.repo.Get(ctx, cmd.TaskID)
		if err != nil {
			return Task{}, Attempt{}, err
		}
		if err := s.guard.AllowStart(ctx, t); err != nil {
			return Task{}, Attempt{}, err
		}
	}
	if len(cmd.Metadata) == 0 {
		cmd.Metadata = json.RawMessage(`{}`)
	}
	if !json.Valid(cmd.Metadata) {
		return Task{}, Attempt{}, fmt.Errorf("%w: attempt metadata must be valid JSON", ErrInvalidCommand)
	}

	attemptID, err := s.ids.New("attempt")
	if err != nil {
		return Task{}, Attempt{}, err
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Task{}, Attempt{}, err
	}
	now := s.clock.UnixMilli()
	var created Attempt

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
		if err != nil {
			return err
		}
		if t.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if !CanTransition(t.State, StateRunning) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.State, StateRunning)
		}
		active, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		if active != nil {
			return ErrActiveAttempt
		}
		n, err := s.repo.NextAttemptNumber(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		created = Attempt{
			ID: attemptID, TaskID: t.ID, AttemptNumber: n, WorkerPrincipalID: cmd.WorkerPrincipalID,
			State: AttemptRunning, StartedAt: &now, Metadata: cloneJSON(cmd.Metadata),
		}
		if err := s.repo.InsertAttempt(ctx, tx, created); err != nil {
			return err
		}
		if err := s.repo.Transition(ctx, tx, transitionRecord{
			TaskID: t.ID, ExpectedRevision: t.Revision, From: t.State, To: StateRunning, UpdatedAt: now,
		}); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"task_id": t.ID, "attempt_id": attemptID, "attempt_number": n,
			"from": t.State, "to": StateRunning, "revision": t.Revision + 1,
		})
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, WorkspaceID: &t.WorkspaceID, Type: "task.started",
			AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID,
			RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		return Task{}, Attempt{}, err
	}
	t, err := s.repo.Get(ctx, cmd.TaskID)
	return t, created, err
}

func (s *Service) WaitDependency(ctx context.Context, cmd TransitionCommand) (Task, error) {
	return s.transitionWithAttempt(ctx, cmd, StateWaitingDependency, AttemptWaiting, "task.waiting_dependency")
}

// WaitDependencyInTransaction is for trusted Agent Worker delegation.
// Callers may atomically write the delegated child, dependency edge, Worker
// continuation and step journal alongside this Task/Attempt transition.
// Unlike the public WaitDependency method this requires an actively running
// attempt; it cannot re-admit a previously blocked or completed operation.
func (s *Service) WaitDependencyInTransaction(ctx context.Context, tx storage.Tx, cmd TransitionCommand) error {
 if tx==nil || strings.TrimSpace(cmd.TaskID)=="" || cmd.ExpectedRevision<1 {
  return fmt.Errorf("%w: transaction, Task identity and revision required",ErrInvalidCommand)
 }
 t,err:=s.repo.GetForUpdate(ctx,tx,cmd.TaskID)
 if err!=nil{return err}
 if t.Revision!=cmd.ExpectedRevision{return ErrRevisionConflict}
 if t.State!=StateRunning || !CanTransition(t.State,StateWaitingDependency) {
  return fmt.Errorf("%w: delegation requires running Task, found %s",ErrInvalidTransition,t.State)
 }
 active,err:=s.repo.ActiveAttempt(ctx,tx,t.ID)
 if err!=nil{return err}
 if active==nil {return ErrNoActiveAttempt}
 if active.State!=AttemptRunning {
  return fmt.Errorf("%w: delegation requires running Attempt, found %s",ErrInvalidAttempt,active.State)
 }
 now:=s.clock.UnixMilli()
 if err:=s.repo.TransitionAttempt(ctx,tx,active.ID,AttemptRunning,AttemptWaiting,now);err!=nil{return err}
 if err:=s.repo.Transition(ctx,tx,transitionRecord{
  TaskID:t.ID,ExpectedRevision:t.Revision,From:t.State,To:StateWaitingDependency,UpdatedAt:now,
 });err!=nil{return err}
 eid,err:=s.ids.New("evt")
 if err!=nil{return err}
 payload,_:=json.Marshal(map[string]any{
  "task_id":t.ID,"from":t.State,"to":StateWaitingDependency,
  "reason":cmd.Reason,"revision":t.Revision+1,
 })
 return s.events.Append(ctx,tx,event.Event{
  ID:eid,WorkspaceID:&t.WorkspaceID,Type:"task.waiting_dependency",
  AggregateType:"task",AggregateID:t.ID,ActorPrincipalID:cmd.ActorPrincipalID,
  RequestID:cmd.RequestID,TraceID:cmd.TraceID,Payload:payload,OccurredAt:now,
 })
}

// ResumeWaitingAttemptInTransaction is the trusted autonomous Worker wake path.
// It is deliberately narrower than operator Resume: only a dependency/resource
// wait with the *same* active Attempt may be reactivated. Callers commit the
// matching Worker status in the same transaction; failed journal/outbox writes
// or stale revisions roll back the Task, Attempt and Worker together.
func (s *Service) ResumeWaitingAttemptInTransaction(ctx context.Context, tx storage.Tx, cmd TransitionCommand, attemptID string) error {
 if tx==nil||strings.TrimSpace(cmd.TaskID)==""||cmd.ExpectedRevision<1||
  strings.TrimSpace(attemptID)==""{
  return fmt.Errorf("%w: transaction, Task revision and Attempt identity required",ErrInvalidCommand)
 }
 t,err:=s.repo.GetForUpdate(ctx,tx,cmd.TaskID)
 if err!=nil{return err}
 if t.Revision!=cmd.ExpectedRevision{return ErrRevisionConflict}
 if t.State!=StateWaitingDependency||!CanTransition(t.State,StateRunning){
  return fmt.Errorf("%w: autonomous wake requires dependency-waiting Task, found %s",ErrInvalidTransition,t.State)
 }
 active,err:=s.repo.ActiveAttempt(ctx,tx,t.ID)
 if err!=nil{return err}
 if active==nil||active.ID!=attemptID||active.State!=AttemptWaiting{
  return fmt.Errorf("%w: autonomous wake requires the original waiting Attempt",ErrInvalidAttempt)
 }
 now:=s.clock.UnixMilli()
 if err:=s.repo.TransitionAttempt(ctx,tx,active.ID,AttemptWaiting,AttemptRunning,now);err!=nil{return err}
 if err:=s.repo.Transition(ctx,tx,transitionRecord{
  TaskID:t.ID,ExpectedRevision:t.Revision,From:StateWaitingDependency,To:StateRunning,UpdatedAt:now,
 });err!=nil{return err}
 eid,err:=s.ids.New("evt")
 if err!=nil{return err}
 payload,_:=json.Marshal(map[string]any{
  "task_id":t.ID,"from":StateWaitingDependency,"to":StateRunning,
  "attempt_id":attemptID,"reason":cmd.Reason,"revision":t.Revision+1,
 })
 return s.events.Append(ctx,tx,event.Event{
  ID:eid,WorkspaceID:&t.WorkspaceID,Type:"task.resumed",
  AggregateType:"task",AggregateID:t.ID,ActorPrincipalID:cmd.ActorPrincipalID,
  RequestID:cmd.RequestID,TraceID:cmd.TraceID,Payload:payload,OccurredAt:now,
 })
}

func (s *Service) WaitApproval(ctx context.Context, cmd TransitionCommand) (Task, error) {
	return s.transitionWithAttempt(ctx, cmd, StateWaitingApproval, AttemptWaiting, "task.waiting_approval")
}

func (s *Service) Pause(ctx context.Context, cmd TransitionCommand) (Task, error) {
	return s.transitionWithOptionalAttempt(ctx, cmd, StatePaused, AttemptWaiting, "task.paused")
}

// Resume resumes an existing waiting Attempt when one exists. A Task paused
// before any Attempt began becomes READY and waits for scheduler admission.
func (s *Service) Resume(ctx context.Context, cmd TransitionCommand) (Task, error) {
	if strings.TrimSpace(cmd.TaskID) == "" || cmd.ExpectedRevision < 1 {
		return Task{}, fmt.Errorf("%w: task id and expected revision are required", ErrInvalidCommand)
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Task{}, err
	}
	now := s.clock.UnixMilli()

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
		if err != nil {
			return err
		}
		if t.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if t.State != StatePaused && t.State != StateWaitingDependency && t.State != StateWaitingApproval {
			return fmt.Errorf("%w: cannot resume from %s", ErrInvalidTransition, t.State)
		}
		active, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		next := StateReady
		var readyAt *int64
		if active != nil {
			if active.State != AttemptWaiting {
				return ErrActiveAttempt
			}
			if err := s.repo.TransitionAttempt(ctx, tx, active.ID, active.State, AttemptRunning, now); err != nil {
				return err
			}
			next = StateRunning
		} else {
			readyAt = &now
		}
		if !CanTransition(t.State, next) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.State, next)
		}
		if err := s.repo.Transition(ctx, tx, transitionRecord{
			TaskID: t.ID, ExpectedRevision: t.Revision, From: t.State, To: next,
			UpdatedAt: now, ReadyAt: readyAt,
		}); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"task_id": t.ID, "from": t.State, "to": next, "revision": t.Revision + 1,
		})
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, WorkspaceID: &t.WorkspaceID, Type: "task.resumed",
			AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID,
			RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		return Task{}, err
	}
	return s.repo.Get(ctx, cmd.TaskID)
}

func (s *Service) RequestCompletion(ctx context.Context, cmd TransitionCommand) (Task, error) {
	return s.transition(ctx, cmd, StateCompletionRequested, "task.completion_requested", nil)
}

func (s *Service) BeginVerification(ctx context.Context, cmd TransitionCommand) (Task, error) {
	return s.transitionWithOptionalAttempt(ctx, cmd, StateVerifying, AttemptWaiting, "task.verification_started")
}

// ContinueAfterVerification returns a Task from VERIFYING to RUNNING after an
// independent verifier found the completion evidence insufficient or
// inconclusive. The existing attempt must be WAITING and is resumed atomically.
func (s *Service) ContinueAfterVerification(ctx context.Context, cmd TransitionCommand) (Task, error) {
	return s.transitionWithAttempt(ctx, cmd, StateRunning, AttemptRunning, "task.verification_retry")
}

func (s *Service) MarkBlocked(ctx context.Context, cmd TransitionCommand) (Task, error) {
	return s.transitionWithOptionalAttempt(ctx, cmd, StateBlocked, AttemptWaiting, "task.blocked")
}

func (s *Service) Fail(ctx context.Context, cmd TransitionCommand) (Task, error) {
	return s.transitionWithOptionalAttempt(ctx, cmd, StateFailed, AttemptFailed, "task.failed")
}

func (s *Service) RequestCancel(ctx context.Context, cmd TransitionCommand) (Task, error) {
	return s.transition(ctx, cmd, StateCancelRequested, "task.cancel_requested", func(tr *transitionRecord, now int64) {
		tr.CancelRequestedAt = &now
	})
}

// InterruptForRecovery records that the execution incarnation was lost. The
// durable Task is blocked and an outbox job asks the future RecoveryCoordinator
// to reconcile durable/external state before any new attempt is admitted.
func (s *Service) InterruptForRecovery(ctx context.Context, cmd TransitionCommand) (Task, error) {
 err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx) error{
  return s.InterruptForRecoveryInTransaction(ctx,tx,cmd)
 })
 if err!=nil{return Task{},err}
 return s.repo.Get(ctx,cmd.TaskID)
}

// InterruptForRecoveryInTransaction is a trusted control-plane primitive.
// The caller can atomically mark the associated Worker run interrupted in
// the SAME transaction as its Task, Attempt, audit event and recovery outbox.
// Never expose this method through model-defined tools or direct API calls.
func (s *Service) InterruptForRecoveryInTransaction(ctx context.Context,tx storage.Tx,cmd TransitionCommand) error{
 if tx==nil||strings.TrimSpace(cmd.TaskID)==""||cmd.ExpectedRevision<1{
  return fmt.Errorf("%w: transaction, task id and expected revision are required",ErrInvalidCommand)
 }
 eventID,err:=s.ids.New("evt")
 if err!=nil{return err}
 jobID,err:=s.ids.New("job")
 if err!=nil{return err}
 now:=s.clock.UnixMilli()
	t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
	if err != nil {
		return err
	}
	if t.Revision != cmd.ExpectedRevision {
		return ErrRevisionConflict
	}
	active, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
	if err != nil {
		return err
	}
	if active == nil {
		return ErrNoActiveAttempt
	}
	if active.State != AttemptRunning && active.State != AttemptWaiting && active.State != AttemptQueued && active.State != AttemptCreated {
		return ErrNoActiveAttempt
	}
	if err := s.repo.TransitionAttempt(ctx, tx, active.ID, active.State, AttemptInterrupted, now); err != nil {
		return err
	}
	// Recovery interruption is intentionally not an ordinary state transition:
	// regardless of which active execution state was lost, the Task becomes
	// BLOCKED until reconciliation decides it is safe to re-admit.
	if err := s.repo.Transition(ctx, tx, transitionRecord{
		TaskID: t.ID, ExpectedRevision: t.Revision, From: t.State, To: StateBlocked, UpdatedAt: now,
	}); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"task_id": t.ID, "attempt_id": active.ID, "attempt_number": active.AttemptNumber,
		"previous_task_state": t.State, "reason": cmd.Reason,
	})
	if err := s.events.Append(ctx, tx, event.Event{
		ID: eventID, WorkspaceID: &t.WorkspaceID, Type: "task.attempt_interrupted",
		AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID,
		RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
	}); err != nil {
		return err
	}
	jobPayload, _ := json.Marshal(map[string]any{
		"task_id": t.ID, "attempt_id": active.ID, "reason": cmd.Reason,
	})
	return s.outbox.Enqueue(ctx, tx, outbox.Job{
		ID: jobID, WorkspaceID: &t.WorkspaceID, Type: "task.recovery.required",
		Payload: jobPayload, AvailableAt: now, MaxAttempts: 10, CreatedAt: now,
	})
}

func (s *Service) transition(ctx context.Context, cmd TransitionCommand, next State, eventType string, mutate func(*transitionRecord, int64)) (Task, error) {
	return s.transitionInternal(ctx, cmd, next, eventType, false, "", mutate)
}

func (s *Service) transitionWithAttempt(ctx context.Context, cmd TransitionCommand, next State, attemptNext AttemptState, eventType string) (Task, error) {
	return s.transitionInternal(ctx, cmd, next, eventType, true, attemptNext, nil)
}

func (s *Service) transitionWithOptionalAttempt(ctx context.Context, cmd TransitionCommand, next State, attemptNext AttemptState, eventType string) (Task, error) {
	return s.transitionInternal(ctx, cmd, next, eventType, false, attemptNext, nil)
}

func (s *Service) transitionInternal(
	ctx context.Context,
	cmd TransitionCommand,
	next State,
	eventType string,
	requireAttempt bool,
	attemptNext AttemptState,
	mutate func(*transitionRecord, int64),
) (Task, error) {
	if strings.TrimSpace(cmd.TaskID) == "" || cmd.ExpectedRevision < 1 {
		return Task{}, fmt.Errorf("%w: task id and expected revision are required", ErrInvalidCommand)
	}
	if !ValidState(next) {
		return Task{}, fmt.Errorf("%w: invalid target state %q", ErrInvalidCommand, next)
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Task{}, err
	}
	now := s.clock.UnixMilli()

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
		if err != nil {
			return err
		}
		if t.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if !CanTransition(t.State, next) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.State, next)
		}
		if attemptNext != "" {
			active, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
			if err != nil {
				return err
			}
			if active == nil {
				if requireAttempt {
					return ErrNoActiveAttempt
				}
			} else if active.State != attemptNext {
				if err := s.repo.TransitionAttempt(ctx, tx, active.ID, active.State, attemptNext, now); err != nil {
					return err
				}
			}
		}
		tr := transitionRecord{TaskID: t.ID, ExpectedRevision: t.Revision, From: t.State, To: next, UpdatedAt: now}
		if mutate != nil {
			mutate(&tr, now)
		}
		if err := s.repo.Transition(ctx, tx, tr); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"task_id": t.ID, "from": t.State, "to": next,
			"reason": cmd.Reason, "revision": t.Revision + 1,
		})
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, WorkspaceID: &t.WorkspaceID, Type: eventType,
			AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID,
			RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		return Task{}, err
	}
	return s.repo.Get(ctx, cmd.TaskID)
}

func cloneJSON(in json.RawMessage) json.RawMessage {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

// IsRevisionConflict allows HTTP/API layers to map optimistic concurrency
// failures without depending on string matching.
func IsRevisionConflict(err error) bool { return errors.Is(err, ErrRevisionConflict) }

// CompleteVerified is the only task-completion command. It requires a valid
// checkpoint backed by a passed Verification and refuses completion while an
// unknown mutation outcome exists for the Task.
func (s *Service) CompleteVerified(ctx context.Context, cmd CompleteCommand) (Task, error) {
	if strings.TrimSpace(cmd.TaskID) == "" || cmd.ExpectedRevision < 1 || strings.TrimSpace(cmd.CheckpointID) == "" {
		return Task{}, fmt.Errorf("%w: task id, expected revision and checkpoint id are required", ErrInvalidCommand)
	}
	if len(cmd.Result) == 0 {
		cmd.Result = json.RawMessage(`{}`)
	}
	if !json.Valid(cmd.Result) {
		return Task{}, fmt.Errorf("%w: result must be valid JSON", ErrInvalidCommand)
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return Task{}, err
	}
	now := s.clock.UnixMilli()

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		t, err := s.repo.GetForUpdate(ctx, tx, cmd.TaskID)
		if err != nil {
			return err
		}
		if t.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if t.State != StateVerifying || !CanTransition(t.State, StateComplete) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.State, StateComplete)
		}
		valid, err := s.repo.ValidCompletionEvidence(ctx, tx, t.ID, cmd.CheckpointID)
		if err != nil {
			return err
		}
		if !valid {
			return ErrVerificationRequired
		}
		active, err := s.repo.ActiveAttempt(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		if active != nil {
			if active.State != AttemptWaiting {
				return ErrActiveAttempt
			}
			if err := s.repo.TransitionAttempt(ctx, tx, active.ID, active.State, AttemptSucceeded, now); err != nil {
				return err
			}
		}
		if err := s.repo.Transition(ctx, tx, transitionRecord{
			TaskID: t.ID, ExpectedRevision: t.Revision, From: t.State, To: StateComplete,
			UpdatedAt: now, Result: append([]byte(nil), cmd.Result...),
		}); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"task_id": t.ID, "from": t.State, "to": StateComplete,
			"checkpoint_id": cmd.CheckpointID, "revision": t.Revision + 1,
		})
		return s.events.Append(ctx, tx, event.Event{
			ID: eventID, WorkspaceID: &t.WorkspaceID, Type: "task.completed",
			AggregateType: "task", AggregateID: t.ID, ActorPrincipalID: cmd.ActorPrincipalID,
			RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now,
		})
	})
	if err != nil {
		return Task{}, err
	}
	return s.repo.Get(ctx, cmd.TaskID)
}
