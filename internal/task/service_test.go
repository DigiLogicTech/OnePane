package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/outbox"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type fixedClock struct{ ms int64 }

func (f fixedClock) Now() time.Time   { return time.UnixMilli(f.ms).UTC() }
func (f fixedClock) UnixMilli() int64 { return f.ms }

type fakeTx struct{}

func (fakeTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	panic("unexpected raw SQL in fake transaction")
}
func (fakeTx) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	panic("unexpected raw SQL in fake transaction")
}
func (fakeTx) QueryRowContext(context.Context, string, ...any) *sql.Row {
	panic("unexpected raw SQL in fake transaction")
}

type fakeTransactor struct{}

func (fakeTransactor) Within(ctx context.Context, fn func(context.Context, storage.Tx) error) error {
	return fn(ctx, fakeTx{})
}

type fakeRepo struct {
	tasks              map[string]Task
	attempts           map[string]Attempt
	completionEvidence bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{tasks: map[string]Task{}, attempts: map[string]Attempt{}}
}
func (r *fakeRepo) Get(_ context.Context, id string) (Task, error) {
	t, ok := r.tasks[id]
	if !ok {
		return Task{}, sql.ErrNoRows
	}
	return t, nil
}
func (r *fakeRepo) List(_ context.Context, workspaceID string, limit int) ([]Task, error) {
	out := make([]Task, 0)
	for _, t := range r.tasks {
		if t.WorkspaceID == workspaceID && t.ArchivedAt == nil {
			out = append(out, t)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *fakeRepo) ListArchived(_ context.Context, workspaceID string, limit int) ([]Task, error) {
	out := make([]Task, 0)
	for _, t := range r.tasks {
		if t.WorkspaceID == workspaceID && t.ArchivedAt != nil {
			out = append(out, t)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *fakeRepo) GetForUpdate(ctx context.Context, _ storage.Tx, id string) (Task, error) {
	return r.Get(ctx, id)
}
func (r *fakeRepo) Insert(_ context.Context, _ storage.Tx, t Task) error {
	r.tasks[t.ID] = t
	return nil
}
func (r *fakeRepo) SetArchived(_ context.Context, _ storage.Tx, taskID string, expectedRevision int64, archivedAt *int64, updatedAt int64) error {
	t, ok := r.tasks[taskID]
	if !ok {
		return sql.ErrNoRows
	}
	if t.Revision != expectedRevision {
		return ErrRevisionConflict
	}
	t.ArchivedAt = archivedAt
	t.Revision++
	t.UpdatedAt = updatedAt
	r.tasks[taskID] = t
	return nil
}
func (r *fakeRepo) Transition(_ context.Context, _ storage.Tx, tr transitionRecord) error {
	t, ok := r.tasks[tr.TaskID]
	if !ok {
		return sql.ErrNoRows
	}
	if t.Revision != tr.ExpectedRevision || t.State != tr.From {
		return ErrRevisionConflict
	}
	t.State = tr.To
	t.Revision++
	t.UpdatedAt = tr.UpdatedAt
	if tr.ReadyAt != nil {
		v := *tr.ReadyAt
		t.ReadyAt = &v
	}
	if tr.CancelRequestedAt != nil {
		v := *tr.CancelRequestedAt
		t.CancelRequestedAt = &v
	}
	if tr.Result != nil {
		t.Result = append([]byte(nil), tr.Result...)
	}
	r.tasks[t.ID] = t
	return nil
}
func (r *fakeRepo) NextAttemptNumber(_ context.Context, _ storage.Tx, taskID string) (int64, error) {
	var max int64
	for _, a := range r.attempts {
		if a.TaskID == taskID && a.AttemptNumber > max {
			max = a.AttemptNumber
		}
	}
	return max + 1, nil
}
func (r *fakeRepo) InsertAttempt(_ context.Context, _ storage.Tx, a Attempt) error {
	r.attempts[a.ID] = a
	return nil
}
func (r *fakeRepo) ActiveAttempt(_ context.Context, _ storage.Tx, taskID string) (*Attempt, error) {
	var list []Attempt
	for _, a := range r.attempts {
		if a.TaskID != taskID {
			continue
		}
		if a.State == AttemptCreated || a.State == AttemptQueued || a.State == AttemptRunning || a.State == AttemptWaiting {
			list = append(list, a)
		}
	}
	if len(list) == 0 {
		return nil, nil
	}
	sort.Slice(list, func(i, j int) bool { return list[i].AttemptNumber > list[j].AttemptNumber })
	a := list[0]
	return &a, nil
}
func (r *fakeRepo) TransitionAttempt(_ context.Context, _ storage.Tx, id string, from, to AttemptState, at int64) error {
	a, ok := r.attempts[id]
	if !ok || a.State != from {
		return ErrInvalidAttempt
	}
	if !CanTransitionAttempt(from, to) {
		return ErrInvalidAttempt
	}
	a.State = to
	if to == AttemptRunning && a.StartedAt == nil {
		v := at
		a.StartedAt = &v
	}
	if to == AttemptSucceeded || to == AttemptFailed || to == AttemptCancelled || to == AttemptInterrupted {
		v := at
		a.EndedAt = &v
	}
	r.attempts[id] = a
	return nil
}

func (r *fakeRepo) ValidCompletionEvidence(_ context.Context, _ storage.Tx, taskID, checkpointID string) (bool, error) {
	return r.completionEvidence, nil
}

type fakeEvents struct{ events []event.Event }

func (f *fakeEvents) Append(_ context.Context, _ storage.Tx, e event.Event) error {
	f.events = append(f.events, e)
	return nil
}

type fakeOutbox struct{ jobs []outbox.Job }

func (f *fakeOutbox) Enqueue(_ context.Context, _ storage.Tx, j outbox.Job) error {
	f.jobs = append(f.jobs, j)
	return nil
}

func newTestService(ms int64) (*Service, *fakeRepo, *fakeEvents, *fakeOutbox) {
	r := newFakeRepo()
	ev := &fakeEvents{}
	ob := &fakeOutbox{}
	return newService(fakeTransactor{}, r, ev, ob, fixedClock{ms: ms}), r, ev, ob
}

func TestCreateTaskIsDurableCommandWithEventAndAdmissionJob(t *testing.T) {
	s, r, ev, ob := newTestService(1_700_000_000_000)
	got, err := s.Create(context.Background(), CreateCommand{
		WorkspaceID: "ws_test", Objective: "Inspect service state", Completion: json.RawMessage(`{"state":"observed"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateCreated || got.Revision != 1 {
		t.Fatalf("unexpected task: %+v", got)
	}
	if len(r.tasks) != 1 {
		t.Fatalf("tasks=%d want 1", len(r.tasks))
	}
	if len(ev.events) != 1 || ev.events[0].Type != "task.created" {
		t.Fatalf("events=%+v", ev.events)
	}
	if len(ob.jobs) != 1 || ob.jobs[0].Type != "task.admission.evaluate" {
		t.Fatalf("jobs=%+v", ob.jobs)
	}
}

func TestOptimisticRevisionRejectsStaleTransition(t *testing.T) {
	s, _, _, _ := newTestService(1_700_000_000_000)
	task, err := s.Create(context.Background(), CreateCommand{WorkspaceID: "ws", Objective: "x"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.MarkReady(context.Background(), TransitionCommand{TaskID: task.ID, ExpectedRevision: 2})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("err=%v want revision conflict", err)
	}
}

func TestStartCreatesOneExecutionAttemptAndRejectsDuplicateStart(t *testing.T) {
	s, r, _, _ := newTestService(1_700_000_000_000)
	task, err := s.Create(context.Background(), CreateCommand{WorkspaceID: "ws", Objective: "x"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = s.MarkReady(context.Background(), TransitionCommand{TaskID: task.ID, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	task, attempt, err := s.Start(context.Background(), StartCommand{TaskID: task.ID, ExpectedRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	if task.State != StateRunning || task.Revision != 3 {
		t.Fatalf("task=%+v", task)
	}
	if attempt.State != AttemptRunning || attempt.AttemptNumber != 1 {
		t.Fatalf("attempt=%+v", attempt)
	}
	_, _, err = s.Start(context.Background(), StartCommand{TaskID: task.ID, ExpectedRevision: 3})
	if err == nil {
		t.Fatal("second start unexpectedly succeeded")
	}
	if len(r.attempts) != 1 {
		t.Fatalf("attempts=%d want 1", len(r.attempts))
	}
}

func TestPauseAndResumeKeepsSameAttempt(t *testing.T) {
	s, r, _, _ := newTestService(1_700_000_000_000)
	task, _ := s.Create(context.Background(), CreateCommand{WorkspaceID: "ws", Objective: "x"})
	task, _ = s.MarkReady(context.Background(), TransitionCommand{TaskID: task.ID, ExpectedRevision: 1})
	task, attempt, err := s.Start(context.Background(), StartCommand{TaskID: task.ID, ExpectedRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	task, err = s.Pause(context.Background(), TransitionCommand{TaskID: task.ID, ExpectedRevision: 3})
	if err != nil {
		t.Fatal(err)
	}
	if task.State != StatePaused {
		t.Fatalf("state=%s", task.State)
	}
	if r.attempts[attempt.ID].State != AttemptWaiting {
		t.Fatalf("attempt state=%s", r.attempts[attempt.ID].State)
	}
	task, err = s.Resume(context.Background(), TransitionCommand{TaskID: task.ID, ExpectedRevision: 4})
	if err != nil {
		t.Fatal(err)
	}
	if task.State != StateRunning {
		t.Fatalf("state=%s", task.State)
	}
	if r.attempts[attempt.ID].State != AttemptRunning {
		t.Fatalf("attempt state=%s", r.attempts[attempt.ID].State)
	}
	if len(r.attempts) != 1 {
		t.Fatalf("attempts=%d want 1", len(r.attempts))
	}
}

func TestInterruptedAttemptBlocksTaskAndQueuesRecovery(t *testing.T) {
	s, r, ev, ob := newTestService(1_700_000_000_000)
	task, _ := s.Create(context.Background(), CreateCommand{WorkspaceID: "ws", Objective: "x"})
	task, _ = s.MarkReady(context.Background(), TransitionCommand{TaskID: task.ID, ExpectedRevision: 1})
	task, attempt, err := s.Start(context.Background(), StartCommand{TaskID: task.ID, ExpectedRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	task, err = s.InterruptForRecovery(context.Background(), TransitionCommand{TaskID: task.ID, ExpectedRevision: 3, Reason: "host_restart"})
	if err != nil {
		t.Fatal(err)
	}
	if task.State != StateBlocked || task.Revision != 4 {
		t.Fatalf("task=%+v", task)
	}
	if r.attempts[attempt.ID].State != AttemptInterrupted {
		t.Fatalf("attempt=%+v", r.attempts[attempt.ID])
	}
	if len(ob.jobs) != 2 || ob.jobs[1].Type != "task.recovery.required" {
		t.Fatalf("jobs=%+v", ob.jobs)
	}
	if ev.events[len(ev.events)-1].Type != "task.attempt_interrupted" {
		t.Fatalf("last event=%s", ev.events[len(ev.events)-1].Type)
	}
}

func TestCompleteVerifiedRequiresCheckpointEvidence(t *testing.T) {
	s, r, ev, _ := newTestService(1_700_000_000_000)
	taskID := "task_verified"
	attemptID := "attempt_verified"
	r.tasks[taskID] = Task{ID: taskID, WorkspaceID: "ws", Objective: "x", State: StateVerifying, Revision: 9, Completion: json.RawMessage(`{}`)}
	r.attempts[attemptID] = Attempt{ID: attemptID, TaskID: taskID, AttemptNumber: 1, State: AttemptWaiting, Metadata: json.RawMessage(`{}`)}

	_, err := s.CompleteVerified(context.Background(), CompleteCommand{TaskID: taskID, ExpectedRevision: 9, CheckpointID: "cp"})
	if !errors.Is(err, ErrVerificationRequired) {
		t.Fatalf("err=%v want verification required", err)
	}
	if r.tasks[taskID].State != StateVerifying || r.attempts[attemptID].State != AttemptWaiting {
		t.Fatal("failed completion mutated task/attempt")
	}

	r.completionEvidence = true
	got, err := s.CompleteVerified(context.Background(), CompleteCommand{TaskID: taskID, ExpectedRevision: 9, CheckpointID: "cp", Result: json.RawMessage(`{"ok":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateComplete || r.attempts[attemptID].State != AttemptSucceeded {
		t.Fatalf("task=%s attempt=%s", got.State, r.attempts[attemptID].State)
	}
	if len(ev.events) == 0 || ev.events[len(ev.events)-1].Type != "task.completed" {
		t.Fatalf("events=%+v", ev.events)
	}
}

type denyAdmissionGuard struct{ err error }

func (g denyAdmissionGuard) AllowReady(context.Context, Task) error { return g.err }
func (g denyAdmissionGuard) AllowStart(context.Context, Task) error { return g.err }

func TestAdmissionGuardBlocksReadyAndStart(t *testing.T) {
	s, r, _, _ := newTestService(1_700_000_000_000)
	guardErr := errors.New("plan required")
	s.SetAdmissionGuard(denyAdmissionGuard{err: guardErr})
	taskRow, err := s.Create(context.Background(), CreateCommand{WorkspaceID: "ws", Objective: "team task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkReady(context.Background(), TransitionCommand{TaskID: taskRow.ID, ExpectedRevision: 1}); !errors.Is(err, guardErr) {
		t.Fatalf("MarkReady err=%v", err)
	}
	if got := r.tasks[taskRow.ID]; got.State != StateCreated || got.Revision != 1 {
		t.Fatalf("guarded ready mutated task: %+v", got)
	}

	// Simulate a previously-ready task to prove Start has its own fail-closed guard.
	ready := r.tasks[taskRow.ID]
	ready.State = StateReady
	ready.Revision = 2
	r.tasks[taskRow.ID] = ready
	if _, _, err := s.Start(context.Background(), StartCommand{TaskID: taskRow.ID, ExpectedRevision: 2}); !errors.Is(err, guardErr) {
		t.Fatalf("Start err=%v", err)
	}
	if len(r.attempts) != 0 {
		t.Fatalf("guarded start created attempt: %d", len(r.attempts))
	}
}


func TestArchiveLifecyclePreservesTaskHistoryAndScope(t *testing.T) {
	s, r, ev, _ := newTestService(1_700_000_000_000)
	projectID, projectWorkspaceID := "proj_test", "pws_test"
	created, err := s.Create(context.Background(), CreateCommand{
		WorkspaceID: "ws", ProjectID: &projectID, ProjectWorkspaceID: &projectWorkspaceID, Objective: "archive me",
	})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := s.Archive(context.Background(), ArchiveCommand{TaskID: created.ID, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if archived.ArchivedAt == nil || archived.Revision != 2 || archived.ProjectWorkspaceID == nil || *archived.ProjectWorkspaceID != projectWorkspaceID {
		t.Fatalf("archived task=%+v", archived)
	}
	active, _ := s.List(context.Background(), "ws", 10)
	if len(active) != 0 {
		t.Fatalf("active tasks=%d want 0", len(active))
	}
	archivedRows, _ := s.ListArchived(context.Background(), "ws", 10)
	if len(archivedRows) != 1 || archivedRows[0].ID != created.ID {
		t.Fatalf("archived rows=%+v", archivedRows)
	}
	restored, err := s.Unarchive(context.Background(), ArchiveCommand{TaskID: created.ID, ExpectedRevision: archived.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if restored.ArchivedAt != nil || restored.Revision != 3 {
		t.Fatalf("restored task=%+v", restored)
	}
	if got := r.tasks[created.ID]; got.ProjectID == nil || got.ProjectWorkspaceID == nil {
		t.Fatalf("scope lost: %+v", got)
	}
	if len(ev.events) < 3 || ev.events[len(ev.events)-2].Type != "task.archived" || ev.events[len(ev.events)-1].Type != "task.unarchived" {
		t.Fatalf("events=%+v", ev.events)
	}
}
