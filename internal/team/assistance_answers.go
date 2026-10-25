package team

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// AssistanceAnswer is final authored input for one immutable request. It is
// neither an execution approval nor a recovery decision. RequestID retains
// the exact original Task/Attempt/Worker/wait-step and accepted assignment.
type AssistanceAnswer struct {
	AnswerID          string `json:"answer_id"`
	RequestID         string `json:"request_id"`
	AuthorPrincipalID string `json:"author_principal_id"`
	IdempotencyKey    string `json:"idempotency_key"`
	Text              string `json:"text"`
	TaskRevision      int64  `json:"task_revision"`
	WorkerRevision    int64  `json:"worker_revision"`
	CreatedAt         int64  `json:"created_at"`
}

// AssistanceAnswerObservation retains separate original-question and authored
// human-answer provenance. Reading either grants no continuation authority.
// Both values are comparable so a private boundary can recollect exact state.
type AssistanceAnswerObservation struct {
	Request AssistanceRequest `json:"request"`
	Answer  AssistanceAnswer  `json:"answer"`
}

// AnswerAssistanceCommand comes from a trusted domain caller. An HTTP
// boundary must independently authenticate the original credential, require
// current human team.write and relevant read permissions, and derive the
// author from that credential. This service does not authorize API credentials.
type AnswerAssistanceCommand struct {
	WorkspaceID, SessionID, RequestID            string
	TaskID, AttemptID, RunID, WaitStepID         string
	ActorPrincipalID, IdempotencyKey, Text       string
	ExpectedTaskRevision, ExpectedWorkerRevision int64
	// Trusted boundaries can require their original credential and capabilities
	// inside this transaction, after current domain scope/human validation and
	// before any answer lookup/write. Invoked on retries too. The callback must
	// inspect through this supplied Tx; it must not open another transaction or
	// refresh a browser session on an independent database connection.
	AuthorizeInTransaction func(context.Context, storage.Tx, AssistanceRequest) error
}

const assistanceAnswerReadSQL = `SELECT id,request_id,author_principal_id,idempotency_key,answer,task_revision,worker_revision,created_at
 FROM team_assistance_answers WHERE request_id=?
 AND length(CAST(id AS BLOB)) BETWEEN 1 AND 256
 AND length(CAST(request_id AS BLOB)) BETWEEN 1 AND 256
 AND length(CAST(author_principal_id AS BLOB)) BETWEEN 1 AND 256
 AND length(CAST(idempotency_key AS BLOB)) BETWEEN 1 AND 128
 AND length(CAST(answer AS BLOB)) BETWEEN 1 AND 4096
 AND task_revision BETWEEN 1 AND 9007199254740991
 AND worker_revision BETWEEN 1 AND 9007199254740991
 AND created_at BETWEEN 1 AND 9007199254740991`

