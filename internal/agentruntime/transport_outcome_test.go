package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
)

func TestAgentProtocolHTTPPostDispatchErrorsRequireReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, code string
	}{
		{"invalid_json", 200, `{`, "invalid_json_response"},
		{"invalid_response", 200, `{}`, "invalid_agent_response"},
		{"wrong_request", 200, `{"protocol_version":"1","request_id":"other","proposal_type":"complete","proposal":{}}`, "invalid_agent_response"},
		{"oversized", 200, strings.Repeat("x", (16<<20)+1), "response_too_large"},
		{"redirect", 307, `{}`, "http_Temporary Redirect"},
		{"rejection", 400, `{}`, "http_Bad Request"},
		{"rate_limit", 429, `{}`, "http_Too Many Requests"},
		{"server_error", 500, `{}`, "http_Internal Server Error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1) // Simulates an effect before the response becomes unusable.
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			endpoint, _ := json.Marshal(map[string]string{"base_url": srv.URL})
			req := agentprotocol.Request{ProtocolVersion: agentprotocol.Version, RequestID: "r", WorkspaceID: "ws", PrincipalID: "agent", Objective: "test", Constraints: json.RawMessage(`{}`), ContextManifest: json.RawMessage(`{}`), PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalComplete}}
			_, err := (AgentProtocolHTTPTransport{}).Invoke(context.Background(), Connection{EndpointJSON: endpoint, AuthType: "none"}, req, nil)
			var te *RuntimeTransportError
			if !errors.As(err, &te) || te.OutcomeKnown || te.Code != tc.code || te.HTTPStatus != tc.status || calls.Load() != 1 {
				t.Fatalf("post-dispatch result considered repeatable: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}
