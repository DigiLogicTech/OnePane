package recoveryinventory

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const WorkerStepReferenceParserVersion = "onepane.worker-step-result-references/v1"
const workerStepReferenceHashDomain = "onepane.worker-step-result-references/exact-input/v1\x00"

type WorkerStepReference struct {
	Family string
	ID     string
}

// This describes only the result_ref/detail_json slice of a stored journal.
// Candidate references still require independently matching rows, original
// Worker/Task/Attempt ownership and reverse provenance. It is not a complete
// step inventory, authenticated producer evidence or postcondition verification.
type WorkerStepReferences struct {
	ParserVersion  string
	ExactBytesHash string
	State          string
	Reason         string
	References     []WorkerStepReference
}

// ClassifyWorkerStepReferences interprets reviewed, explicit result shapes.
// Unsupported/ambiguous input never yields partial references or "no effects".
// No current collector, maintenance loop or admission path calls this parser.
func ClassifyWorkerStepReferences(kind, status string, resultRef *string, detail []byte) WorkerStepReferences {
	out := WorkerStepReferences{ParserVersion: WorkerStepReferenceParserVersion}
	finish := func(state, reason string) WorkerStepReferences {
		out.State, out.Reason = state, reason
		return out
	}
	if len(detail) > ModelRequestAttributionMaxBytes || len(kind) > 64 || len(status) > 64 ||
		(resultRef != nil && len(*resultRef) > ModelRequestAttributionMaxIdentityBytes) {
		return finish("oversized", "input_byte_limit")
	}
	h := sha256.New()
	_, _ = h.Write([]byte(workerStepReferenceHashDomain))
	// Length framing preserves exact bytes and null versus empty result refs.
	write := func(value []byte) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = h.Write(size[:])
		_, _ = h.Write(value)
	}
	write([]byte(kind))
	write([]byte(status))
	if resultRef == nil {
		_, _ = h.Write([]byte{0})
	} else {
		_, _ = h.Write([]byte{1})
		write([]byte(*resultRef))
	}
	write(detail)
	out.ExactBytesHash = "sha256:" + hex.EncodeToString(h.Sum(nil))
	budget := modelRequestTokenBudget{}
	if failure := budget.scan(detail, false); failure != nil {
		return finish(string(failure.state), "detail_"+failure.reason)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(detail, &fields) != nil || fields == nil {
		return finish("unsupported", "detail_object_required")
	}
	validID := func(value string) bool {
		return value != "" && len(value) <= ModelRequestAttributionMaxIdentityBytes &&
			value == strings.TrimSpace(value) && utf8.ValidString(value) &&
			!strings.ContainsAny(value, "\x00\r\n") && !strings.ContainsRune(value, utf8.RuneError)
	}
	if resultRef == nil || !validID(*resultRef) {
		return finish("unsupported", "result_identity_required")
	}
	text := func(key string) (string, bool) {
		raw, exists := fields[key]
		var value string
		if !exists || len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
			return "", false
		}
		return value, true
	}
	exact := func(keys ...string) bool {
		if len(fields) != len(keys) {
			return false
		}
		for _, key := range keys {
			if _, exists := fields[key]; !exists {
				return false
			}
		}
		return true
	}
	stringFields := func(keys ...string) bool {
		for _, key := range keys {
			if _, ok := text(key); !ok {
				return false
			}
		}
		return true
	}
	matchingID := func(key string) (string, bool) {
		value, ok := text(key)
		return value, ok && validID(value)
	}
	known := func(refs ...WorkerStepReference) WorkerStepReferences {
		out.References = refs
		return finish("known_candidate", "reviewed_result_shape_only")
	}
	ref := func(family string) WorkerStepReference { return WorkerStepReference{family, *resultRef} }
	if kind == "model" || kind == "agent_runtime" {
		family := "inference_requests"
		if kind == "agent_runtime" {
			family = "agent_runtime_invocations"
		}
		if status == "succeeded" && exact("message") && stringFields("message") {
			if kind == "model" {
				family = "artifacts"
			}
			return known(ref(family))
		}
		if status == "failed" && exact("error") && stringFields("error") {
			return known(ref(family))
		}
	}
	if status == "unknown" && (kind == "model" || kind == "agent_runtime" || kind == "route") {
		outcome, outcomeOK := text("outcome")
		action, actionOK := text("action")
		if !outcomeOK || !actionOK || action != "operator_reconciliation_required" || !stringFields("reason") {
			return finish("unsupported", "unreviewed_reconciliation_shape")
		}
		family, expectedKind := "", ""
		switch outcome {
		case "unknown_or_response_unreadable":
			if kind == "model" {
				family = "inference_requests"
			}
			if kind == "agent_runtime" {
				family = "agent_runtime_invocations"
			}
			if !exact("reason", "outcome", "action") {
				family = ""
			}
		case "response_journal_uncommitted":
			if kind == "model" {
				family, expectedKind = "artifacts", "agent_response_artifact"
			}
			if kind == "agent_runtime" {
				family, expectedKind = "agent_runtime_invocations", "agent_runtime_invocation"
			}
		case "budget_release_uncommitted":
			switch kind {
			case "model":
				family, expectedKind = "inference_requests", "inference_request"
			case "agent_runtime":
				family, expectedKind = "agent_runtime_invocations", "agent_runtime_invocation"
			case "route":
				family, expectedKind = "budget_reservations", "budget_reservation"
			}
		}
		if expectedKind != "" {
			actualKind, ok := text("result_ref_kind")
			if !ok || actualKind != expectedKind || !exact("reason", "outcome", "action", "result_ref_kind") {
				family = ""
			}
		}
		if family != "" {
			return known(ref(family))
		}
		return finish("unsupported", "unreviewed_result_discriminator")
	}
	if kind == "route" && status == "succeeded" && exact("action", "reservation_status", "budget_account_id") {
		action, _ := text("action")
		state, _ := text("reservation_status")
		_, accountOK := matchingID("budget_account_id")
		if action == "budget_reservation" && state == "reserved" && accountOK {
			return known(ref("budget_reservations"))
		}
	}
	if kind == "tool" && status == "succeeded" && exact("invocation_id", "observation_id") {
		invocation, invocationOK := matchingID("invocation_id")
		observation, observationOK := matchingID("observation_id")
		if invocationOK && observationOK && observation == *resultRef {
			return known(WorkerStepReference{"tool_invocations", invocation}, ref("observations"))
		}
	}
	if kind == "operation" && (status == "succeeded" || status == "unknown") && exact("operation_id", "state", "tool_id", "resource_ref") && stringFields("state", "tool_id", "resource_ref") {
		operation, ok := matchingID("operation_id")
		if ok && operation == *resultRef {
			return known(ref("operations"))
		}
	}
	if kind == "delegate" && status == "waiting" && exact("child_task_id") {
		child, ok := matchingID("child_task_id")
		if ok && child == *resultRef {
			return known(ref("tasks"))
		}
	}
	if kind == "verification" {
		verification, ok := matchingID("verification_id")
		if !ok {
			return finish("unsupported", "verification_identity_required")
		}
		if status == "waiting" && exact("verification_id", "required_level", "state") {
			state, _ := text("state")
			level, _ := text("required_level")
			if verification == *resultRef && state == "awaiting_independent_assurance" && len(level) == 2 && level[0] == 'V' && level[1] >= '1' && level[1] <= '5' {
				return known(ref("verifications"))
			}
		}
		if status == "succeeded" {
			action, _ := text("action")
			if exact("verification_id", "action", "verification_passed") && action == "retry" && string(fields["verification_passed"]) == "false" && verification == *resultRef {
				return known(ref("verifications"))
			}
			checkpoint, checkpointOK := matchingID("checkpoint_id")
			level, _ := text("level")
			v0 := exact("verification_id", "checkpoint_id", "level") && level == "V0"
			independent := exact("verification_id", "checkpoint_id", "independent") && string(fields["independent"]) == "true"
			if checkpointOK && checkpoint == *resultRef && (v0 || independent) {
				return known(WorkerStepReference{"verifications", verification}, ref("checkpoints"))
			}
		}
	}
	return finish("unsupported", "unreviewed_result_shape")
}
