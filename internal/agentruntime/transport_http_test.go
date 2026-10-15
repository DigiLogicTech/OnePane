package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"net/http"
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type runtimeStaticSecrets map[string]string

func (s runtimeStaticSecrets) Resolve(_ context.Context, ref string) (string, error) {
	return s[ref], nil
}

func TestAgentProtocolHTTPTransportRoundTrip(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req agentprotocol.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		_ = json.NewEncoder(w).Encode(agentprotocol.Response{ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID, ProposalType: agentprotocol.ProposalComplete, Proposal: json.RawMessage(`{"summary":"done"}`)})
	}))
	defer srv.Close()
	secret := "env:X"
	c := Connection{EndpointJSON: json.RawMessage(`{"base_url":"` + srv.URL + `"}`), AuthType: "bearer", SecretRef: &secret}
	req := agentprotocol.Request{ProtocolVersion: agentprotocol.Version, RequestID: "r", WorkspaceID: "ws", PrincipalID: "a", Objective: "x", Constraints: json.RawMessage(`{}`), ContextManifest: json.RawMessage(`{}`), PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalComplete}}
	resp, err := (AgentProtocolHTTPTransport{}).Invoke(context.Background(), c, req, runtimeStaticSecrets{"env:X": "token"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer token" || resp.ProposalType != agentprotocol.ProposalComplete {
		t.Fatalf("auth=%q resp=%+v", gotAuth, resp)
	}
}

func TestAgentProtocolHTTPRejectsBearerOverRemoteHTTP(t *testing.T) {
	secret := "vault:runtime"
	c := Connection{EndpointJSON: json.RawMessage(`{"base_url":"http://example.com"}`), AuthType: "bearer", SecretRef: &secret}
	req := agentprotocol.Request{ProtocolVersion: agentprotocol.Version, RequestID: "r", WorkspaceID: "ws", PrincipalID: "a", Objective: "x", Constraints: json.RawMessage(`{}`), ContextManifest: json.RawMessage(`{}`), PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalComplete}}
	if _, err := (AgentProtocolHTTPTransport{}).Invoke(context.Background(), c, req, runtimeStaticSecrets{"vault:runtime": "token"}); err == nil {
		t.Fatal("expected remote plaintext credential destination rejection")
	}
}

func TestAgentProtocolHTTPNeverSendsOriginLabelToRemoteOrDNSHost(t *testing.T){
 req:=agentprotocol.Request{ProtocolVersion:agentprotocol.Version,RequestID:"r",
  WorkspaceID:"ws",PrincipalID:"agent",Objective:"reason",
  Constraints:json.RawMessage(`{}`),ContextManifest:json.RawMessage(`{}`),
  PermittedProposalTypes:[]agentprotocol.ProposalType{agentprotocol.ProposalComplete}}
 for _,base:=range []string{
  "http://localhost:9000","https://example.com","http://192.168.2.1:9000",
  "http://127.0.0.1@evil.test","https://[2001:db8::1]/v1",
 }{
  t.Run(base,func(t *testing.T){
   cfg,_:=json.Marshal(map[string]string{"base_url":base})
   c:=Connection{EndpointJSON:cfg,AuthType:"none",
    DataPolicyJSON:json.RawMessage(`{"destination_kind":"origin_node"}`)}
   _,err:=(AgentProtocolHTTPTransport{}).Invoke(context.Background(),c,req,nil)
   var transportErr *RuntimeTransportError
   if !errors.As(err,&transportErr)||transportErr.Code!="origin_endpoint_not_loopback"||
    !transportErr.OutcomeKnown{
    t.Fatalf("origin endpoint %s did not fail before transport: %v",base,err)
   }
  })
 }
}