// AssistanceAnswer observes one retained answer alongside the exact current
// original waiting contribution, in one transaction. Held Teams remain readable;
// a missing answer is not fabricated. It writes nothing and never releases a
// hold, resumes an Attempt, or establishes reusable authorization.
func (s *Service) AssistanceAnswer(ctx context.Context, workspaceID, sessionID, requestID, readerPrincipalID string) (AssistanceAnswerObservation, error) {
	var out AssistanceAnswerObservation
	if s == nil || s.db == nil || s.tx == nil {
		return out, ErrInvalid
	}
	for _, value := range []string{workspaceID, sessionID, requestID, readerPrincipalID} {
		if value == "" || len(value) > 256 || !utf8.ValidString(value) {
			return out, ErrInvalid
		}
	}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var err error
		out.Request, err = s.assistanceRequestTx(ctx, tx, workspaceID, sessionID, requestID, readerPrincipalID)
		if err != nil {
			return err
		}
		answer := &out.Answer
		err = tx.QueryRowContext(ctx, assistanceAnswerReadSQL, requestID).Scan(&answer.AnswerID, &answer.RequestID, &answer.AuthorPrincipalID, &answer.IdempotencyKey, &answer.Text, &answer.TaskRevision, &answer.WorkerRevision, &answer.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			// SQL budgets suppress malformed rows before materialization. Keep
			// genuine absence distinct from a retained but invalid record.
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_assistance_answers WHERE request_id=?)`, requestID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return ErrInvalid
			}
			return sql.ErrNoRows
		}
		if err != nil {
			return err
		}
		for _, value := range []string{answer.AnswerID, answer.RequestID, answer.AuthorPrincipalID, answer.IdempotencyKey, answer.Text} {
			if !utf8.ValidString(value) {
				return ErrInvalid
			}
		}
		if answer.RequestID != requestID || strings.TrimSpace(answer.IdempotencyKey) == "" || strings.TrimSpace(answer.Text) == "" {
			return ErrInvalid
		}
		return nil
	})
	if err != nil {
		return AssistanceAnswerObservation{}, err
	}
	return out, nil
}

// AnswerAssistance atomically retains one final answer and its audit. It never
// resumes an owner, releases a Team hold, changes admission or refunds accounting.
// Accepted graph, original scope and human membership are observed in the SAME
// transaction as both writes. The API supplies original-credential authority
// through AuthorizeInTransaction; UI and explicit Continue are separate work.
func (s *Service) AnswerAssistance(ctx context.Context, c AnswerAssistanceCommand) (AssistanceAnswer, error) {
	var out AssistanceAnswer
	if s == nil || s.db == nil || s.tx == nil || s.clock == nil {
		return out, ErrInvalid
	}
	for _, value := range []string{c.WorkspaceID, c.SessionID, c.RequestID, c.TaskID, c.AttemptID, c.RunID, c.WaitStepID, c.ActorPrincipalID} {
		if value == "" || len(value) > 256 || !utf8.ValidString(value) {
			return out, ErrInvalid
		}
	}
	if strings.TrimSpace(c.IdempotencyKey) == "" || len(c.IdempotencyKey) > 128 || !utf8.ValidString(c.IdempotencyKey) ||
		strings.TrimSpace(c.Text) == "" || len(c.Text) > 4096 || !utf8.ValidString(c.Text) ||
		c.ExpectedTaskRevision <= 0 || c.ExpectedTaskRevision > 1<<53-1 || c.ExpectedWorkerRevision <= 0 || c.ExpectedWorkerRevision > 1<<53-1 {
		return out, ErrInvalid
	}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		request, err := s.assistanceRequestTx(ctx, tx, c.WorkspaceID, c.SessionID, c.RequestID, c.ActorPrincipalID)
		if err != nil {
			return err
		}
		if request.TaskID != c.TaskID || request.AttemptID != c.AttemptID || request.RunID != c.RunID || request.WaitStepID != c.WaitStepID ||
			request.TaskRevision != c.ExpectedTaskRevision || request.WorkerRevision != c.ExpectedWorkerRevision {
			return ErrInvalid
		}
		var human bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM principals p
 JOIN workspace_memberships m ON m.principal_id=p.id AND m.workspace_id=? AND m.status='active'
 JOIN workspaces w ON w.id=m.workspace_id AND w.status='active'
 WHERE p.id=? AND p.principal_type='human' AND p.status='active')`, c.WorkspaceID, c.ActorPrincipalID).Scan(&human); err != nil {
			return err
		}
		if !human {
			return ErrHumanRequired
		}
		if c.AuthorizeInTransaction != nil {
			if err := c.AuthorizeInTransaction(ctx, tx, request); err != nil {
				return err
			}
		}
		err = tx.QueryRowContext(ctx, assistanceAnswerReadSQL, c.RequestID).Scan(&out.AnswerID, &out.RequestID, &out.AuthorPrincipalID, &out.IdempotencyKey, &out.Text, &out.TaskRevision, &out.WorkerRevision, &out.CreatedAt)
		if err == nil {
			if !utf8.ValidString(out.AnswerID) || out.RequestID != c.RequestID || out.AuthorPrincipalID != c.ActorPrincipalID || out.IdempotencyKey != c.IdempotencyKey || out.Text != c.Text ||
				out.TaskRevision != c.ExpectedTaskRevision || out.WorkerRevision != c.ExpectedWorkerRevision {
				return ErrInvalid
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id, err := s.ids.New("thanswer")
		if err != nil {
			return err
		}
		now := s.clock.UnixMilli()
		if now <= 0 || now > 1<<53-1 {
			return ErrInvalid
		}
		out = AssistanceAnswer{AnswerID: id, RequestID: c.RequestID, AuthorPrincipalID: c.ActorPrincipalID, IdempotencyKey: c.IdempotencyKey, Text: c.Text, TaskRevision: c.ExpectedTaskRevision, WorkerRevision: c.ExpectedWorkerRevision, CreatedAt: now}
		if _, err = tx.ExecContext(ctx, `INSERT INTO team_assistance_answers(request_id,id,author_principal_id,idempotency_key,answer,task_revision,worker_revision,created_at) VALUES(?,?,?,?,?,?,?,?)`, out.RequestID, out.AnswerID, out.AuthorPrincipalID, out.IdempotencyKey, out.Text, out.TaskRevision, out.WorkerRevision, out.CreatedAt); err != nil {
			return err
		}
		return s.emit(ctx, tx, c.WorkspaceID, "team.assistance_answered", "team_session", c.SessionID, &c.ActorPrincipalID, map[string]any{
			"answer_id": out.AnswerID, "request_id": c.RequestID, "plan_id": request.PlanID,
			"task_id": c.TaskID, "attempt_id": c.AttemptID, "run_id": c.RunID, "wait_step_id": c.WaitStepID,
			"task_revision": out.TaskRevision, "worker_revision": out.WorkerRevision,
		})
	})
	if err != nil {
		return AssistanceAnswer{}, err
	}
	return out, nil
}
