package recoveryinventory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
)

const (
	ModelRequestAttributionParserVersion = "onepane.model-request-attribution/v1"
	ModelRequestAttributionHashDomain    = "onepane.model-request-attribution/exact-stored-bytes/v1\x00"
	ModelRequestAttributionMaxBytes      = 64 * 1024
	ModelRequestAttributionMaxDepth      = 32
	ModelRequestAttributionMaxTokens     = 8192
	// Object members bound the duplicate-key tracking maps. Array size is
	// independently bounded by the whole-input token budget.
	ModelRequestAttributionMaxEntries       = 2048
	ModelRequestAttributionMaxIdentityBytes = 512
)

type ModelRequestAttributionState string

const (
	ModelRequestAttributionKnownCandidate ModelRequestAttributionState = "known_candidate"
	ModelRequestAttributionUnsupported    ModelRequestAttributionState = "unsupported"
	ModelRequestAttributionMalformed      ModelRequestAttributionState = "malformed"
	ModelRequestAttributionAmbiguous      ModelRequestAttributionState = "ambiguous"
	ModelRequestAttributionOversized      ModelRequestAttributionState = "oversized"
	ModelRequestAttributionLimitExceeded  ModelRequestAttributionState = "limit_exceeded"
)

// ModelRequestAttribution is a bounded observation of one stored payload, not
// authenticated producer provenance or proof that any Task owns its effects.
// Unknown outcomes never retain candidate identities. ExactBytesHash is empty
// for oversized input: computing its hash would exceed the input work bound.
type ModelRequestAttribution struct {
	ParserVersion    string
	ExactBytesHash   string
	State            ModelRequestAttributionState
	Reason           string
	InnerTaskID      string
	InnerAttemptID   string
	InnerRequestID   string
	InnerWorkspaceID string
	InnerPrincipalID string
}

var modelAgentRequestFields = []string{
	"protocol_version", "request_id", "workspace_id", "task_id", "attempt_id",
	"principal_id", "role", "objective", "constraints", "context",
	"context_manifest", "permitted_proposal_types", "tool_callback", "json_tool_proposals",
}

// ClassifyModelRequestAttribution recognizes only the reviewed two-message
// envelope and a protocol-valid v1 AgentRequest with original worker lineage.
// Recognizing that shape does not authenticate its producer or validate the
// provider's optional generation parameters. It accepts no direct database scope,
// changes no state, and supplies no "non-agent" or absence-clearance result.
// A known candidate, a missing identity or an unsupported encoding cannot
// authorize settlement, retry, dispatch, or complete-inventory classification.
func ClassifyModelRequestAttribution(raw []byte) ModelRequestAttribution {
	out := ModelRequestAttribution{ParserVersion: ModelRequestAttributionParserVersion}
	finish := func(state ModelRequestAttributionState, reason string) ModelRequestAttribution {
		out.State, out.Reason = state, reason
		return out
	}
	if len(raw) > ModelRequestAttributionMaxBytes {
		return finish(ModelRequestAttributionOversized, "stored_byte_limit")
	}
	h := sha256.New()
	_, _ = h.Write([]byte(ModelRequestAttributionHashDomain))
	_, _ = h.Write(raw)
	out.ExactBytesHash = "sha256:" + hex.EncodeToString(h.Sum(nil))
	budget := modelRequestTokenBudget{}
	if failure := budget.scan(raw, false); failure != nil {
		return finish(failure.state, "outer_"+failure.reason)
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil || outer == nil {
		return finish(ModelRequestAttributionUnsupported, "outer_object_required")
	}
	for key := range outer {
		switch key {
		case "messages", "temperature", "max_tokens", "stream", "response_format":
		default:
			return finish(ModelRequestAttributionUnsupported, "unrecognized_envelope_field")
		}
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(outer["messages"], &messages); err != nil || len(messages) != 2 {
		return finish(ModelRequestAttributionUnsupported, "two_message_envelope_required")
	}
	var inner string
	for i, message := range messages {
		if len(message) != 2 {
			return finish(ModelRequestAttributionUnsupported, "unrecognized_message_fields")
		}
		var role, content string
		if bytes.Equal(bytes.TrimSpace(message["role"]), []byte("null")) || bytes.Equal(bytes.TrimSpace(message["content"]), []byte("null")) || json.Unmarshal(message["role"], &role) != nil || json.Unmarshal(message["content"], &content) != nil {
			return finish(ModelRequestAttributionUnsupported, "text_message_required")
		}
		if (i == 0 && role != "system") || (i == 1 && role != "user") {
			return finish(ModelRequestAttributionUnsupported, "unsupported_message_role")
		}
		if i == 1 {
			inner = content
		}
	}
	if len(inner) > ModelRequestAttributionMaxBytes {
		return finish(ModelRequestAttributionOversized, "inner_byte_limit")
	}
	if failure := budget.scan([]byte(inner), true); failure != nil {
		return finish(failure.state, "inner_"+failure.reason)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(inner), &fields) != nil || fields == nil {
		return finish(ModelRequestAttributionUnsupported, "agent_object_required")
	}
	for key := range fields {
		known := false
		for _, canonical := range modelAgentRequestFields {
			known = known || key == canonical
		}
		if !known {
			return finish(ModelRequestAttributionUnsupported, "unrecognized_agent_field")
		}
	}
	var req agentprotocol.Request
	decoder := json.NewDecoder(strings.NewReader(inner))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return finish(ModelRequestAttributionUnsupported, "unsupported_agent_field_type")
	}
	if req.ProtocolVersion != agentprotocol.Version {
		return finish(ModelRequestAttributionUnsupported, "unsupported_agent_version")
	}
	if req.Validate() != nil {
		return finish(ModelRequestAttributionUnsupported, "invalid_agent_request")
	}
	// This slice describes ordinary Task workers. Taskless AgentRequests and
	// incomplete lineage remain unsupported, even though the wire protocol
	// permits them for other producers.
	if strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.AttemptID) == "" {
		return finish(ModelRequestAttributionUnsupported, "missing_worker_lineage")
	}
	for _, identity := range []string{req.RequestID, req.WorkspaceID, req.TaskID, req.AttemptID, req.PrincipalID, req.Role} {
		if len(identity) > ModelRequestAttributionMaxIdentityBytes {
			return finish(ModelRequestAttributionLimitExceeded, "identity_byte_limit")
		}
		// Go replaces invalid surrogate escapes when decoding. A replacement
		// inside an identity is not a reviewed lossless attribution encoding.
		if strings.ContainsRune(identity, utf8.RuneError) {
			return finish(ModelRequestAttributionUnsupported, "unsupported_identity_encoding")
		}
	}
	for _, section := range req.Context {
		if len(section.ID) > ModelRequestAttributionMaxIdentityBytes || len(section.Kind) > ModelRequestAttributionMaxIdentityBytes {
			return finish(ModelRequestAttributionLimitExceeded, "context_identity_byte_limit")
		}
	}
	out.InnerTaskID, out.InnerAttemptID = req.TaskID, req.AttemptID
	out.InnerRequestID, out.InnerWorkspaceID, out.InnerPrincipalID = req.RequestID, req.WorkspaceID, req.PrincipalID
	return finish(ModelRequestAttributionKnownCandidate, "reviewed_v1_candidate_only")
}

