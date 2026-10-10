package agentprotocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const Version = "v1"

type ProposalType string

const (
	ProposalTool     ProposalType = "tool"
	ProposalDelegate ProposalType = "delegate"
	ProposalReplan   ProposalType = "replan"
	ProposalComplete ProposalType = "complete"
	ProposalHuman    ProposalType = "human"
	ProposalEscalate ProposalType = "escalate"
	ProposalWait     ProposalType = "wait"
	ProposalFail     ProposalType = "fail"
)

type ContextSection struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Trust         string          `json:"trust"`
	Authoritative bool            `json:"authoritative"`
	Content       json.RawMessage `json:"content"`
}

type Request struct {
	ProtocolVersion        string           `json:"protocol_version"`
	RequestID              string           `json:"request_id"`
	WorkspaceID            string           `json:"workspace_id"`
	TaskID                 string           `json:"task_id,omitempty"`
	AttemptID              string           `json:"attempt_id,omitempty"`
	PrincipalID            string           `json:"principal_id"`
	Role                   string           `json:"role,omitempty"`
	Objective              string           `json:"objective"`
	Constraints            json.RawMessage  `json:"constraints"`
	Context                []ContextSection `json:"context"`
	ContextManifest        json.RawMessage  `json:"context_manifest"`
	PermittedProposalTypes []ProposalType   `json:"permitted_proposal_types"`
	ToolCallback           bool             `json:"tool_callback"`
	// JSONToolProposals allows a qualified structured-JSON model to request
	// brokered tools without native function calling. This is NOT tool authority.
	JSONToolProposals      bool             `json:"json_tool_proposals,omitempty"`
}

type Response struct {
	ProtocolVersion string          `json:"protocol_version"`
	RequestID       string          `json:"request_id"`
	ProposalType    ProposalType    `json:"proposal_type"`
	Message         string          `json:"message,omitempty"`
	Proposal        json.RawMessage `json:"proposal"`
	Usage           json.RawMessage `json:"usage,omitempty"`
}

var ErrInvalidProtocol = errors.New("invalid agent protocol message")

func ValidProposalType(p ProposalType) bool {
	switch p {
	case ProposalTool, ProposalDelegate, ProposalReplan, ProposalComplete, ProposalHuman, ProposalEscalate, ProposalWait, ProposalFail:
		return true
	default:
		return false
	}
}

func ValidTrustLabel(v string) bool {
	switch v {
	case "TRUSTED_CONTROL", "TRUSTED_PROCEDURE", "AUTHORITATIVE_DATA", "USER_INSTRUCTION", "UNTRUSTED_CONTENT", "UNVERIFIED_DERIVED", "VERIFIED_DERIVED":
		return true
	default:
		return false
	}
}

func (r Request) Validate() error {
	if r.ProtocolVersion != Version || strings.TrimSpace(r.RequestID) == "" || strings.TrimSpace(r.WorkspaceID) == "" || strings.TrimSpace(r.PrincipalID) == "" || strings.TrimSpace(r.Objective) == "" {
		return fmt.Errorf("%w: required request fields", ErrInvalidProtocol)
	}
	if len(r.Constraints) == 0 || !json.Valid(r.Constraints) || len(r.ContextManifest) == 0 || !json.Valid(r.ContextManifest) {
		return fmt.Errorf("%w: request JSON fields", ErrInvalidProtocol)
	}
	if len(r.PermittedProposalTypes) == 0 {
		return fmt.Errorf("%w: proposal types required", ErrInvalidProtocol)
	}
	seen := map[ProposalType]bool{}
	for _, p := range r.PermittedProposalTypes {
		if !ValidProposalType(p) || seen[p] {
			return fmt.Errorf("%w: proposal type %q", ErrInvalidProtocol, p)
		}
		seen[p] = true
	}
	for _, s := range r.Context {
		if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Kind) == "" || !ValidTrustLabel(s.Trust) || len(s.Content) == 0 || !json.Valid(s.Content) {
			return fmt.Errorf("%w: invalid context section", ErrInvalidProtocol)
		}
	}
	return nil
}

func (r Response) ValidateFor(req Request) error {
	if r.ProtocolVersion != Version || r.RequestID != req.RequestID || !ValidProposalType(r.ProposalType) || len(r.Proposal) == 0 || !json.Valid(r.Proposal) {
		return fmt.Errorf("%w: invalid response fields", ErrInvalidProtocol)
	}
	allowed := false
	for _, p := range req.PermittedProposalTypes {
		if p == r.ProposalType {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%w: proposal type %q was not permitted", ErrInvalidProtocol, r.ProposalType)
	}
	if r.ProposalType == ProposalTool && !req.ToolCallback && !req.JSONToolProposals {
		return fmt.Errorf("%w: tool proposal requires gateway-mediated request", ErrInvalidProtocol)
	}
	if len(r.Usage) > 0 && !json.Valid(r.Usage) {
		return fmt.Errorf("%w: usage must be JSON", ErrInvalidProtocol)
	}
	return nil
}
