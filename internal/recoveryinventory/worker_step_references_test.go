package recoveryinventory

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestRecoveryWorkerStepReferencesReviewedShapes(t *testing.T) {
	for _, tc := range []struct {
		name, kind, status, detail string
		refs                       []WorkerStepReference
	}{
		{"model_response", "model", "succeeded", `{"message":"do not infer ownership from this text"}`, []WorkerStepReference{{"artifacts", "result"}}},
		{"model_failure", "model", "failed", `{"error":"transport preflight failed"}`, []WorkerStepReference{{"inference_requests", "result"}}},
		{"runtime_response", "agent_runtime", "succeeded", `{"message":"ok"}`, []WorkerStepReference{{"agent_runtime_invocations", "result"}}},
		{"runtime_failure", "agent_runtime", "failed", `{"error":"failed"}`, []WorkerStepReference{{"agent_runtime_invocations", "result"}}},
		{"budget", "route", "succeeded", `{"action":"budget_reservation","reservation_status":"reserved","budget_account_id":"account"}`, []WorkerStepReference{{"budget_reservations", "result"}}},
		{"tool", "tool", "succeeded", `{"invocation_id":"invocation","observation_id":"result"}`, []WorkerStepReference{{"tool_invocations", "invocation"}, {"observations", "result"}}},
		{"operation", "operation", "succeeded", `{"operation_id":"result","state":"committed","tool_id":"tool","resource_ref":"resource"}`, []WorkerStepReference{{"operations", "result"}}},
		{"operation_unknown", "operation", "unknown", `{"operation_id":"result","state":"unknown_outcome","tool_id":"tool","resource_ref":"resource"}`, []WorkerStepReference{{"operations", "result"}}},
		{"delegate", "delegate", "waiting", `{"child_task_id":"result"}`, []WorkerStepReference{{"tasks", "result"}}},
		{"verification_wait", "verification", "waiting", `{"verification_id":"result","required_level":"V2","state":"awaiting_independent_assurance"}`, []WorkerStepReference{{"verifications", "result"}}},
		{"verification_retry", "verification", "succeeded", `{"verification_id":"result","action":"retry","verification_passed":false}`, []WorkerStepReference{{"verifications", "result"}}},
		{"verification_v0", "verification", "succeeded", `{"verification_id":"verification","checkpoint_id":"result","level":"V0"}`, []WorkerStepReference{{"verifications", "verification"}, {"checkpoints", "result"}}},
		{"verification_independent", "verification", "succeeded", `{"verification_id":"verification","checkpoint_id":"result","independent":true}`, []WorkerStepReference{{"verifications", "verification"}, {"checkpoints", "result"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := "result"
			got := ClassifyWorkerStepReferences(tc.kind, tc.status, &ref, []byte(tc.detail))
			if got.State != "known_candidate" || !reflect.DeepEqual(got.References, tc.refs) {
				t.Fatalf("wrong references: %+v", got)
			}
			if got.ParserVersion != WorkerStepReferenceParserVersion || got.ExactBytesHash == "" || got.Reason == "" {
				t.Fatal("missing exact observation identity")
			}
		})
	}
}

func TestRecoveryWorkerStepReferencesReconciliationDiscriminators(t *testing.T) {
	for _, tc := range []struct{ kind, outcome, discriminator, family string }{
		{"model", "unknown_or_response_unreadable", "", "inference_requests"},
		{"agent_runtime", "unknown_or_response_unreadable", "", "agent_runtime_invocations"},
		{"model", "response_journal_uncommitted", "agent_response_artifact", "artifacts"},
		{"agent_runtime", "response_journal_uncommitted", "agent_runtime_invocation", "agent_runtime_invocations"},
		{"model", "budget_release_uncommitted", "inference_request", "inference_requests"},
		{"agent_runtime", "budget_release_uncommitted", "agent_runtime_invocation", "agent_runtime_invocations"},
		{"route", "budget_release_uncommitted", "budget_reservation", "budget_reservations"},
	} {
		t.Run(tc.kind+"/"+tc.outcome, func(t *testing.T) {
			ref := "retained-result"
			detail := map[string]any{"reason": "agent_worker_dispatch_outcome_requires_operator_reconciliation", "outcome": tc.outcome, "action": "operator_reconciliation_required"}
			if tc.discriminator != "" {
				detail["result_ref_kind"] = tc.discriminator
			}
			raw, err := json.Marshal(detail)
			if err != nil {
				t.Fatal(err)
			}
			got := ClassifyWorkerStepReferences(tc.kind, "unknown", &ref, raw)
			if got.State != "known_candidate" || !reflect.DeepEqual(got.References, []WorkerStepReference{{tc.family, ref}}) {
				t.Fatalf("wrong unknown-outcome reference: %+v", got)
			}
			// The discriminator is an exact typed boundary, never a hint.
			detail["result_ref_kind"] = "arbitrary_table"
			raw, _ = json.Marshal(detail)
			got = ClassifyWorkerStepReferences(tc.kind, "unknown", &ref, raw)
			if got.State == "known_candidate" || len(got.References) != 0 {
				t.Fatalf("unknown discriminator produced identities: %+v", got)
			}
		})
	}
}

func TestRecoveryWorkerStepReferencesFailClosedAndBoundWork(t *testing.T) {
	for _, tc := range []struct{ name, kind, status, ref, detail string }{
		{"missing_ref", "model", "succeeded", "", `{"message":"ok"}`},
		{"whitespace_identity", "model", "succeeded", " result", `{"message":"ok"}`},
		{"control_identity", "model", "succeeded", "result\x00", `{"message":"ok"}`},
		{"oversized_ref", "model", "succeeded", strings.Repeat("x", 513), `{"message":"ok"}`},
		{"unknown_kind", "future_kind", "succeeded", "result", `{}`},
		{"unknown_status", "model", "retry", "result", `{"message":"ok"}`},
		{"unknown_field", "model", "succeeded", "result", `{"message":"ok","operation_id":"hidden"}`},
		{"null_message", "model", "succeeded", "result", `{"message":null}`},
		{"array", "model", "succeeded", "result", `[]`},
		{"null", "model", "succeeded", "result", `null`},
		{"malformed", "model", "succeeded", "result", `{`},
		{"duplicate", "tool", "succeeded", "result", `{"invocation_id":"a","observation_id":"result","observation_id":"other"}`},
		{"escaped_duplicate", "tool", "succeeded", "result", `{"invocation_id":"a","observation_id":"result","observation_\u0069d":"other"}`},
		{"case_alias", "tool", "succeeded", "result", `{"invocation_id":"a","Observation_ID":"result"}`},
		{"tool_mismatch", "tool", "succeeded", "result", `{"invocation_id":"a","observation_id":"other"}`},
		{"tool_null", "tool", "succeeded", "result", `{"invocation_id":null,"observation_id":"result"}`},
		{"operation_mismatch", "operation", "unknown", "result", `{"operation_id":"other","state":"unknown_outcome","tool_id":"t","resource_ref":"r"}`},
		{"child_mismatch", "delegate", "waiting", "result", `{"child_task_id":"other"}`},
		{"retry_is_not_pass", "verification", "succeeded", "result", `{"verification_id":"result","action":"retry","verification_passed":true}`},
		{"retry_string_bool", "verification", "succeeded", "result", `{"verification_id":"result","action":"retry","verification_passed":"false"}`},
		{"checkpoint_mismatch", "verification", "succeeded", "result", `{"verification_id":"v","checkpoint_id":"other","independent":true}`},
		{"false_independence", "verification", "succeeded", "result", `{"verification_id":"v","checkpoint_id":"result","independent":false}`},
		{"missing_discriminator", "model", "unknown", "result", `{"reason":"r","outcome":"response_journal_uncommitted","action":"operator_reconciliation_required"}`},
		{"unreviewed_outcome", "model", "unknown", "result", `{"reason":"r","outcome":"new_outcome","action":"operator_reconciliation_required"}`},
		{"extra_step_reference", "model", "unknown", "result", `{"reason":"r","outcome":"unknown_or_response_unreadable","action":"operator_reconciliation_required","operation_id":"hidden"}`},
		{"invalid_utf8", "model", "succeeded", "result", "{\"message\":\"\xff\"}"},
		{"unpaired_identity_surrogate", "tool", "succeeded", "result", `{"invocation_id":"\ud800","observation_id":"result"}`},
		{"depth", "model", "succeeded", "result", strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33)},
		{"oversized_detail", "model", "succeeded", "result", strings.Repeat("x", ModelRequestAttributionMaxBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(tc.detail)
			before := bytes.Clone(raw)
			got := ClassifyWorkerStepReferences(tc.kind, tc.status, &tc.ref, raw)
			if got.State == "known_candidate" || len(got.References) != 0 || got.Reason == "" {
				t.Fatalf("unsafe partial attribution: %+v", got)
			}
			if !bytes.Equal(raw, before) {
				t.Fatal("mutated source bytes")
			}
			if got.State == "oversized" && got.ExactBytesHash != "" {
				t.Fatal("hashed over-budget input")
			}
		})
	}
}

