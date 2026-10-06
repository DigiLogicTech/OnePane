package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/tool"
)

type SecretBroker interface {
	Resolve(context.Context, string) (string, error)
	ValidatePluginCredentialRef(context.Context, string, string) error
}

type HTTPAdapter struct {
	preset  Preset
	mode    string
	secrets SecretBroker
	client  *http.Client
}

func NewHTTPAdapter(p Preset, mode string, secrets SecretBroker) *HTTPAdapter {
	return &HTTPAdapter{preset: p, mode: mode, secrets: secrets, client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (a *HTTPAdapter) ID() string      { return "builtin.plugin_http." + a.preset.ID + "." + a.mode }
func (a *HTTPAdapter) Version() string { return "1" }

type requestInput struct {
	SecretRef string            `json:"secret_ref"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Query     map[string]string `json:"query,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      json.RawMessage   `json:"body,omitempty"`
}

func (a *HTTPAdapter) Invoke(ctx context.Context, req tool.AdapterRequest) (tool.AdapterResult, error) {
	var in requestInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("invalid connector input: %w", err))
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = "GET"
	}
	allowedMethods := a.preset.ReadMethods
	if a.mode != "read" {
		allowedMethods = a.preset.MutationMethods
	}
	if !contains(allowedMethods, method) {
		return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("method %s is not allowed for %s mode", method, a.mode))
	}
	cleanPath, err := sanitizePath(in.Path)
	if err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(err)
	}
	if !allowedPrefix(cleanPath, a.preset.AllowedPrefixes) {
		return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("path %s is outside connector scope", cleanPath))
	}
	if a.mode == "read" && method == http.MethodPost && !allowedPrefix(cleanPath, a.preset.ReadPostPrefixes) {
		return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("POST is not approved as read-only for path %s", cleanPath))
	}
	if a.mode == "read" && a.preset.ID == "linear" && graphQLMutation(in.Body) {
		return tool.AdapterResult{}, tool.KnownFailure(errors.New("GraphQL mutation requires the mutate tool"))
	}
	isSend := pathMatchesAny(cleanPath, a.preset.SendPathMarkers)
	if a.preset.SupportsSend && a.mode != "send" && isSend {
		return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("external-send endpoint requires %s", a.preset.SendToolID))
	}
	if a.mode == "send" && !isSend {
		return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("path %s is not an approved external-send endpoint", cleanPath))
	}
	if strings.TrimSpace(in.SecretRef) == "" || a.secrets == nil {
		return tool.AdapterResult{}, tool.KnownFailure(errors.New("vault secret_ref is required"))
	}
	if err := a.secrets.ValidatePluginCredentialRef(ctx, in.SecretRef, a.preset.ID); err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("credential scope: %w", err))
	}
	secret, err := a.secrets.Resolve(ctx, in.SecretRef)
	if err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(err)
	}
	base, err := url.Parse(a.preset.BaseURL)
	if err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(err)
	}
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + cleanPath
	q := u.Query()
	for k, v := range in.Query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	var body io.Reader
	if len(bytes.TrimSpace(in.Body)) > 0 {
		if !json.Valid(in.Body) {
			return tool.AdapterResult{}, tool.KnownFailure(errors.New("body must be valid JSON"))
		}
		body = bytes.NewReader(in.Body)
	}
	hr, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return tool.AdapterResult{}, tool.KnownFailure(err)
	}
	if body != nil {
		hr.Header.Set("Content-Type", "application/json")
	}
	for k, v := range a.preset.StaticHeaders {
		hr.Header.Set(k, v)
	}
	for k, v := range in.Headers {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "authorization" || lk == "cookie" || lk == "host" || lk == "x-api-key" || strings.ContainsAny(k, "\r\n") || strings.ContainsAny(v, "\r\n") {
			return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("header %q is reserved", k))
		}
		hr.Header.Set(k, v)
	}
	auth := secret
	if strings.TrimSpace(a.preset.AuthPrefix) != "" {
		auth = strings.TrimSpace(a.preset.AuthPrefix) + " " + secret
	}
	hr.Header.Set(a.preset.AuthHeader, auth)
	resp, err := a.client.Do(hr)
	if err != nil {
		return tool.AdapterResult{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return tool.AdapterResult{}, err
	}
	if len(data) > 16<<20 {
		return tool.AdapterResult{}, fmt.Errorf("connector response exceeds 16 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tool.AdapterResult{}, fmt.Errorf("connector returned HTTP %d", resp.StatusCode)
	}
	if a.preset.LogicalSuccess == "slack_ok" {
		var x struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &x) == nil && !x.OK {
			return tool.AdapterResult{}, tool.KnownFailure(fmt.Errorf("slack API error: %s", x.Error))
		}
	}
	var resultBody any
	if len(bytes.TrimSpace(data)) == 0 {
		resultBody = map[string]any{}
	} else if json.Unmarshal(data, &resultBody) != nil {
		resultBody = string(data)
	}
	out, _ := json.Marshal(map[string]any{"plugin_id": a.preset.ID, "http_status": resp.StatusCode, "body": resultBody})
	return tool.AdapterResult{Summary: fmt.Sprintf("%s %s returned HTTP %d", method, cleanPath, resp.StatusCode), Result: out}, nil
}

func sanitizePath(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		v = "/"
	}
	u, err := url.ParseRequestURI(v)
	if err != nil || u.IsAbs() || u.Host != "" {
		return "", fmt.Errorf("path must be relative")
	}
	if u.RawQuery != "" {
		return "", fmt.Errorf("query parameters must use the query object")
	}
	c := path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
	if strings.Contains(c, "..") {
		return "", fmt.Errorf("invalid path")
	}
	return c, nil
}
func allowedPrefix(p string, prefixes []string) bool {
	for _, x := range prefixes {
		if strings.HasPrefix(p, x) {
			return true
		}
	}
	return false
}
func contains(xs []string, want string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, want) {
			return true
		}
	}
	return false
}

func pathMatchesAny(p string, markers []string) bool {
	for _, marker := range markers {
		if marker != "" && strings.Contains(p, marker) {
			return true
		}
	}
	return false
}

func graphQLMutation(body json.RawMessage) bool {
	var v struct {
		Query string `json:"query"`
	}
	if json.Unmarshal(body, &v) != nil {
		return true
	}
	q := strings.ToLower(strings.TrimSpace(v.Query))
	return q == "" || strings.Contains(q, "mutation") || strings.Contains(q, "subscription")
}
