package agentworker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

// AssurancePassed closes the durable worker run after the independent
// assurance service has completed the Task through CompleteVerified.
func (s *Service) AssurancePassed(ctx context.Context, runID, verificationID, checkpointID string) error {
	r, err := s.getRun(ctx, runID)
	if err != nil {
		return err
	}
	if r.Status == RunSucceeded {
		return nil
	}
	if r.Status != RunBlocked && r.Status != RunWaiting {
		return ErrInvalidWorkerState
	}
	if err := s.finishRun(ctx, r.ID, RunSucceeded, ""); err != nil {
		return err
	}
	return s.journal(ctx, r.ID, "verification", "succeeded", nil, nil, strPtr("complete"), strPtr(checkpointID), map[string]any{
		"verification_id": verificationID,
		"checkpoint_id":   checkpointID,
		"independent":     true,
	})
}

// AssuranceRetry returns a completion attempt that failed or was inconclusive
// to the existing worker attempt. The Task transition and worker-run state are
// explicit; no new attempt is created and verified checkpoints remain intact.
func (s *Service) AssuranceRetry(ctx context.Context, runID, verificationID string, result json.RawMessage) error {
	r, err := s.getRun(ctx, runID)
	if err != nil {
		return err
	}
	if r.Status == RunRunning {
		return nil
	}
	if r.Status != RunBlocked {
		return ErrInvalidWorkerState
	}
	t, err := s.tasks.Get(ctx, r.TaskID)
	if err != nil {
		return err
	}
	if t.State != task.StateVerifying {
		return fmt.Errorf("%w: task is %s", ErrInvalidWorkerState, t.State)
	}
	actor := VerifierPrincipal
	if _, err := s.tasks.ContinueAfterVerification(ctx, task.TransitionCommand{
		TaskID:           t.ID,
		ExpectedRevision: t.Revision,
		ActorPrincipalID: &actor,
		Reason:           "independent assurance did not establish completion",
	}); err != nil {
		return err
	}
	if len(result) == 0 || !json.Valid(result) {
		result = json.RawMessage(`{}`)
	}
	cont, _ := json.Marshal(map[string]any{
		"verification_feedback": map[string]any{
			"verification_id": verificationID,
			"result":          json.RawMessage(result),
		},
	})
	if err := s.updateRun(ctx, r.ID, r.Revision, RunRunning, cont, nil, 0, 0, nil, nil, strPtr("completion verification did not pass")); err != nil {
		return err
	}
	return s.journal(ctx, r.ID, "verification", "retry", nil, nil, strPtr("complete"), strPtr(verificationID), map[string]any{"verification_id": verificationID})
}
