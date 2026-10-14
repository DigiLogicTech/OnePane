package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
