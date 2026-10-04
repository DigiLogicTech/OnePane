package agentprotocol

import (
	"encoding/json"
	"testing"
)

func baseRequest(tool bool) Request {
	p := []ProposalType{ProposalComplete, ProposalFail}
	if tool {
		p = append([]ProposalType{ProposalTool}, p...)
	}
	return Request{ProtocolVersion: Version, RequestID: "r", WorkspaceID: "ws", PrincipalID: "agent", Objective: "do work", Constraints: json.RawMessage(`{}`), ContextManifest: json.RawMessage(`{}`), PermittedProposalTypes: p, ToolCallback: tool}
}
func TestCompleteIsOnlyAProposal(t *testing.T) {
	req := baseRequest(false)
	resp := Response{ProtocolVersion: Version, RequestID: "r", ProposalType: ProposalComplete, Proposal: json.RawMessage(`{"result":"done"}`)}
	if err := resp.ValidateFor(req); err != nil {
		t.Fatal(err)
	}
}
func TestToolProposalRequiresGatewayCallback(t *testing.T) {
	req := baseRequest(false)
	req.PermittedProposalTypes = append(req.PermittedProposalTypes, ProposalTool)
	resp := Response{ProtocolVersion: Version, RequestID: "r", ProposalType: ProposalTool, Proposal: json.RawMessage(`{"tool_id":"x"}`)}
	if err := resp.ValidateFor(req); err == nil {
		t.Fatal("tool proposal accepted without callback authority")
	}
}
func TestResponseMustMatchRequestID(t *testing.T) {
	req := baseRequest(false)
	resp := Response{ProtocolVersion: Version, RequestID: "other", ProposalType: ProposalFail, Proposal: json.RawMessage(`{}`)}
	if err := resp.ValidateFor(req); err == nil {
		t.Fatal("mismatched response accepted")
	}
}
