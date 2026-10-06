package agentruntime

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestEligibility(t *testing.T) {
	c := Connection{Status: StatusConnected, TrustState: TrustUserTrusted, OperatingMode: ProposalOnly}
	e := c.Eligibility()
	if !e.Schedulable || e.ToolCallback || e.OutputTrust != "unverified_derived" {
		t.Fatalf("unexpected proposal eligibility: %+v", e)
	}
	c.OperatingMode = GatewayMediated
	e = c.Eligibility()
	if !e.Schedulable || !e.ToolCallback {
		t.Fatalf("unexpected mediated eligibility: %+v", e)
	}
	c.OperatingMode = Unmanaged
	e = c.Eligibility()
	if e.Schedulable || e.OutputTrust != "untrusted_content" {
		t.Fatalf("unmanaged runtime became schedulable: %+v", e)
	}
}

func TestDefaultDataPolicyIsPublicAndNeverRawSecrets(t *testing.T) {
	raw, err := normalizeDataPolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	var p DataPolicy
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.MaxConfidentiality != "public" || p.DestinationKind != "untrusted" || p.AllowRawSecrets {
		t.Fatalf("unsafe default policy: %+v", p)
	}
}

func TestRejectSecretLikeEndpoint(t *testing.T) {
	raw, _ := canonicalJSON(json.RawMessage(`{"base_url":"http://localhost","api_key":"nope"}`), "{}")
	if err := rejectSecretLikeJSON(raw); !errors.Is(err, ErrUnsafeConfig) {
		t.Fatalf("expected unsafe config, got %v", err)
	}
}

func TestBuiltinAgentProtocolSupportsHermesBridgeModes(t *testing.T) {
	r := NewRegistry()
	if err := RegisterBuiltinAdapters(r); err != nil {
		t.Fatal(err)
	}
	d, ok := r.Resolve("builtin.agent_protocol_http", "1")
	if !ok || !d.Supports(ProposalOnly) || !d.Supports(GatewayMediated) || d.Supports(Unmanaged) {
		t.Fatalf("unexpected descriptor: %+v ok=%v", d, ok)
	}
}

func TestTrustedAdapterStateIsNotEquivalentToRuntimeOutputTrust(t *testing.T) {
	c := Connection{Status: StatusConnected, TrustState: TrustTrustedAdapter, OperatingMode: ProposalOnly}
	if e := c.Eligibility(); e.OutputTrust != "unverified_derived" {
		t.Fatalf("external runtime output became trusted: %+v", e)
	}
}

func TestUntrustedRuntimeIsNotAutonomouslySchedulable(t *testing.T) {
	c := Connection{Status: StatusConnected, TrustState: TrustUntrusted, OperatingMode: ProposalOnly}
	if e := c.Eligibility(); e.Schedulable {
		t.Fatalf("untrusted runtime became schedulable: %+v", e)
	}
}
