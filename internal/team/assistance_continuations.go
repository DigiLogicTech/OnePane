package team

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/featurepolicy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// AssistanceContinuationWorker owns Worker journals and the exact original
// Task/Attempt transition. Implementations must use only the supplied transaction
// and repeat ordinary execution admission. The caller retains the receipt and
// Team audit in that same transaction; an error rolls back every mutation.
type AssistanceContinuationWorker interface {
	ContinueAssistanceInTransaction(context.Context, storage.Tx, AssistanceContinuationCommand) (string, error)
}

type AssistanceContinuationCommand struct {
	Request          AssistanceRequest
	Answer           AssistanceAnswer
	ActorPrincipalID string
}

// SetAssistanceContinuationWorker wires a trusted collaborator during startup,
// before the service is published to concurrent callers.
func (s *Service) SetAssistanceContinuationWorker(worker AssistanceContinuationWorker) {
	s.assistanceWorker = worker
}

type ContinueAssistanceCommand struct {
	WorkspaceID, SessionID, RequestID, AnswerID  string
	TaskID, AttemptID, RunID, WaitStepID         string
	ActorPrincipalID                             string
	ExpectedTaskRevision, ExpectedWorkerRevision int64
	// Mandatory original-credential check. It must query through this Tx and
	// require current human team/task writes plus relevant Project permissions.
	AuthorizeInTransaction func(context.Context, storage.Tx, AssistanceRequest) error
}

// AssistanceContinuation records one explicit approval of the original owner.
// Revisions are the observed waiting revisions, before the atomic transition.
// A receipt is provenance, never reusable permission to replay or release holds.
type AssistanceContinuation struct {
	ContinuationID   string `json:"continuation_id"`
	RequestID        string `json:"request_id"`
	AnswerID         string `json:"answer_id"`
	ActorPrincipalID string `json:"actor_principal_id"`
	ContinueStepID   string `json:"continue_step_id"`
	TaskRevision     int64  `json:"task_revision"`
	WorkerRevision   int64  `json:"worker_revision"`
	CreatedAt        int64  `json:"created_at"`
}

