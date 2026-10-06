package inference

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDeploymentTransitionRequiresQualificationBeforeReady(t *testing.T) {
	if CanDeploymentTransition(DeploymentDiscovered, DeploymentReady) {
		t.Fatal("discovered deployment bypassed qualification")
	}
	if !CanDeploymentTransition(DeploymentDiscovered, DeploymentQualifying) || !CanDeploymentTransition(DeploymentQualifying, DeploymentReady) {
		t.Fatal("expected qualification path is not available")
	}
}

func TestDeploymentFingerprintCanonicalizesRuntimeConfig(t *testing.T) {
	node := "node_local"
	a, err := normalizeJSON(json.RawMessage(`{"gpu":0,"ctx":65536}`), "{}")
	if err != nil {
		t.Fatal(err)
	}
	b, err := normalizeJSON(json.RawMessage(`{ "ctx": 65536, "gpu": 0 }`), "{}")
	if err != nil {
		t.Fatal(err)
	}
	cmd := RegisterDeploymentCommand{ModelID: "model_x", NodeID: &node}
	if deploymentFingerprint(cmd, a) != deploymentFingerprint(cmd, b) {
		t.Fatal("semantically identical runtime configs produced different fingerprints")
	}
}

func TestProviderConfigRejectsRawSecretLikeFields(t *testing.T) {
	raw, err := normalizeJSON(json.RawMessage(`{"base_url":"https://example.invalid","authorization":"Bearer nope"}`), "{}")
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectSecretLikeJSON(raw); !errors.Is(err, ErrUnsafeConfig) {
		t.Fatalf("expected unsafe config error, got %v", err)
	}
}

func TestValidStates(t *testing.T) {
	if !ValidProviderStatus(ProviderConnected) || !ValidModelTrustState(ModelQuarantined) || !ValidDeploymentStatus(DeploymentReady) || !ValidResidencyState(ResidencyResident) {
		t.Fatal("known enum value rejected")
	}
}

func TestSystemTrustedModelStateIsReserved(t *testing.T) {
	if !ValidModelTrustState(ModelTrusted) {
		t.Fatal("trusted storage state should remain valid")
	}
	// Public service commands reserve ModelTrusted for future built-in/signed qualification paths.
}