func TestAgentProtocolHTTPPlainBearerRejectsAmbiguousLocalhostDNS(t *testing.T){
 secret:="vault:local-test"
 req:=agentprotocol.Request{ProtocolVersion:agentprotocol.Version,RequestID:"r",
  WorkspaceID:"ws",PrincipalID:"agent",Objective:"reason",
  Constraints:json.RawMessage(`{}`),ContextManifest:json.RawMessage(`{}`),
  PermittedProposalTypes:[]agentprotocol.ProposalType{agentprotocol.ProposalComplete}}
 c:=Connection{EndpointJSON:json.RawMessage(`{"base_url":"http://localhost:9000"}`),
  AuthType:"bearer",SecretRef:&secret}
 _,err:=(AgentProtocolHTTPTransport{}).Invoke(context.Background(),c,req,
  runtimeStaticSecrets{"vault:local-test":"PRIVATE_SECRET_CANARY"})
 var transportErr *RuntimeTransportError
 if !errors.As(err,&transportErr)||transportErr.Code!="insecure_credential_destination"{
  t.Fatalf("DNS hostname was allowed plaintext bearer: %v",err)
 }
}

func TestAgentProtocolOriginLoopbackCannotUseConfiguredHTTPProxy(t *testing.T) {
	var originCalls, proxyCalls int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originCalls++
		var req agentprotocol.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { t.Error(err); return }
		if req.Objective != "private-origin-prompt" { t.Errorf("unexpected objective: %q", req.Objective) }
		_ = json.NewEncoder(w).Encode(agentprotocol.Response{
			ProtocolVersion: agentprotocol.Version, RequestID: req.RequestID,
			ProposalType: agentprotocol.ProposalComplete,
			Proposal: json.RawMessage(`{"summary":"host-local"}`),
		})
	}))
	defer origin.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls++
		t.Errorf("private origin data routed through a configured HTTP proxy: %q", r.URL)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil { t.Fatal(err) }
	cfg, _ := json.Marshal(map[string]string{"base_url": origin.URL})
	c := Connection{
		EndpointJSON: cfg, AuthType: "none",
		DataPolicyJSON: json.RawMessage(`{"destination_kind":"origin_node"}`),
	}
	req := agentprotocol.Request{
		ProtocolVersion: agentprotocol.Version, RequestID: "origin-proxy-regression",
		WorkspaceID: "ws", PrincipalID: "agent", Objective: "private-origin-prompt",
		Constraints: json.RawMessage(`{}`), ContextManifest: json.RawMessage(`{}`),
		PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalComplete},
	}
	client := &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}}
	_, err = (AgentProtocolHTTPTransport{Client: client}).Invoke(context.Background(), c, req, nil)
	if err != nil || originCalls != 1 || proxyCalls != 0 {
		t.Fatalf("origin-only transport escaped loopback: origin=%d proxy=%d err=%v", originCalls, proxyCalls, err)
	}
	if client.Transport.(*http.Transport).Proxy == nil {
		t.Fatal("caller-provided transport mutated while stripping the dispatch proxy")
	}
}

type opaqueOriginRoundTripper struct{ calls int }
func (t *opaqueOriginRoundTripper) RoundTrip(*http.Request) (*http.Response,error) {
	t.calls++
	return nil, errors.New("untrusted opaque transport called")
}

func TestAgentProtocolOriginRejectsUninspectableRoundTripper(t *testing.T) {
	opaque := &opaqueOriginRoundTripper{}
	c := Connection{
		EndpointJSON: json.RawMessage(`{"base_url":"http://127.0.0.1:10555"}`),
		AuthType: "none",
		DataPolicyJSON: json.RawMessage(`{"destination_kind":"origin_node"}`),
	}
	req := agentprotocol.Request{
		ProtocolVersion: agentprotocol.Version, RequestID: "origin-opaque-regression",
		WorkspaceID: "ws", PrincipalID: "agent", Objective: "private-input",
		Constraints: json.RawMessage(`{}`), ContextManifest: json.RawMessage(`{}`),
		PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalComplete},
	}
	_,err := (AgentProtocolHTTPTransport{Client: &http.Client{Transport:opaque}}).Invoke(context.Background(),c,req,nil)
	var transportErr *RuntimeTransportError
	if !errors.As(err,&transportErr) || transportErr.Code!="unsafe_origin_transport" ||
		!transportErr.OutcomeKnown || opaque.calls != 0 {
		t.Fatalf("opaque client bypassed origin-only transport rule: calls=%d err=%v",opaque.calls,err)
	}
	if strings.Contains(err.Error(),"private-input") { t.Fatal("private content included in unsafe transport error") }
}

