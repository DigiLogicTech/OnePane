package observation

import (
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/policy"
)

func TestIntegrityHashIsDeterministicAndCoversObservedValue(t *testing.T) {
	o := Observation{
		WorkspaceID: "ws", SubjectRef: "dns://record/a", ObservationType: "dns.lookup",
		ProbeToolID: "dns.lookup", ProbeToolVersion: "1", Value: json.RawMessage(`{"ip":"10.0.0.1"}`),
		Label:      policy.DataLabel{WorkspaceID: "ws", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustAuthoritativeData},
		ObservedAt: 1700000000000,
	}
	first, err := IntegrityHash(o)
	if err != nil {
		t.Fatal(err)
	}
	second, err := IntegrityHash(o)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("hash not deterministic: %s != %s", first, second)
	}
	o.Value = json.RawMessage(`{"ip":"10.0.0.2"}`)
	changed, err := IntegrityHash(o)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("integrity hash did not change with observation value")
	}
}
