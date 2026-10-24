package recoveryinventory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
)

func modelAttributionRequest(t testing.TB) string {
	t.Helper()
	raw, err := json.Marshal(agentprotocol.Request{
		ProtocolVersion: agentprotocol.Version, RequestID: "request-1", WorkspaceID: "tenant-1",
		TaskID: "task-1", AttemptID: "attempt-1", PrincipalID: "worker-1", Role: "builder",
		Objective: "Observe only", Constraints: json.RawMessage(`{"limit":1}`),
		Context:         []agentprotocol.ContextSection{{ID: "ctx-1", Kind: "data", Trust: "UNTRUSTED_CONTENT", Content: json.RawMessage(`{"mention":"other-task"}`)}},
		ContextManifest: json.RawMessage(`{}`), PermittedProposalTypes: []agentprotocol.ProposalType{agentprotocol.ProposalWait},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func modelAttributionEnvelope(t testing.TB, inner string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"messages":    []map[string]string{{"role": "system", "content": "Task mentions in this text are not ownership"}, {"role": "user", "content": inner}},
		"temperature": 0.1, "max_tokens": 2048, "stream": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertModelAttribution(t testing.TB, raw []byte, state ModelRequestAttributionState) ModelRequestAttribution {
	t.Helper()
	before := bytes.Clone(raw)
	got := ClassifyModelRequestAttribution(raw)
	if !bytes.Equal(raw, before) {
		t.Fatal("classification mutated stored payload")
	}
	if got.State != state {
		t.Fatalf("state=%s reason=%s want=%s", got.State, got.Reason, state)
	}
	if got.ParserVersion != ModelRequestAttributionParserVersion || got.Reason == "" {
		t.Fatalf("missing version/reason: %+v", got)
	}
	if len(raw) > ModelRequestAttributionMaxBytes {
		if got.ExactBytesHash != "" {
			t.Fatal("oversized input was hashed")
		}
	} else {
		sum := sha256.Sum256(append([]byte(ModelRequestAttributionHashDomain), raw...))
		if got.ExactBytesHash != "sha256:"+hex.EncodeToString(sum[:]) {
			t.Fatal("hash does not bind domain and exact original bytes")
		}
	}
	if got.State != ModelRequestAttributionKnownCandidate && (got.InnerTaskID != "" || got.InnerAttemptID != "" || got.InnerRequestID != "" || got.InnerWorkspaceID != "" || got.InnerPrincipalID != "") {
		t.Fatal("unsupported payload retained partial identities")
	}
	if again := ClassifyModelRequestAttribution(raw); again != got {
		t.Fatalf("classification is not deterministic: %+v / %+v", got, again)
	}
	return got
}

func TestModelRequestAttributionKnownCandidateAndExactBytes(t *testing.T) {
	inner := modelAttributionRequest(t)
	raw := modelAttributionEnvelope(t, inner)
	got := assertModelAttribution(t, raw, ModelRequestAttributionKnownCandidate)
	if got.InnerTaskID != "task-1" || got.InnerAttemptID != "attempt-1" || got.InnerRequestID != "request-1" || got.InnerWorkspaceID != "tenant-1" || got.InnerPrincipalID != "worker-1" {
		t.Fatalf("wrong nested identity: %+v", got)
	}
	// Whitespace and key-escape changes preserve the candidate but must not
	// collapse distinct stored bytes to one observation identity.
	variants := [][]byte{append([]byte(" \n"), raw...), modelAttributionEnvelope(t, strings.Replace(inner, `"task_id"`, `"t\u0061sk_id"`, 1))}
	for _, v := range variants {
		other := assertModelAttribution(t, v, ModelRequestAttributionKnownCandidate)
		if other.InnerTaskID != got.InnerTaskID || other.ExactBytesHash == got.ExactBytesHash {
			t.Fatal("canonicalized stored bytes or lost escaped identity")
		}
	}
	// The Task name mentioned inside data is not selected as ownership.
	if strings.Contains(got.InnerTaskID, "other") {
		t.Fatal("attributed arbitrary context text")
	}
}

func TestModelRequestAttributionRejectsUnsupportedAndAmbiguousPayloads(t *testing.T) {
	inner := modelAttributionRequest(t)
	type sample struct {
		name  string
		raw   []byte
		state ModelRequestAttributionState
	}
	innerSample := func(name, value string, state ModelRequestAttributionState) sample {
		return sample{name, modelAttributionEnvelope(t, value), state}
	}
	samples := []sample{
		{"empty", nil, ModelRequestAttributionMalformed},
		{"invalid_utf8", []byte{'"', 0xff, '"'}, ModelRequestAttributionMalformed},
		{"trailing", []byte(`{} {}`), ModelRequestAttributionMalformed},
		{"array", []byte(`[]`), ModelRequestAttributionUnsupported},
		{"null", []byte(`null`), ModelRequestAttributionUnsupported},
		{"missing_messages", []byte(`{}`), ModelRequestAttributionUnsupported},
		{"duplicate_outer", []byte(`{"messages":[],"mess\u0061ges":[]}`), ModelRequestAttributionAmbiguous},
		{"duplicate_message", []byte(`{"messages":[{"role":"system","role":"user"},{"role":"user","content":"{}"}]}`), ModelRequestAttributionAmbiguous},
		{"null_content", []byte(`{"messages":[{"role":"system","content":null},{"role":"user","content":"{}"}]}`), ModelRequestAttributionUnsupported},
		innerSample("missing_attempt", strings.Replace(inner, `"attempt_id":"attempt-1",`, "", 1), ModelRequestAttributionUnsupported),
		innerSample("missing_task", strings.Replace(inner, `"task_id":"task-1",`, "", 1), ModelRequestAttributionUnsupported),
		innerSample("blank_attempt", strings.Replace(inner, `"attempt-1"`, `"  "`, 1), ModelRequestAttributionUnsupported),
		innerSample("null_task", strings.Replace(inner, `"task-1"`, `null`, 1), ModelRequestAttributionUnsupported),
		innerSample("foreign_version", strings.Replace(inner, `"v1"`, `"v2"`, 1), ModelRequestAttributionUnsupported),
		innerSample("unknown_field", strings.TrimSuffix(inner, "}")+`,"unexpected":true}`, ModelRequestAttributionUnsupported),
		innerSample("wrong_identity_type", strings.Replace(inner, `"task-1"`, `7`, 1), ModelRequestAttributionUnsupported),
		innerSample("duplicate_inner", strings.TrimSuffix(inner, "}")+`,"task_id":"other"}`, ModelRequestAttributionAmbiguous),
		innerSample("escaped_duplicate", strings.TrimSuffix(inner, "}")+`,"task\u005fid":"other"}`, ModelRequestAttributionAmbiguous),
		innerSample("case_alias", strings.Replace(inner, `"task_id"`, `"TASK_ID"`, 1), ModelRequestAttributionAmbiguous),
		innerSample("case_alias_with_original", strings.TrimSuffix(inner, "}")+`,"Task_Id":"other"}`, ModelRequestAttributionAmbiguous),
		innerSample("nested_duplicate", strings.Replace(inner, `{"limit":1}`, `{"limit":1,"l\u0069mit":2}`, 1), ModelRequestAttributionAmbiguous),
		innerSample("unpaired_identity_surrogate", strings.Replace(inner, `"task-1"`, `"\ud800"`, 1), ModelRequestAttributionUnsupported),
		innerSample("replacement_identity", strings.Replace(inner, `"task-1"`, `"\ufffd"`, 1), ModelRequestAttributionUnsupported),
		innerSample("malformed_inner", `{"task_id":`, ModelRequestAttributionMalformed),
		innerSample("inner_trailing", inner+` {}`, ModelRequestAttributionMalformed),
	}
	for _, s := range samples {
		t.Run(s.name, func(t *testing.T) { assertModelAttribution(t, s.raw, s.state) })
	}
}

func TestModelRequestAttributionWholeInputAndIdentityBounds(t *testing.T) {
	inner := modelAttributionRequest(t)
	base := modelAttributionEnvelope(t, inner)
	for _, n := range []int{ModelRequestAttributionMaxBytes, ModelRequestAttributionMaxBytes + 1} {
		raw := append(bytes.Clone(base), bytes.Repeat([]byte(" "), n-len(base))...)
		state := ModelRequestAttributionKnownCandidate
		if n > ModelRequestAttributionMaxBytes {
			state = ModelRequestAttributionOversized
		}
		assertModelAttribution(t, raw, state)
	}
	for _, n := range []int{ModelRequestAttributionMaxIdentityBytes, ModelRequestAttributionMaxIdentityBytes + 1} {
		state := ModelRequestAttributionKnownCandidate
		if n > ModelRequestAttributionMaxIdentityBytes {
			state = ModelRequestAttributionLimitExceeded
		}
		assertModelAttribution(t, modelAttributionEnvelope(t, strings.Replace(inner, `task-1`, strings.Repeat("x", n), 1)), state)
	}
	// Identity limits count UTF-8 bytes, not code points.
	assertModelAttribution(t, modelAttributionEnvelope(t, strings.Replace(inner, `task-1`, strings.Repeat("界", 171), 1)), ModelRequestAttributionLimitExceeded)
	for _, n := range []int{ModelRequestAttributionMaxDepth - 1, ModelRequestAttributionMaxDepth} {
		value := strings.Repeat("[", n) + "0" + strings.Repeat("]", n)
		raw := modelAttributionEnvelope(t, strings.Replace(inner, `{"limit":1}`, value, 1))
		want := ModelRequestAttributionKnownCandidate
		if n == ModelRequestAttributionMaxDepth {
			want = ModelRequestAttributionLimitExceeded
		}
		assertModelAttribution(t, raw, want)
	}
	// Entry and token budgets are shared across the envelope and inner JSON.
	for _, family := range []string{"entries", "tokens"} {
		var value string
		if family == "entries" {
			var fields []string
			for i := 0; i < ModelRequestAttributionMaxEntries; i++ {
				fields = append(fields, fmt.Sprintf(`"k%d":0`, i))
			}
			value = "{" + strings.Join(fields, ",") + "}"
		} else {
			value = "[" + strings.Repeat("0,", ModelRequestAttributionMaxTokens-1) + "0]"
		}
		raw := modelAttributionEnvelope(t, strings.Replace(inner, `{"limit":1}`, value, 1))
		if len(raw) > ModelRequestAttributionMaxBytes {
			t.Fatal("fixture exceeded bytes before structural limit")
		}
		got := assertModelAttribution(t, raw, ModelRequestAttributionLimitExceeded)
		if !strings.Contains(got.Reason, strings.TrimSuffix(family, "s")+"_limit") && family != "entries" {
			t.Fatalf("wrong structural limit: %+v", got)
		}
		if family == "entries" && !strings.Contains(got.Reason, "entry_limit") {
			t.Fatalf("wrong entry limit: %+v", got)
		}
	}
}

func TestModelRequestTokenBudgetExactBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		budget modelRequestTokenBudget
		raw    string
		reason string
	}{
		{"last_token", modelRequestTokenBudget{tokens: ModelRequestAttributionMaxTokens - 1}, "0", ""},
		{"one_token_over", modelRequestTokenBudget{tokens: ModelRequestAttributionMaxTokens}, "0", "token_limit"},
		{"last_entry", modelRequestTokenBudget{entries: ModelRequestAttributionMaxEntries - 1}, `{"k":0}`, ""},
		{"one_entry_over", modelRequestTokenBudget{entries: ModelRequestAttributionMaxEntries}, `{"k":0}`, "entry_limit"},
		{"last_depth", modelRequestTokenBudget{}, strings.Repeat("[", ModelRequestAttributionMaxDepth) + "0" + strings.Repeat("]", ModelRequestAttributionMaxDepth), ""},
		{"one_depth_over", modelRequestTokenBudget{}, strings.Repeat("[", ModelRequestAttributionMaxDepth+1) + "0" + strings.Repeat("]", ModelRequestAttributionMaxDepth+1), "depth_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := tc.budget.scan([]byte(tc.raw), false)
			if tc.reason == "" {
				if failure != nil {
					t.Fatalf("boundary failed: %+v", failure)
				}
			} else if failure == nil || failure.reason != tc.reason {
				t.Fatalf("wrong boundary failure: %+v", failure)
			}
			if tc.budget.tokens > ModelRequestAttributionMaxTokens || tc.budget.entries > ModelRequestAttributionMaxEntries {
				t.Fatal("parser exceeded shared work bound")
			}
		})
	}
}

func FuzzModelRequestAttribution(f *testing.F) {
	f.Add(modelAttributionEnvelope(f, modelAttributionRequest(f)))
	f.Add([]byte(`{"messages":[],"messages":null}`))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, raw []byte) {
		got := ClassifyModelRequestAttribution(raw)
		switch got.State {
		case ModelRequestAttributionKnownCandidate, ModelRequestAttributionUnsupported, ModelRequestAttributionMalformed, ModelRequestAttributionAmbiguous, ModelRequestAttributionOversized, ModelRequestAttributionLimitExceeded:
		default:
			t.Fatalf("unclassified result: %+v", got)
		}
		assertModelAttribution(t, raw, got.State)
		if got.State == ModelRequestAttributionKnownCandidate && (strings.TrimSpace(got.InnerTaskID) == "" || strings.TrimSpace(got.InnerAttemptID) == "") {
			t.Fatal("candidate lacks original lineage")
		}
	})
}
