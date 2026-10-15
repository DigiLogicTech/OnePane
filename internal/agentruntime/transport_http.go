package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"github.com/DigiLogicTech/OnePane/internal/runtimeendpoint"
)

type AgentProtocolHTTPTransport struct{ Client *http.Client }
type httpRuntimeConfig struct {
	BaseURL    string `json:"base_url"`
	InvokePath string `json:"invoke_path"`
	AuthHeader string `json:"auth_header"`
	AuthPrefix string `json:"auth_prefix"`
	TimeoutMS  int64  `json:"timeout_ms"`
}

func (t AgentProtocolHTTPTransport) Invoke(ctx context.Context, c Connection, req agentprotocol.Request, secrets SecretResolver) (agentprotocol.Response, error) {
	var cfg httpRuntimeConfig
	if err := json.Unmarshal(c.EndpointJSON, &cfg); err != nil {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "invalid_endpoint", OutcomeKnown: true, Err: err}
	}
	// The built-in transport is an independent last safety boundary:
	// a self-declared origin_node must not send data to a remote service,
	// even when the caller neglected the Service's canonical admission.
	var dp DataPolicy
	// Service admission normally normalises this policy, but the adapter is
	// also a final egress boundary. A malformed explicit policy must not
	// silently lose its origin-only classification and default to remote
	// HTTP dispatch when the transport is called directly.
	if raw := bytes.TrimSpace(c.DataPolicyJSON); len(raw) != 0 {
		if raw[0] != '{' || hasAmbiguousPolicyKeys(raw) {
			return agentprotocol.Response{}, &RuntimeTransportError{Code: "invalid_data_policy", OutcomeKnown: true}
		}
		if err := json.Unmarshal(raw, &dp); err != nil {
			return agentprotocol.Response{}, &RuntimeTransportError{Code: "invalid_data_policy", OutcomeKnown: true}
		}
		// An explicit but empty, unknown, or mistyped destination is not
		// permission to send content beyond the local Node. Canonical
		// Service registration always normalises the destination kind.
		switch dp.DestinationKind {
		case "origin_node", "trusted_node", "cloud", "untrusted":
		default:
			return agentprotocol.Response{}, &RuntimeTransportError{Code: "invalid_data_policy", OutcomeKnown: true}
		}
	}
	originOnly := dp.DestinationKind == "origin_node"
	if originOnly && !runtimeendpoint.LiteralLoopbackHTTP(c.EndpointJSON) {
		return agentprotocol.Response{},&RuntimeTransportError{
		 Code:"origin_endpoint_not_loopback",OutcomeKnown:true}
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "invalid_base_url", OutcomeKnown: true, Err: err}
	}
	if strings.ToLower(c.AuthType) != "none" && u.Scheme != "https" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return agentprotocol.Response{}, &RuntimeTransportError{Code: "insecure_credential_destination", OutcomeKnown: true}
		}
	}
	if cfg.InvokePath == "" {
		cfg.InvokePath = "/v1/agent/run"
	}
	if !strings.HasPrefix(cfg.InvokePath, "/") {
		cfg.InvokePath = "/" + cfg.InvokePath
	}
	if cfg.AuthHeader == "" {
		cfg.AuthHeader = "Authorization"
	}
	if cfg.AuthPrefix == "" {
		cfg.AuthPrefix = "Bearer"
	}
	if strings.ContainsAny(cfg.AuthHeader+cfg.AuthPrefix, "\r\n") {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "invalid_auth_config", OutcomeKnown: true}
	}
	if cfg.TimeoutMS < 0 || cfg.TimeoutMS > int64((10*time.Minute)/time.Millisecond) {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "invalid_timeout", OutcomeKnown: true}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "encode_request", OutcomeKnown: true, Err: err}
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+cfg.InvokePath, bytes.NewReader(body))
	if err != nil {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "build_request", OutcomeKnown: true, Err: err}
	}
	hr.Header.Set("Content-Type", "application/json")
	if strings.ToLower(c.AuthType) != "none" {
		if c.SecretRef == nil || secrets == nil {
			return agentprotocol.Response{}, &RuntimeTransportError{Code: "secret_unavailable", OutcomeKnown: true}
		}
		secret, err := secrets.Resolve(ctx, *c.SecretRef)
		if err != nil {
			return agentprotocol.Response{}, &RuntimeTransportError{Code: "secret_unavailable", OutcomeKnown: true, Err: err}
		}
		hr.Header.Set(cfg.AuthHeader, cfg.AuthPrefix+" "+secret)
	}
	client := t.Client
	if client == nil {
		client = &http.Client{}
	}
	clone := *client
	if originOnly {
		// A literal loopback endpoint must also use an independently owned
		// direct transport. Cloning a caller-supplied http.Transport and
		// clearing Proxy is insufficient: custom DialContext/DialTLSContext,
		// TLSNextProto or similar hooks can silently redirect private input.
		// Reject opaque RoundTrippers and discard all caller transport hooks.
		// HTTPS must use the normal platform trust roots.
		switch transport := clone.Transport.(type) {
		case nil:
		case *http.Transport:
			if transport == nil {
				return agentprotocol.Response{}, &RuntimeTransportError{Code: "unsafe_origin_transport", OutcomeKnown: true}
			}
		default:
			return agentprotocol.Response{}, &RuntimeTransportError{Code: "unsafe_origin_transport", OutcomeKnown: true}
		}
		clone.Transport = &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
	}
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	// Even a zero/omitted timeout must not pin an Agent Worker forever.
	// Explicit per-runtime timeouts take precedence, while callers may
	// supply a shorter bounded deadline through their HTTP client.
	clone.Timeout = boundedRuntimeHTTPTimeout(cfg.TimeoutMS, clone.Timeout)
	client = &clone
	resp, err := client.Do(hr)
	if err != nil {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "network_error", OutcomeKnown: false, Err: err}
	}
	defer resp.Body.Close()
	const maxResponse = 16 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "read_response", OutcomeKnown: false, Err: err}
	}
	if len(data) > maxResponse {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "response_too_large", HTTPStatus: resp.StatusCode, OutcomeKnown: true}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "http_" + http.StatusText(resp.StatusCode), HTTPStatus: resp.StatusCode, OutcomeKnown: true}
	}
	var out agentprotocol.Response
	if err := json.Unmarshal(data, &out); err != nil {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "invalid_json_response", HTTPStatus: resp.StatusCode, OutcomeKnown: true, Err: err}
	}
	if err := out.ValidateFor(req); err != nil {
		return agentprotocol.Response{}, &RuntimeTransportError{Code: "invalid_agent_response", HTTPStatus: resp.StatusCode, OutcomeKnown: true, Err: err}
	}
	return out, nil
}