// ContinueAssistance explicitly approves one retained answer for its exact
// original waiting contribution. This action creates no new Attempt, releases
// no hold and refunds no accounting. A repeated committed request conflicts;
// it cannot start a second execution if its first response was interrupted.
func (s *Service) ContinueAssistance(ctx context.Context, c ContinueAssistanceCommand) (AssistanceContinuation, error) {
	var out AssistanceContinuation
	if s == nil || s.db == nil || s.tx == nil || s.clock == nil || s.assistanceWorker == nil || c.AuthorizeInTransaction == nil {
		return out, ErrInvalid
	}
	for _, value := range []string{c.WorkspaceID, c.SessionID, c.RequestID, c.AnswerID, c.TaskID, c.AttemptID, c.RunID, c.WaitStepID, c.ActorPrincipalID} {
		if value == "" || len(value) > 256 || !utf8.ValidString(value) {
			return out, ErrInvalid
		}
	}
	if c.ExpectedTaskRevision < 1 || c.ExpectedTaskRevision > 1<<53-1 || c.ExpectedWorkerRevision < 1 || c.ExpectedWorkerRevision > 1<<53-1 {
		return out, ErrInvalid
	}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		request, err := s.assistanceRequestTx(ctx, tx, c.WorkspaceID, c.SessionID, c.RequestID, c.ActorPrincipalID)
		if err != nil {
			return err
		}
		if request.TaskState != "waiting_approval" || request.TaskID != c.TaskID || request.AttemptID != c.AttemptID || request.RunID != c.RunID || request.WaitStepID != c.WaitStepID || request.TaskRevision != c.ExpectedTaskRevision || request.WorkerRevision != c.ExpectedWorkerRevision {
			return ErrInvalid
		}
		var human bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM principals p
 JOIN workspace_memberships m ON m.principal_id=p.id AND m.workspace_id=? AND m.status='active'
 WHERE p.id=? AND p.principal_type='human' AND p.status='active')`, c.WorkspaceID, c.ActorPrincipalID).Scan(&human); err != nil {
			return err
		}
		if !human {
			return ErrHumanRequired
		}
		if err = c.AuthorizeInTransaction(ctx, tx, request); err != nil {
			return err
		}
		var answer AssistanceAnswer
		err = tx.QueryRowContext(ctx, assistanceAnswerReadSQL, c.RequestID).Scan(&answer.AnswerID, &answer.RequestID, &answer.AuthorPrincipalID, &answer.IdempotencyKey, &answer.Text, &answer.TaskRevision, &answer.WorkerRevision, &answer.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		if answer.AnswerID != c.AnswerID || answer.RequestID != c.RequestID || answer.TaskRevision > request.TaskRevision || answer.WorkerRevision > request.WorkerRevision {
			return ErrInvalid
		}
		for _, value := range []string{answer.AnswerID, answer.RequestID, answer.AuthorPrincipalID, answer.IdempotencyKey, answer.Text} {
			if !utf8.ValidString(value) {
				return ErrInvalid
			}
		}
		if strings.TrimSpace(answer.Text) == "" || strings.TrimSpace(answer.IdempotencyKey) == "" {
			return ErrInvalid
		}
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM principals p JOIN workspace_memberships m ON m.principal_id=p.id AND m.workspace_id=? AND m.status='active' WHERE p.id=? AND p.principal_type='human' AND p.status='active')`, c.WorkspaceID, answer.AuthorPrincipalID).Scan(&human); err != nil {
			return err
		}
		if !human {
			return ErrHumanRequired
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_assistance_continuations WHERE request_id=?)`, c.RequestID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrInvalid
		}
		allowed, err := (featurepolicy.StoredScopes{DB: s.db}).AuthorizeTaskTx(ctx, tx, c.WorkspaceID, c.TaskID, "tasks.autonomous")
		if err != nil {
			return err
		}
		if !allowed {
			return ErrInvalid
		}
		step, err := s.assistanceWorker.ContinueAssistanceInTransaction(ctx, tx, AssistanceContinuationCommand{Request: request, Answer: answer, ActorPrincipalID: c.ActorPrincipalID})
		if err != nil {
			return err
		}
		if step == "" || len(step) > 256 || !utf8.ValidString(step) {
			return ErrInvalid
		}
		id, err := s.ids.New("thcontinue")
		if err != nil {
			return err
		}
		now := s.clock.UnixMilli()
		if now < 1 || now > 1<<53-1 {
			return ErrInvalid
		}
		out = AssistanceContinuation{ContinuationID: id, RequestID: c.RequestID, AnswerID: answer.AnswerID, ActorPrincipalID: c.ActorPrincipalID, ContinueStepID: step, TaskRevision: request.TaskRevision, WorkerRevision: request.WorkerRevision, CreatedAt: now}
		if _, err = tx.ExecContext(ctx, `INSERT INTO team_assistance_continuations(request_id,id,answer_id,actor_principal_id,continue_step_id,task_revision,worker_revision,created_at) VALUES(?,?,?,?,?,?,?,?)`, out.RequestID, out.ContinuationID, out.AnswerID, out.ActorPrincipalID, out.ContinueStepID, out.TaskRevision, out.WorkerRevision, out.CreatedAt); err != nil {
			return err
		}
		return s.emit(ctx, tx, c.WorkspaceID, "team.assistance_continued", "team_session", c.SessionID, &c.ActorPrincipalID, map[string]any{"continuation_id": id, "request_id": c.RequestID, "answer_id": answer.AnswerID, "task_id": c.TaskID, "attempt_id": c.AttemptID, "run_id": c.RunID, "wait_step_id": c.WaitStepID, "continue_step_id": step, "task_revision": request.TaskRevision, "worker_revision": request.WorkerRevision})
	})
	if err != nil {
		return AssistanceContinuation{}, err
	}
	return out, nil
}
