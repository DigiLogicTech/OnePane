package agentworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

var errProposalRoutingDisabled = errors.New("automatic escalation is disabled by workspace routing policy")

// A recorded response may change future reasoning only while its exact original
// execution owner is still active. Commit the continuation, bounded counter and
// immutable observation/audit together; journal failure must not silently apply
// an unaudited route or revised instructions. No model/tool is dispatched here.
func (s *Service) commitProposalContinuation(ctx context.Context, run Run, kind, reason, instructions string) error {
	if s == nil || s.tx == nil || s.clock == nil || run.ID == "" || run.WorkspaceID == "" || run.TaskID == "" || run.AttemptID == "" || run.Revision < 1 {
		return ErrInvalidWorkerState
	}
	writePrepared := false
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var route string
		var replans, maxReplans, escalations, maxEscalations int64
		err := tx.QueryRowContext(ctx, `SELECT r.route_policy_json,r.replan_count,r.max_replans,r.escalation_count,r.max_escalations
   FROM agent_worker_runs r WHERE r.id=? AND r.workspace_id=? AND r.task_id=? AND r.attempt_id=? AND r.revision=? AND r.status='running' AND `+activeWorkerExecutionBindingPredicate,
			run.ID, run.WorkspaceID, run.TaskID, run.AttemptID, run.Revision).Scan(&route, &replans, &maxReplans, &escalations, &maxEscalations)
		if err == sql.ErrNoRows {
			return ErrInvalidWorkerState
		}
		if err != nil {
			return err
		}
		// This repeats the accepted-assignment/dependency and bounded ancestor-hold
		// fences after response journaling, inside the continuation transaction.
		if _, err = teamgate.Require(ctx, tx, run.WorkspaceID, run.TaskID); err != nil {
			return err
		}
		var continuation []byte
		var replanDelta, escalationDelta int64
		switch kind {
		case "replan":
			if replans >= maxReplans {
				return ErrReplanLimit
			}
			replanDelta = 1
			continuation, err = json.Marshal(map[string]any{"replan": map[string]any{"reason": reason, "instructions": instructions}})
		case "escalate":
			rp := defaultRoutePolicy()
			if err = json.Unmarshal([]byte(route), &rp); err != nil {
				return err
			}
			if !rp.RoutingEnabled {
				return errProposalRoutingDisabled
			}
			if escalations >= maxEscalations {
				return ErrEscalationLimit
			}
			// Read the selected candidate from this same durable owner, rather than
			// trusting an earlier caller's route-policy/candidate snapshot.
			var candidate sql.NullString
			if err = tx.QueryRowContext(ctx, `SELECT last_candidate_id FROM agent_worker_runs WHERE id=?`, run.ID).Scan(&candidate); err != nil {
				return err
			}
			if candidate.Valid && !contains(rp.ExcludedCandidateIDs, candidate.String) {
				rp.ExcludedCandidateIDs = append(rp.ExcludedCandidateIDs, candidate.String)
			}
			var raw []byte
			raw, err = json.Marshal(rp)
			if err != nil {
				return err
			}
			route = string(raw)
			escalationDelta = 1
			continuation, err = json.Marshal(map[string]any{"escalation": map[string]any{"reason": reason, "excluded": rp.ExcludedCandidateIDs}})
		default:
			return ErrInvalidWorkerState
		}
		if err != nil {
			return err
		}
		writePrepared = true
		result, err := tx.ExecContext(ctx, `UPDATE agent_worker_runs AS r SET continuation_json=?,route_policy_json=?,
   escalation_count=escalation_count+?,replan_count=replan_count+?,last_error=NULL,revision=revision+1,updated_at=?
   WHERE r.id=? AND r.workspace_id=? AND r.task_id=? AND r.attempt_id=? AND r.revision=? AND r.status='running' AND `+activeWorkerExecutionBindingPredicate,
			string(continuation), route, escalationDelta, replanDelta, s.clock.UnixMilli(), run.ID, run.WorkspaceID, run.TaskID, run.AttemptID, run.Revision)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrInvalidWorkerState
		}
		return s.journalInTransaction(ctx, tx, run.ID, kind, "succeeded", nil, nil, &kind, nil, map[string]any{"reason": reason})
	})
	safeRejection := errors.Is(err, ErrInvalidWorkerState) || errors.Is(err, teamgate.ErrExecution) ||
		errors.Is(err, teamgate.ErrPaused) || errors.Is(err, ErrReplanLimit) ||
		errors.Is(err, ErrEscalationLimit) || errors.Is(err, errProposalRoutingDisabled)
	if err != nil && (writePrepared || !safeRejection) {
		// The recorded response's proposal did not commit. Stop this scheduler
		// until explicit startup recovery parks the original owner, instead of
		// issuing another reasoning request over an unapplied response. An owner
		// rejected before any write does not halt a potentially newer incarnation.
		// An unavailable/cancelled observation is not such a rejection: the
		// preceding response is already durable, so halt even before the UPDATE
		// rather than reason again over an unapplied response after a read failure.
		s.noteSchedulingHalt(run)
		return fmt.Errorf("Worker proposal continuation commit failed; scheduling halted pending recovery: %w", err)
	}
	return err
}