func TestAgentProtocolOriginLoopbackCannotUseInjectedDialContext(t *testing.T) {
	// The substituted receiver simulates an unauthorized off-host sink,
	// without allowing any real external network traffic in the test.
	var originCalls, interceptedCalls, injectedDials int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originCalls++
		var req agentprotocol.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Objective != "private-origin-dialer-input" {
			t.Errorf("unexpected objective: %q", req.Objective)
		}
		_ = json.NewEncoder(w).Encode(agentprotocol.Response{
			ProtocolVersion: agentprotocol.Version,
			RequestID: req.RequestID,
			ProposalType: agentprotocol.ProposalComplete,
			Proposal: json.RawMessage(`{"summary":"local-only"}`),
		})
	}))
	defer origin.Close()
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		interceptedCalls++
		t.Error("private origin input reached the substituted network dial target")
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer sink.Close()
	endpoint, err := json.Marshal(map[string]string{"base_url": origin.URL})
	if err != nil { t.Fatal(err) }
	c := Connection{
		EndpointJSON: endpoint, AuthType: "none",
		DataPolicyJSON: json.RawMessage(`{"destination_kind":"origin_node"}`),
	}
	req := agentprotocol.Request{
		ProtocolVersion: agentprotocol.Version,
		RequestID: "origin-dialer-regression",
		WorkspaceID: "ws", PrincipalID: "agent",
		Objective: "private-origin-dialer-input",
		Constraints: json.RawMessage(`{}`),
		ContextManifest: json.RawMessage(`{}`),
		PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalComplete},
	}
	customDial := func(ctx context.Context, network, address string) (net.Conn, error) {
		injectedDials++
		return (&net.Dialer{}).DialContext(ctx, network, sink.Listener.Addr().String())
	}
	caller := &http.Client{Transport: &http.Transport{DialContext: customDial}}
	_, err = (AgentProtocolHTTPTransport{Client: caller}).Invoke(context.Background(), c, req, nil)
	if err != nil || originCalls != 1 || interceptedCalls != 0 || injectedDials != 0 {
		t.Fatalf("origin input escaped through custom dialer: origin=%d intercepted=%d injected=%d err=%v",
			originCalls, interceptedCalls, injectedDials, err)
	}
	if caller.Transport.(*http.Transport).DialContext == nil {
		t.Fatal("caller-owned transport was mutated")
	}
}

func TestAgentRuntimeHTTPTimeoutIsAlwaysBounded(t *testing.T) {
 for _,tc:=range []struct{name string;configured int64;caller,want time.Duration}{
  {"omitted config and unbounded client",0,0,10*time.Minute},
  {"negative caller timeout",0,-time.Second,10*time.Minute},
  {"excessive caller timeout",0,30*time.Minute,10*time.Minute},
  {"short caller deadline preserved",0,30*time.Second,30*time.Second},
  {"explicit runtime override",250,30*time.Second,250*time.Millisecond},
  {"explicit maximum",int64((10*time.Minute)/time.Millisecond),0,10*time.Minute},
 }{
  t.Run(tc.name,func(t *testing.T){
   got:=boundedRuntimeHTTPTimeout(tc.configured,tc.caller)
   if got!=tc.want{t.Fatalf("timeout=%v want %v",got,tc.want)}
  })
 }
}

