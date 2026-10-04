package agentruntime

import (
	"context"
	"encoding/json"
	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"net/http"
	"net/http/httptest"
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