// The configured upper bound is shared by the default and any injected
// http.Client. Unlimited or excessive caller timeouts would otherwise
// keep a queued Worker and its budget reservation indefinitely occupied.
func boundedRuntimeHTTPTimeout(configuredMillis int64, callerTimeout time.Duration) time.Duration {
 const maximum = 10 * time.Minute
 if configuredMillis > 0 {
  return time.Duration(configuredMillis) * time.Millisecond
 }
 if callerTimeout <= 0 || callerTimeout > maximum {
  return maximum
 }
 return callerTimeout
}


// Top-level duplicate policy keys are ambiguous: the later JSON value
// can silently replace an original origin-only label. Never interpret
// that as permission to send a prompt/credential to a different endpoint.
// This accepts ordinary JSON field order and whitespace without requiring
// canonical Service serialization; nested payloads are not unwrapped here.
func hasAmbiguousPolicyKeys(raw []byte) bool {
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 begin,err:=decoder.Token()
 if err!=nil||begin!=json.Delim('{'){return true}
 seen:=make(map[string]bool,8)
 for decoder.More(){
  keyToken,err:=decoder.Token()
  if err!=nil{return true}
  key,ok:=keyToken.(string)
  if !ok||seen[key]{return true}
  seen[key]=true
  var value json.RawMessage
  if err=decoder.Decode(&value);err!=nil{return true}
 }
 end,err:=decoder.Token()
 if err!=nil||end!=json.Delim('}'){return true}
 _,err=decoder.Token()
 return err!=io.EOF
}
