package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OpenAICompatibleTransport struct{ Client *http.Client }

type openAIConnectionConfig struct {
	BaseURL             string   `json:"base_url"`
	ChatPath            string   `json:"chat_path"`
	AuthHeader          string   `json:"auth_header"`
	AuthPrefix          string   `json:"auth_prefix"`
	AuthRaw             bool     `json:"auth_raw"`
	AllowedHostSuffixes []string `json:"allowed_host_suffixes"`
	TimeoutMS           int64    `json:"timeout_ms"`
}

func (t OpenAICompatibleTransport) Dispatch(ctx context.Context, req DispatchRequest, secrets SecretResolver) (DispatchResult, error) {
	var providerConfig, runtimeConfig openAIConnectionConfig
	if req.Provider != nil {
		_ = json.Unmarshal(req.Provider.ConnectionJSON, &providerConfig)
	}
	_ = json.Unmarshal(req.Deployment.RuntimeConfigJSON, &runtimeConfig)
	cfg := providerConfig
	if runtimeConfig.BaseURL != "" {
		cfg.BaseURL = runtimeConfig.BaseURL
	}
	if runtimeConfig.ChatPath != "" {
		cfg.ChatPath = runtimeConfig.ChatPath
	}
	if runtimeConfig.AuthHeader != "" {
		cfg.AuthHeader = runtimeConfig.AuthHeader
	}
	if runtimeConfig.AuthPrefix != "" {
		cfg.AuthPrefix = runtimeConfig.AuthPrefix
	}
	if runtimeConfig.AuthRaw {
		cfg.AuthRaw = true
	}
	if len(runtimeConfig.AllowedHostSuffixes) > 0 {
		cfg.AllowedHostSuffixes = append([]string(nil), runtimeConfig.AllowedHostSuffixes...)
	}
	if runtimeConfig.TimeoutMS > 0 {
		cfg.TimeoutMS = runtimeConfig.TimeoutMS
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return DispatchResult{}, &TransportError{Code: "missing_base_url", OutcomeKnown: true}
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return DispatchResult{}, &TransportError{Code: "invalid_base_url", OutcomeKnown: true, Err: err}
	}
	if len(cfg.AllowedHostSuffixes) > 0 && !hostAllowedBySuffixes(parsed.Hostname(), cfg.AllowedHostSuffixes) {
		return DispatchResult{}, &TransportError{Code: "untrusted_credential_destination", OutcomeKnown: true}
	}
	if cfg.ChatPath == "" {
		cfg.ChatPath = "/chat/completions"
	}
	if !strings.HasPrefix(cfg.ChatPath, "/") {
		cfg.ChatPath = "/" + cfg.ChatPath
	}
	if cfg.AuthHeader == "" {
		cfg.AuthHeader = "Authorization"
	}
	if strings.ContainsAny(cfg.AuthHeader, "\r\n") {
		return DispatchResult{}, &TransportError{Code: "invalid_auth_header", OutcomeKnown: true}
	}
	if cfg.AuthPrefix == "" && !cfg.AuthRaw {
		cfg.AuthPrefix = "Bearer"
	}
	if strings.ContainsAny(cfg.AuthPrefix, "\r\n") {
		return DispatchResult{}, &TransportError{Code: "invalid_auth_prefix", OutcomeKnown: true}
	}
	if cfg.TimeoutMS < 0 || cfg.TimeoutMS > int64((10*time.Minute)/time.Millisecond) {
		return DispatchResult{}, &TransportError{Code: "invalid_timeout", OutcomeKnown: true}
	}

	var bodyObj map[string]any
	if err := json.Unmarshal(req.RequestJSON, &bodyObj); err != nil {
		return DispatchResult{}, &TransportError{Code: "invalid_request_json", OutcomeKnown: true, Err: err}
	}
	bodyObj["model"] = req.Model.ModelRef
	bodyObj["stream"] = false
	body, err := json.Marshal(bodyObj)
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "encode_request", OutcomeKnown: true, Err: err}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+cfg.ChatPath, bytes.NewReader(body))
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "build_request", OutcomeKnown: true, Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.Provider != nil && strings.ToLower(req.Provider.AuthType) != "none" {
		if req.Provider.SecretRef == nil || secrets == nil {
			return DispatchResult{}, &TransportError{Code: "secret_unavailable", OutcomeKnown: true}
		}
		secret, err := secrets.Resolve(ctx, *req.Provider.SecretRef)
		if err != nil {
			return DispatchResult{}, &TransportError{Code: "secret_unavailable", OutcomeKnown: true, Err: err}
		}
		value := secret
		if !cfg.AuthRaw && cfg.AuthPrefix != "" {
			value = cfg.AuthPrefix + " " + secret
		}
		httpReq.Header.Set(cfg.AuthHeader, value)
	}
	client := t.Client
	if client == nil {
		client = &http.Client{}
	}
	clone := *client
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if cfg.TimeoutMS > 0 {
		clone.Timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}
	client = &clone
	resp, err := client.Do(httpReq)
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "network_error", OutcomeKnown: false, Err: err}
	}
	defer resp.Body.Close()
	const maxResponse = 32 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "read_response", OutcomeKnown: false, Err: err}
	}
	if len(data) > maxResponse {
		return DispatchResult{}, &TransportError{Code: "response_too_large", HTTPStatus: resp.StatusCode, OutcomeKnown: true}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		te := &TransportError{Code: fmt.Sprintf("http_%d", resp.StatusCode), HTTPStatus: resp.StatusCode, OutcomeKnown: true}
		if resp.StatusCode == http.StatusTooManyRequests {
			if secs, err := strconv.ParseInt(strings.TrimSpace(resp.Header.Get("Retry-After")), 10, 64); err == nil {
				v := time.Now().UTC().Add(time.Duration(secs) * time.Second).UnixMilli()
				te.RetryAfterMS = &v
			}
		}
		return DispatchResult{}, te
	}
	if !json.Valid(data) {
		return DispatchResult{}, &TransportError{Code: "invalid_json_response", HTTPStatus: resp.StatusCode, OutcomeKnown: true}
	}
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	_ = json.Unmarshal(data, &envelope)
	return DispatchResult{ResponseJSON: append(json.RawMessage(nil), data...), UsageJSON: envelope.Usage}, nil
}

func hostAllowedBySuffixes(host string, suffixes []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, suffix := range suffixes {
		suffix = strings.ToLower(strings.TrimSpace(suffix))
		if suffix == "" {
			continue
		}
		if strings.HasPrefix(suffix, ".") {
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
				return true
			}
		} else if host == suffix {
			return true
		}
	}
	return false
}
