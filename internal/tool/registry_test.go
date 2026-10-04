package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type registryAdapter struct{ id, version string }

func (a registryAdapter) ID() string      { return a.id }
func (a registryAdapter) Version() string { return a.version }
func (a registryAdapter) Invoke(context.Context, AdapterRequest) (AdapterResult, error) {
	return AdapterResult{Result: json.RawMessage(`{}`)}, nil
}

func TestRegistryRejectsAdapterIdentityMismatchAndDuplicates(t *testing.T) {
	def := Definition{ID: "t", Version: "1", CapabilityID: "c", Mode: authority.ActionRead, AdapterID: "a", AdapterVersion: "1", Risk: policy.RiskLow, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone}
	reg := NewRegistry()
	if err := reg.Register(def, registryAdapter{id: "wrong", version: "1"}); !errors.Is(err, ErrAdapterIdentityMismatch) {
		t.Fatalf("err=%v", err)
	}
	if err := reg.Register(def, registryAdapter{id: "a", version: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(def, registryAdapter{id: "a", version: "1"}); !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("err=%v", err)
	}
}
