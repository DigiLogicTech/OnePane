package inference

import (
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

type AnthropicTransport struct{ Client *http.Client }

type anthropicConnectionConfig struct {
	BaseURL             string   `json:"base_url"`
	MessagesPath        string   `json:"messages_path"`
	AllowedHostSuffixes []string `json:"allowed_host_suffixes"`
	TimeoutMS           int64    `json:"timeout_ms"`
}

func (t AnthropicTransport) Dispatch(ctx context.Context, req DispatchRequest, secrets SecretResolver) (DispatchResult, error) {
	if req.Provider == nil || req.Provider.SecretRef == nil || secrets == nil {
		return DispatchResult{}, &TransportError{Code: "secret_unavailable", OutcomeKnown: true}
	}
	var pc, dc anthropicConnectionConfig
	_ = json.Unmarshal(req.Provider.ConnectionJSON, &pc)
	_ = json.Unmarshal(req.Deployment.RuntimeConfigJSON, &dc)
	cfg := pc
	if dc.BaseURL != "" {
		cfg.BaseURL = dc.BaseURL
	}
	if dc.MessagesPath != "" {
		cfg.MessagesPath = dc.MessagesPath
	}
	if len(dc.AllowedHostSuffixes) > 0 {
		cfg.AllowedHostSuffixes = append([]string(nil), dc.AllowedHostSuffixes...)
	}
	if dc.TimeoutMS > 0 {
		cfg.TimeoutMS = dc.TimeoutMS
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	if cfg.MessagesPath == "" {
		cfg.MessagesPath = "/v1/messages"
	}
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return DispatchResult{}, &TransportError{Code: "invalid_base_url", OutcomeKnown: true, Err: err}
	}
	if len(cfg.AllowedHostSuffixes) == 0 {
		cfg.AllowedHostSuffixes = []string{"api.anthropic.com"}
	}
	if !hostAllowedBySuffixes(u.Hostname(), cfg.AllowedHostSuffixes) {
		return DispatchResult{}, &TransportError{Code: "untrusted_credential_destination", OutcomeKnown: true}
	}
	if cfg.TimeoutMS < 0 || cfg.TimeoutMS > int64((10*time.Minute)/time.Millisecond) {
		return DispatchResult{}, &TransportError{Code: "invalid_timeout", OutcomeKnown: true}
	}

	var in map[string]any
	if err := json.Unmarshal(req.RequestJSON, &in); err != nil {
		return DispatchResult{}, &TransportError{Code: "invalid_request_json", OutcomeKnown: true, Err: err}
	}
	messages, _ := in["messages"].([]any)
	var systemParts []string
	filtered := make([]any, 0, len(messages))
	for _, item := range messages {
		m, ok := item.(map[string]any)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		role, _ := m["role"].(string)
		if role == "system" {
			if text, ok := m["content"].(string); ok && strings.TrimSpace(text) != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}
		filtered = append(filtered, item)
	}
	in["messages"] = filtered
	if len(systemParts) > 0 {
		in["system"] = strings.Join(systemParts, "\n\n")
	}
	in["model"] = req.Model.ModelRef
	in["stream"] = false
	if _, ok := in["max_tokens"]; !ok {
		in["max_tokens"] = 2048
	}
	// OpenAI response-format metadata is a OnePane request hint, not an Anthropic Messages field.
	delete(in, "response_format")
	body, err := json.Marshal(in)
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "encode_request", OutcomeKnown: true, Err: err}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+ensureHTTPPath(cfg.MessagesPath), bytes.NewReader(body))
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "build_request", OutcomeKnown: true, Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	secret, err := secrets.Resolve(ctx, *req.Provider.SecretRef)
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "secret_unavailable", OutcomeKnown: true, Err: err}
	}
	httpReq.Header.Set("x-api-key", secret)
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
	data, err := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "read_response", OutcomeKnown: false, Err: err}
	}
	if len(data) > 32<<20 {
		return DispatchResult{}, &TransportError{Code: "response_too_large", HTTPStatus: resp.StatusCode, OutcomeKnown: true}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DispatchResult{}, &TransportError{Code: fmt.Sprintf("http_%d", resp.StatusCode), HTTPStatus: resp.StatusCode, OutcomeKnown: true}
	}
	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return DispatchResult{}, &TransportError{Code: "invalid_json_response", HTTPStatus: resp.StatusCode, OutcomeKnown: true, Err: err}
	}
	var parts []string
	for _, c := range env.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	if len(parts) == 0 {
		return DispatchResult{}, &TransportError{Code: "missing_text_content", HTTPStatus: resp.StatusCode, OutcomeKnown: true}
	}
	normalized, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": strings.Join(parts, "\n")}}}, "usage": json.RawMessage(env.Usage), "provider_response": json.RawMessage(data)})
	return DispatchResult{ResponseJSON: normalized, UsageJSON: env.Usage}, nil
}

func ensureHTTPPath(v string) string {
	if strings.HasPrefix(v, "/") {
		return v
	}
	return "/" + v
}