func TestRecoveryWorkerStepReferencesHashBindsExactSlice(t *testing.T) {
	ref := "result"
	raw := []byte(`{"message":"ok"}`)
	base := ClassifyWorkerStepReferences("model", "succeeded", &ref, raw)
	if again := ClassifyWorkerStepReferences("model", "succeeded", &ref, raw); !reflect.DeepEqual(again, base) {
		t.Fatal("nondeterministic parser")
	}
	variants := []WorkerStepReferences{
		ClassifyWorkerStepReferences("model", "succeeded", &ref, append([]byte(" \n"), raw...)),
		ClassifyWorkerStepReferences("agent_runtime", "succeeded", &ref, raw),
		ClassifyWorkerStepReferences("model", "failed", &ref, raw),
		ClassifyWorkerStepReferences("model", "succeeded", nil, raw),
	}
	empty := ""
	variants = append(variants, ClassifyWorkerStepReferences("model", "succeeded", &empty, raw))
	seen := map[string]bool{base.ExactBytesHash: true}
	for _, other := range variants {
		if other.ExactBytesHash == "" || seen[other.ExactBytesHash] {
			t.Fatal("collapsed distinct exact inputs")
		}
		seen[other.ExactBytesHash] = true
	}
}

func TestRecoveryWorkerStepReferencesAmbiguityAndJSONBudgets(t *testing.T) {
	ref := "result"
	var object strings.Builder
	object.WriteByte('{')
	for i := 0; i < 2049; i++ {
		if i > 0 {
			object.WriteByte(',')
		}
		object.WriteString(strconv.Quote(strconv.Itoa(i)))
		object.WriteString(":0")
	}
	object.WriteByte('}')
	for _, tc := range []struct{ raw, state string }{
		{`{"message":"one","mess\u0061ge":"two"}`, "ambiguous"},
		{`{"message":"ok"} {}`, "malformed"},
		{strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33), "limit_exceeded"},
		{"[" + strings.Repeat("0,", 8192) + "0]", "limit_exceeded"},
		{object.String(), "limit_exceeded"},
	} {
		got := ClassifyWorkerStepReferences("model", "succeeded", &ref, []byte(tc.raw))
		if got.State != tc.state || len(got.References) != 0 {
			t.Fatalf("budget/ambiguity state=%s want=%s", got.State, tc.state)
		}
	}
	// Caps count UTF-8 bytes, not characters. A normal full-bound input still
	// works; one additional byte cannot be parsed or hashed.
	raw := []byte(`{"message":"` + strings.Repeat("x", ModelRequestAttributionMaxBytes-len(`{"message":""}`)) + `"}`)
	got := ClassifyWorkerStepReferences("model", "succeeded", &ref, raw)
	if len(raw) != ModelRequestAttributionMaxBytes || got.State != "known_candidate" {
		t.Fatalf("exact byte boundary failed: %d %+v", len(raw), got)
	}
	raw = append(raw, ' ')
	got = ClassifyWorkerStepReferences("model", "succeeded", &ref, raw)
	if got.State != "oversized" || got.ExactBytesHash != "" || len(got.References) != 0 {
		t.Fatal("hashed/accepted oversized JSON")
	}
	unicodeRef := strings.Repeat("é", 256)
	got = ClassifyWorkerStepReferences("model", "succeeded", &unicodeRef, []byte(`{"message":"ok"}`))
	if got.State != "known_candidate" {
		t.Fatal("512byte UTF-8 identity rejected")
	}
	unicodeRef += "é"
	got = ClassifyWorkerStepReferences("model", "succeeded", &unicodeRef, []byte(`{"message":"ok"}`))
	if got.State != "oversized" || got.ExactBytesHash != "" {
		t.Fatal("identity cap counted runes instead of bytes")
	}
}
