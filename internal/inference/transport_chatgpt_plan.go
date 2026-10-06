package inference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ChatGPTPlanTransport implements the public Responses API wire contract used
// by Sign in with ChatGPT plan usage. It intentionally does not call ChatGPT
// private/backend-api endpoints.
type ChatGPTPlanTransport struct{ Client *http.Client }

type chatGPTPlanConfig struct {
	BaseURL       string `json:"base_url"`
	ResponsesPath string `json:"responses_path"`
	TimeoutMS     int64  `json:"timeout_ms"`
}

var chatGPTPlanUnsupportedFields = map[string]bool{
	"background": true, "conversation": true, "max_output_tokens": true, "max_tool_calls": true, "metadata": true,
	"moderation": true, "multi_agent": true, "prompt": true, "prompt_cache_retention": true, "safety_identifier": true,
	"temperature": true, "top_logprobs": true, "top_p": true, "truncation": true, "user": true, "previous_response_id": true,
}

func (t ChatGPTPlanTransport) Dispatch(ctx context.Context, req DispatchRequest, secrets SecretResolver) (DispatchResult, error) {
	if req.Provider == nil || req.Provider.SecretRef == nil || secrets == nil {
		return DispatchResult{}, &TransportError{Code: "secret_unavailable", OutcomeKnown: true}
	}
	var pc, dc chatGPTPlanConfig
	_ = json.Unmarshal(req.Provider.ConnectionJSON, &pc)
	_ = json.Unmarshal(req.Deployment.RuntimeConfigJSON, &dc)
	cfg := pc
	if dc.BaseURL != "" {
		cfg.BaseURL = dc.BaseURL
	}
	if dc.ResponsesPath != "" {
		cfg.ResponsesPath = dc.ResponsesPath
	}
	if dc.TimeoutMS > 0 {
		cfg.TimeoutMS = dc.TimeoutMS
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.ResponsesPath == "" {
		cfg.ResponsesPath = "/responses"
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return DispatchResult{}, &TransportError{Code: "invalid_base_url", OutcomeKnown: true, Err: err}
	}
	// Plan-usage OAuth tokens must only be sent to the public OpenAI API host.
	if !strings.EqualFold(u.Hostname(), "api.openai.com") {
		return DispatchResult{}, &TransportError{Code: "untrusted_plan_token_destination", OutcomeKnown: true}
	}
	if cfg.TimeoutMS < 0 || cfg.TimeoutMS > int64((10*time.Minute)/time.Millisecond) {
		return DispatchResult{}, &TransportError{Code: "invalid_timeout", OutcomeKnown: true}
	}
	var body map[string]any
	if err := json.Unmarshal(req.RequestJSON, &body); err != nil {
		return DispatchResult{}, &TransportError{Code: "invalid_request_json", OutcomeKnown: true, Err: err}
	}
	if _, ok := body["input"]; !ok {
		return DispatchResult{}, &TransportError{Code: "missing_input", OutcomeKnown: true}
	}
	for k := range body {
		if chatGPTPlanUnsupportedFields[k] {
			return DispatchResult{}, &TransportError{Code: "unsupported_plan_field_" + k, OutcomeKnown: true}
		}
	}
	body["model"] = req.Model.ModelRef
	body["store"] = false
	body["stream"] = true
	encoded, err := json.Marshal(body)
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "encode_request", OutcomeKnown: true, Err: err}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+cfg.ResponsesPath, bytes.NewReader(encoded))
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "build_request", OutcomeKnown: true, Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	token, err := secrets.Resolve(ctx, *req.Provider.SecretRef)
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "secret_unavailable", OutcomeKnown: true, Err: err}
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	client := t.Client
	if client == nil {
		client = &http.Client{}
	}
	clone := *client
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if cfg.TimeoutMS > 0 {
		clone.Timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}
	resp, err := clone.Do(httpReq)
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "network_error", OutcomeKnown: false, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return DispatchResult{}, &TransportError{Code: fmt.Sprintf("http_%d", resp.StatusCode), HTTPStatus: resp.StatusCode, OutcomeKnown: true}
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 32<<20))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	var completed json.RawMessage
	var usage json.RawMessage
	seenTerminal := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev struct {
			Type     string          `json:"type"`
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal([]byte(payload), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "response.completed":
			seenTerminal = true
			if len(ev.Response) > 0 && json.Valid(ev.Response) {
				completed = append([]byte(nil), ev.Response...)
				var e struct {
					Usage json.RawMessage `json:"usage"`
				}
				_ = json.Unmarshal(ev.Response, &e)
				usage = e.Usage
			} else {
				completed = append([]byte(nil), []byte(payload)...)
			}
		case "response.failed", "response.incomplete":
			seenTerminal = true
			return DispatchResult{}, &TransportError{Code: ev.Type, HTTPStatus: resp.StatusCode, OutcomeKnown: true}
		}
	}
	if err := scanner.Err(); err != nil {
		return DispatchResult{}, &TransportError{Code: "read_stream", OutcomeKnown: false, Err: err}
	}
	if !seenTerminal || len(completed) == 0 {
		return DispatchResult{}, &TransportError{Code: "stream_ended_without_completion", OutcomeKnown: false}
	}
	return DispatchResult{ResponseJSON: completed, UsageJSON: usage}, nil
}
