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
	if json.Unmarshal(c.DataPolicyJSON,&dp)==nil &&
	 dp.DestinationKind=="origin_node" &&
	 !runtimeendpoint.LiteralLoopbackHTTP(c.EndpointJSON){
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
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if cfg.TimeoutMS > 0 {
		clone.Timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}
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