func TestAgentProtocolHTTPRejectsMalformedExplicitDataPolicyBeforeDial(t *testing.T) {
 var calls int
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  calls++
  t.Error("a malformed origin policy was sent to a network receiver")
  w.WriteHeader(http.StatusBadGateway)
 }))
 defer srv.Close()
 endpoint,_:=json.Marshal(map[string]string{"base_url":srv.URL})
 req:=agentprotocol.Request{
  ProtocolVersion:agentprotocol.Version,RequestID:"malformed-policy",
  WorkspaceID:"ws",PrincipalID:"agent",Objective:"private-prompt",
  Constraints:json.RawMessage(`{}`),ContextManifest:json.RawMessage(`{}`),
  PermittedProposalTypes:[]agentprotocol.ProposalType{agentprotocol.ProposalComplete},
 }
 for _,raw:=range []string{
  `{"destination_kind":"origin_node"`,
  `{"destination_kind":123}`,
  `["origin_node"]`,
  `null`,
  `{}`,
  `{"destination_kind":"unknown_remote"}`,
  `{"destination_kind":"origin_node","destination_kind":"cloud"}`,
  `{"destination_kind":"cloud","destination_kind":"origin_node"}`,
  `{"destination_kind":"origin_node","allow_raw_secrets":false,"allow_raw_secrets":false}`,
 } {
  c:=Connection{EndpointJSON:endpoint,AuthType:"none",
   DataPolicyJSON:json.RawMessage(raw)}
  _,err:=(AgentProtocolHTTPTransport{}).Invoke(context.Background(),c,req,nil)
  var te *RuntimeTransportError
  if !errors.As(err,&te)||te.Code!="invalid_data_policy"||
    !te.OutcomeKnown||calls!=0{
   t.Fatalf("malformed policy reached transport: raw=%q calls=%d err=%v",
    raw,calls,err)
  }
 }
}


func TestAgentHTTPPolicyKeyCheckerAcceptsUnambiguousJSON(t *testing.T) {
 for _,raw:=range []string{
  `{"destination_kind":"origin_node"}`,
  ` { "allowed_residency": ["origin_node"], "destination_kind": "origin_node" } `,
  `{"destination_kind":"cloud","max_confidentiality":"public"}`,
 }{
  if hasAmbiguousPolicyKeys([]byte(raw)){
   t.Fatalf("unambiguous explicit policy rejected: %s",raw)
  }
 }
}


func TestAgentHTTPAuthHeaderCannotBecomeProxyOrRoutingSecret(t *testing.T){
 var calls int
 sink:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  t.Error("unsafe auth header reached server")
 }))
 defer sink.Close()
 secret:="env:auth-boundary"
 request:=agentprotocol.Request{ProtocolVersion:agentprotocol.Version,
  RequestID:"header-policy",WorkspaceID:"ws",PrincipalID:"agent",
  Constraints:json.RawMessage(`{}`),ContextManifest:json.RawMessage(`{}`),
  PermittedProposalTypes:[]agentprotocol.ProposalType{agentprotocol.ProposalComplete},
 }
 for _,name:=range []string{"Proxy-Authorization","Host","Connection",
  "Transfer-Encoding","Content-Length","Bad Header","X-Auth\nInjected"}{
  config,_:=json.Marshal(map[string]string{"base_url":sink.URL,"auth_header":name})
  c:=Connection{EndpointJSON:config,AuthType:"bearer",SecretRef:&secret}
  _,err:=(AgentProtocolHTTPTransport{}).Invoke(context.Background(),c,request,
   runtimeStaticSecrets{"env:auth-boundary":"PRIVATE_CANARY"})
  var transportErr *RuntimeTransportError
  if !errors.As(err,&transportErr)||transportErr.Code!="invalid_auth_config"||
    !transportErr.OutcomeKnown||calls!=0{
   t.Fatalf("unsafe auth header %q escaped: calls=%d err=%v",name,calls,err)
  }
 }
 for _,allowed:=range []string{"Authorization","X-API-Key","Api-Key","X_Auth_Token"}{
  if !safeAgentAuthHeader(allowed){t.Fatalf("valid custom auth header rejected: %s",allowed)}
 }
}
