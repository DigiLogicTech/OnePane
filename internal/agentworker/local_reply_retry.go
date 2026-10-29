package agentworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/contextcompiler"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
)

// A local zero-cost model can emit truncated JSON without touching anything
// outside the machine. Two further attempts, with the parse error in context,
// are enough to correct that. Cloud and external runtimes stay on the
// reconciliation path because their effects may already have happened.
const maxLocalMalformedReplyRetries = 2

type malformedLocalReplyError struct{ cause error }

func (e *malformedLocalReplyError) Error() string {
	if e == nil || e.cause == nil {
		return "malformed local model reply"
	}
	return e.cause.Error()
}

func (e *malformedLocalReplyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type localReplyRetry struct {
	Attempt     int    `json:"attempt"`
	Error       string `json:"error"`
	CandidateID string `json:"candidate_id"`
}

func retriableLocalMalformedReply(c scheduler.Candidate, q inference.InferenceRequest) bool {
	return c.Kind == scheduler.CandidateModel && c.Local &&
		c.CostClass == scheduler.CostLocal && c.HardZeroIncrementalCost &&
		q.Status == inference.RequestSucceeded
}

func localReplyRetryFrom(raw json.RawMessage) *localReplyRetry {
	fields, ok := readWorkerContinuationFields(raw)
	if !ok {
		return nil
	}
	encoded, exists := fields["local_reply_retry"]
	if !exists || len(encoded) == 0 || string(encoded) == "null" {
		return nil
	}
	var retry localReplyRetry
	if json.Unmarshal(encoded, &retry) != nil || retry.Attempt < 1 || retry.CandidateID == "" {
		return nil
	}
	return &retry
}

func mergeLocalReplyRetry(raw json.RawMessage, retry localReplyRetry) (json.RawMessage, error) {
	fields := map[string]any{}
	if len(raw) > 0 && string(raw) != "{}" && string(raw) != "null" {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
	}
	fields["local_reply_retry"] = retry
	return json.Marshal(fields)
}

func localReplyRetrySection(raw json.RawMessage) (contextcompiler.Section, bool) {
	retry := localReplyRetryFrom(raw)
	if retry == nil {
		return contextcompiler.Section{}, false
	}
	body, err := json.Marshal(map[string]any{
		"attempt":      retry.Attempt,
		"error":        retry.Error,
		"candidate_id": retry.CandidateID,
		"instruction":  "The previous local reply was rejected because it was malformed or truncated. Return exactly one AgentResponse JSON object. Do not repeat the broken text.",
	})
	if err != nil {
		return contextcompiler.Section{}, false
	}
	return contextcompiler.Section{
		ID: "local-reply-rejected", Kind: "continuation", Trust: "UNVERIFIED_DERIVED",
		Authoritative: false, Required: false, Priority: 85, Content: body,
	}, true
}

func (s *Service) retryMalformedLocalReply(ctx context.Context, run Run, res TickResult, candidate scheduler.Candidate, ref string, cause error) TickResult {
	previous := localReplyRetryFrom(run.Continuation)
	attempt := 1
	if previous != nil {
		attempt = previous.Attempt + 1
	}
	message := boundedString(cause.Error(), 2048)
	kind := string(candidate.Kind)
	id := candidate.ID
	if attempt > maxLocalMalformedReplyRetries {
		if journalErr := s.journal(ctx, run.ID, "model", "failed", &kind, &id, nil, &ref, map[string]any{
			"error": message, "action": "local_malformed_reply_exhausted", "attempt": attempt,
		}); journalErr != nil {
			s.noteSchedulingHalt(run)
			return failedResult(res, fmt.Errorf("record exhausted local reply retry: %w", errors.Join(cause, journalErr)))
		}
		return s.blockRun(ctx, run, res, "local model reply stayed malformed after bounded retries: "+message)
	}
	cont, err := mergeLocalReplyRetry(run.Continuation, localReplyRetry{Attempt: attempt, Error: message, CandidateID: candidate.ID})
	if err != nil {
		return failedResult(res, err)
	}
	if err := s.updateRun(ctx, run.ID, run.Revision, RunRunning, cont, nil, 0, 0, &kind, &id, strPtr(message)); err != nil {
		s.noteSchedulingHalt(run)
		return failedResult(res, fmt.Errorf("persist local reply retry: %w", errors.Join(cause, err)))
	}
	if journalErr := s.journal(ctx, run.ID, "model", "failed", &kind, &id, nil, &ref, map[string]any{
		"error": message, "action": "local_malformed_reply_retry", "attempt": attempt,
	}); journalErr != nil {
		s.noteSchedulingHalt(run)
		return failedResult(res, fmt.Errorf("record local reply retry: %w", errors.Join(cause, journalErr)))
	}
	res.Status = "retry_reasoning"
	res.Error = message
	return res
}
