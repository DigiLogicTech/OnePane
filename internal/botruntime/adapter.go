package botruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

type SecretResolver interface {
	Resolve(context.Context, string) (string, error)
}

type Adapter interface {
	EnsureSession(context.Context, Connection, Bot, string, string, bool) (string, error)
	Send(context.Context, Connection, Bot, string, string, string) (RemoteReply, error)
}

type RemoteReply struct {
	Text            string
	RemoteMessageID string
	RemoteRunID     string
	Receipt         json.RawMessage
}

type transportError struct {
	err     error
	unknown bool
}

func (e transportError) Error() string { return e.err.Error() }
func (e transportError) Unwrap() error { return e.err }
func isUnknown(err error) bool         { var te transportError; return errorsAs(err, &te) && te.unknown }

// small local equivalent avoids importing errors in callers just for one check.
func errorsAs(err error, target *transportError) bool {
	for err != nil {
		if v, ok := err.(transportError); ok {
			*target = v
			return true
		}
		if v, ok := err.(*transportError); ok {
			*target = *v
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return false
}

type HTTPAdapter struct {
	Mode    string
	Secrets SecretResolver
	Client  *http.Client
}

func (a HTTPAdapter) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
}

func (a HTTPAdapter) auth(ctx context.Context, c Connection, req *http.Request) error {
	if c.CredentialRef == nil {
		return nil
	}
	if a.Secrets == nil {
		return fmt.Errorf("secret resolver unavailable")
	}
	secret, err := a.Secrets.Resolve(ctx, *c.CredentialRef)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	return nil
}

func safeBase(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("invalid bot base URL")
	}
	host := strings.ToLower(u.Hostname())
	loop := host == "127.0.0.1" || host == "localhost" || host == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loop) {
		return nil, fmt.Errorf("bot base URL must use HTTPS except loopback HTTP")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("bot base URL may not contain query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

var safeProfile = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func hermesPrefix(profile string) (string, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" || profile == "default" {
		return "", nil
	}
	if !safeProfile.MatchString(profile) {
		return "", fmt.Errorf("invalid Hermes profile name")
	}
	return "/p/" + url.PathEscape(profile), nil
}

func (a HTTPAdapter) EnsureSession(ctx context.Context, c Connection, b Bot, localID, title string, canonical bool) (string, error) {
	if a.Mode == "relay" {
		return localID, nil
	}
	if a.Mode != "hermes" {
		return "", fmt.Errorf("unsupported bot adapter mode")
	}
	if c.BaseURL == nil {
		return "", fmt.Errorf("Hermes base URL missing")
	}
	base, err := safeBase(*c.BaseURL)
	if err != nil {
		return "", err
	}
	prefix, err := hermesPrefix(b.RemoteBotID)
	if err != nil {
		return "", err
	}
	if canonical {
		q := *base
		q.Path = path.Join(q.Path, prefix, "/api/sessions")
		vals := q.Query()
		vals.Set("title", "Bot Chat")
		vals.Set("include_hidden", "true")
		vals.Set("limit", "1")
		q.RawQuery = vals.Encode()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, q.String(), nil)
		if err := a.auth(ctx, c, req); err != nil {
			return "", err
		}
		resp, err := a.client().Do(req)
		if err != nil {
			return "", transportError{err: err, unknown: false}
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if id := extractFirstID(body); id != "" {
				return id, nil
			}
		}
	}
	q := *base
	q.Path = path.Join(q.Path, prefix, "/api/sessions")
	if strings.TrimSpace(title) == "" {
		if canonical {
			title = "Bot Chat"
		} else {
			title = "OnePane chat"
		}
	}
	payload, _ := json.Marshal(map[string]any{"id": localID, "title": title, "source": "api_server"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.String(), bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if err := a.auth(ctx, c, req); err != nil {
		return "", err
	}
	resp, err := a.client().Do(req)
	if err != nil {
		return "", transportError{err: err, unknown: true}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	resp.Body.Close()
	if resp.StatusCode == 409 {
		return localID, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Hermes session create HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if id := extractFirstID(body); id != "" {
		return id, nil
	}
	return localID, nil
}

func (a HTTPAdapter) Send(ctx context.Context, c Connection, b Bot, remoteSessionID, input, idempotency string) (RemoteReply, error) {
	if c.BaseURL == nil {
		return RemoteReply{}, fmt.Errorf("bot base URL missing")
	}
	base, err := safeBase(*c.BaseURL)
	if err != nil {
		return RemoteReply{}, err
	}
	switch a.Mode {
	case "hermes":
		prefix, err := hermesPrefix(b.RemoteBotID)
		if err != nil {
			return RemoteReply{}, err
		}
		q := *base
		q.Path = path.Join(q.Path, prefix, "/api/sessions", remoteSessionID, "chat")
		payload, _ := json.Marshal(map[string]any{"input": input})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.String(), bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotency)
		if err := a.auth(ctx, c, req); err != nil {
			return RemoteReply{}, err
		}
		resp, err := a.client().Do(req)
		if err != nil {
			return RemoteReply{}, transportError{err: err, unknown: true}
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return RemoteReply{}, fmt.Errorf("Hermes chat HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		text, msgID, runID := extractReply(body)
		if text == "" {
			h := *base
			h.Path = path.Join(h.Path, prefix, "/api/sessions", remoteSessionID, "messages")
			hreq, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.String(), nil)
			if err := a.auth(ctx, c, hreq); err != nil {
				return RemoteReply{}, err
			}
			hresp, e := a.client().Do(hreq)
			if e == nil {
				hb, _ := io.ReadAll(io.LimitReader(hresp.Body, 4<<20))
				hresp.Body.Close()
				if hresp.StatusCode >= 200 && hresp.StatusCode < 300 {
					text, msgID, _ = extractReply(hb)
				}
			}
		}
		if text == "" {
			return RemoteReply{}, fmt.Errorf("Hermes returned no assistant text")
		}
		return RemoteReply{Text: text, RemoteMessageID: msgID, RemoteRunID: runID, Receipt: canonicalJSON(body)}, nil
	case "relay":
		q := *base
		q.Path = path.Join(q.Path, "/v1/bot/chat")
		payload, _ := json.Marshal(map[string]any{"bot_id": b.RemoteBotID, "session_id": remoteSessionID, "input": input, "idempotency_key": idempotency})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, q.String(), bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotency)
		if err := a.auth(ctx, c, req); err != nil {
			return RemoteReply{}, err
		}
		resp, err := a.client().Do(req)
		if err != nil {
			return RemoteReply{}, transportError{err: err, unknown: true}
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return RemoteReply{}, fmt.Errorf("bot relay HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		text, msgID, runID := extractReply(body)
		if text == "" {
			return RemoteReply{}, fmt.Errorf("bot relay returned no assistant text")
		}
		return RemoteReply{Text: text, RemoteMessageID: msgID, RemoteRunID: runID, Receipt: canonicalJSON(body)}, nil
	}
	return RemoteReply{}, fmt.Errorf("unsupported bot adapter mode")
}

func canonicalJSON(raw []byte) json.RawMessage {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return json.RawMessage(`{}`)
	}
	b, _ := json.Marshal(v)
	return b
}
func extractFirstID(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return findID(v)
}
func findID(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"session_id", "id"} {
			if s, ok := x[k].(string); ok && s != "" {
				return s
			}
		}
		for _, k := range []string{"session", "data", "result"} {
			if y, ok := x[k]; ok {
				if s := findID(y); s != "" {
					return s
				}
			}
		}
	case []any:
		for _, y := range x {
			if s := findID(y); s != "" {
				return s
			}
		}
	}
	return ""
}
func extractReply(raw []byte) (string, string, string) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "", "", ""
	}
	return findReply(v)
}
func findReply(v any) (string, string, string) {
	switch x := v.(type) {
	case map[string]any:
		role, _ := x["role"].(string)
		if role == "assistant" {
			if t := contentText(x["content"]); t != "" {
				id, _ := x["id"].(string)
				return t, id, ""
			}
		}
		run, _ := x["run_id"].(string)
		for _, k := range []string{"output_text", "text", "response", "answer"} {
			if s, ok := x[k].(string); ok && strings.TrimSpace(s) != "" {
				id, _ := x["id"].(string)
				return s, id, run
			}
		}
		if m, ok := x["message"]; ok {
			if t, id, r := findReply(m); t != "" {
				if r == "" {
					r = run
				}
				return t, id, r
			}
		}
		// Prefer the last assistant-like item in list-bearing fields.
		for _, k := range []string{"data", "messages", "output", "content", "result"} {
			if y, ok := x[k]; ok {
				if t, id, r := findReply(y); t != "" {
					if r == "" {
						r = run
					}
					return t, id, r
				}
			}
		}
	case []any:
		for i := len(x) - 1; i >= 0; i-- {
			if t, id, r := findReply(x[i]); t != "" {
				return t, id, r
			}
		}
	case string:
		if strings.TrimSpace(x) != "" {
			return x, "", ""
		}
	}
	return "", "", ""
}
func contentText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, y := range x {
			if m, ok := y.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