type modelRequestParseFailure struct {
	state  ModelRequestAttributionState
	reason string
}

type modelRequestTokenBudget struct {
	tokens, entries int
}

// scan detects ambiguity before any map/struct decoding of source objects.
// The same budget is shared by the outer envelope and decoded inner payload.
func (b *modelRequestTokenBudget) scan(raw []byte, agentRoot bool) *modelRequestParseFailure {
	if !utf8.Valid(raw) {
		return &modelRequestParseFailure{ModelRequestAttributionMalformed, "invalid_utf8"}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if failure := b.value(d, 0, agentRoot); failure != nil {
		return failure
	}
	if _, err := d.Token(); err != io.EOF {
		return &modelRequestParseFailure{ModelRequestAttributionMalformed, "trailing_or_invalid_json"}
	}
	return nil
}

func (b *modelRequestTokenBudget) token(d *json.Decoder) (json.Token, *modelRequestParseFailure) {
	if b.tokens >= ModelRequestAttributionMaxTokens {
		return nil, &modelRequestParseFailure{ModelRequestAttributionLimitExceeded, "token_limit"}
	}
	token, err := d.Token()
	if err != nil {
		return nil, &modelRequestParseFailure{ModelRequestAttributionMalformed, "invalid_json"}
	}
	b.tokens++
	return token, nil
}

func (b *modelRequestTokenBudget) value(d *json.Decoder, depth int, agentRoot bool) *modelRequestParseFailure {
	token, failure := b.token(d)
	if failure != nil {
		return failure
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return &modelRequestParseFailure{ModelRequestAttributionMalformed, "unexpected_delimiter"}
	}
	depth++
	if depth > ModelRequestAttributionMaxDepth {
		return &modelRequestParseFailure{ModelRequestAttributionLimitExceeded, "depth_limit"}
	}
	seen := map[string]struct{}{}
	for d.More() {
		if delimiter == '{' {
			if b.entries >= ModelRequestAttributionMaxEntries {
				return &modelRequestParseFailure{ModelRequestAttributionLimitExceeded, "entry_limit"}
			}
			b.entries++
			keyToken, failure := b.token(d)
			if failure != nil {
				return failure
			}
			key, valid := keyToken.(string)
			if !valid {
				return &modelRequestParseFailure{ModelRequestAttributionMalformed, "invalid_object_key"}
			}
			if _, exists := seen[key]; exists {
				return &modelRequestParseFailure{ModelRequestAttributionAmbiguous, "duplicate_decoded_key"}
			}
			seen[key] = struct{}{}
			if agentRoot {
				for _, canonical := range modelAgentRequestFields {
					if key != canonical && strings.EqualFold(key, canonical) {
						return &modelRequestParseFailure{ModelRequestAttributionAmbiguous, "agent_field_case_alias"}
					}
				}
			}
		}
		if failure := b.value(d, depth, false); failure != nil {
			return failure
		}
	}
	closing, failure := b.token(d)
	if failure != nil {
		return failure
	}
	want := json.Delim('}')
	if delimiter == '[' {
		want = ']'
	}
	if closing != want {
		return &modelRequestParseFailure{ModelRequestAttributionMalformed, "invalid_container_close"}
	}
	return nil
}
