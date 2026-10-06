package agentworker

import (
	"encoding/json"
	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
	"testing"
)

func testReq() agentprotocol.Request {
	return agentprotocol.Request{ProtocolVersion: agentprotocol.Version, RequestID: "req1", WorkspaceID: "ws", PrincipalID: "p", Objective: "do it", Constraints: json.RawMessage(`{}`), ContextManifest: json.RawMessage(`{}`), PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalComplete}, ToolCallback: false}
}
func TestDecodeOpenAIEnvelope(t *testing.T) {
	body := json.RawMessage(`{"choices":[{"message":{"content":"{\"protocol_version\":\"v1\",\"request_id\":\"req1\",\"proposal_type\":\"complete\",\"proposal\":{\"result\":{\"ok\":true}}}"}}]}`)
	got, err := decodeModelResponse(body, testReq())
	if err != nil {
		t.Fatal(err)
	}
	if got.ProposalType != agentprotocol.ProposalComplete {
		t.Fatalf("got %s", got.ProposalType)
	}
}
func TestBuildModelRequestUsesSchemaForL2(t *testing.T) {
	b, err := buildModelRequest(testReq(), "L2")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	_ = json.Unmarshal(b, &v)
	if _, ok := v["response_format"]; !ok {
		t.Fatal("missing response_format")
	}
}
