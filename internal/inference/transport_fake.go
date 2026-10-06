package inference

import (
	"context"
	"encoding/json"
	"fmt"
)

type FakeTransport struct{}

func (FakeTransport) Dispatch(ctx context.Context, req DispatchRequest, _ SecretResolver) (DispatchResult, error) {
	if err := ctx.Err(); err != nil {
		return DispatchResult{}, &TransportError{Code: "context_cancelled", OutcomeKnown: true, Err: err}
	}
	var input any
	if err := json.Unmarshal(req.RequestJSON, &input); err != nil {
		return DispatchResult{}, &TransportError{Code: "invalid_request", OutcomeKnown: true, Err: err}
	}
	body, err := json.Marshal(map[string]any{
		"id":           "fake-" + req.RequestID,
		"object":       "chat.completion",
		"model":        req.Model.ModelRef,
		"choices":      []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "fake response"}, "finish_reason": "stop"}},
		"usage":        map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
		"harness_fake": map[string]any{"input": input},
	})
	if err != nil {
		return DispatchResult{}, fmt.Errorf("marshal fake response: %w", err)
	}
	usage := json.RawMessage(`{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`)
	return DispatchResult{ResponseJSON: body, UsageJSON: usage}, nil
}
