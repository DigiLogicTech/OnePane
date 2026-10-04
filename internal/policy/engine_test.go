package policy

import (
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/system"
)

func TestSystemModeMutationBoundary(t *testing.T) {
	for _, mode := range []system.Mode{system.ModeSafe, system.ModeReadOnly, system.ModeCommissioning, system.ModeRecovery, system.ModeDegraded} {
		if systemModeAllows(mode, authority.ActionMutate) {
			t.Fatalf("mode %s unexpectedly permits mutation", mode)
		}
	}
	if !systemModeAllows(system.ModeNormal, authority.ActionMutate) {
		t.Fatal("normal mode should permit mutation subject to authority")
	}
	if systemModeAllows(system.ModeMaintenance, authority.ActionExternalSend) {
		t.Fatal("maintenance mode must not permit external send")
	}
	if !systemModeAllows(system.ModeReadOnly, authority.ActionRead) {
		t.Fatal("read_only should permit reads")
	}
}

func TestRiskRequirementsComposeConservatively(t *testing.T) {
	if got := strongestVerification(VerificationV1, VerificationV4, VerificationV2); got != VerificationV4 {
		t.Fatalf("verification=%s", got)
	}
	if got := strongestApproval(ApprovalNone, ApprovalApprover, ApprovalAdmin); got != ApprovalAdmin {
		t.Fatalf("approval=%s", got)
	}
	if got := riskVerification(RiskCritical); got != VerificationV5 {
		t.Fatalf("critical verification=%s", got)
	}
	if got := riskApproval(RiskHigh); got != ApprovalApprover {
		t.Fatalf("high risk approval=%s", got)
	}
}

func TestInformationFlowEnforcesWorkspaceClearanceResidencyAndNoDeclassification(t *testing.T) {
	source := DataLabel{
		WorkspaceID: "ws1", Confidentiality: ConfidentialityConfidential,
		Residency: ResidencyOriginNode, Trust: TrustAuthoritativeData, OriginNodeID: "node1",
	}

	allowed := EvaluateInformationFlow(InformationFlowInput{
		Source:      source,
		Destination: FlowDestination{WorkspaceID: "ws1", Kind: DestinationOriginNode, NodeID: "node1", Clearance: ConfidentialitySecret},
		ProposedDerivedLabel: &DataLabel{
			WorkspaceID: "ws1", Confidentiality: ConfidentialitySecret,
			Residency: ResidencyOriginNode, Trust: TrustUnverifiedDerived, OriginNodeID: "node1",
		},
	})
	if !allowed.Allowed {
		t.Fatalf("expected allowed flow, reasons=%v", allowed.Reasons)
	}

	crossWorkspace := EvaluateInformationFlow(InformationFlowInput{
		Source:      source,
		Destination: FlowDestination{WorkspaceID: "ws2", Kind: DestinationOriginNode, NodeID: "node1", Clearance: ConfidentialitySecret},
	})
	if crossWorkspace.Allowed || !hasReason(crossWorkspace.Reasons, ReasonCrossWorkspace) {
		t.Fatalf("cross-workspace flow not denied correctly: %+v", crossWorkspace)
	}

	wrongNode := EvaluateInformationFlow(InformationFlowInput{
		Source:      source,
		Destination: FlowDestination{WorkspaceID: "ws1", Kind: DestinationTrustedNode, NodeID: "node2", Clearance: ConfidentialitySecret},
	})
	if wrongNode.Allowed || !hasReason(wrongNode.Reasons, ReasonResidency) {
		t.Fatalf("origin-node residency not enforced: %+v", wrongNode)
	}

	declassified := EvaluateInformationFlow(InformationFlowInput{
		Source:      source,
		Destination: FlowDestination{WorkspaceID: "ws1", Kind: DestinationOriginNode, NodeID: "node1", Clearance: ConfidentialitySecret},
		ProposedDerivedLabel: &DataLabel{
			WorkspaceID: "ws1", Confidentiality: ConfidentialityInternal,
			Residency: ResidencyAny, Trust: TrustUnverifiedDerived,
		},
	})
	if declassified.Allowed || !hasReason(declassified.Reasons, ReasonDeclassification) {
		t.Fatalf("declassification not denied: %+v", declassified)
	}
}

func TestVerifiedDerivedRequiresVerification(t *testing.T) {
	source := DataLabel{
		WorkspaceID: "ws1", Confidentiality: ConfidentialityInternal,
		Residency: ResidencyAny, Trust: TrustUntrustedContent,
	}
	derived := DataLabel{
		WorkspaceID: "ws1", Confidentiality: ConfidentialityInternal,
		Residency: ResidencyAny, Trust: TrustVerifiedDerived,
	}
	without := EvaluateInformationFlow(InformationFlowInput{
		Source:               source,
		Destination:          FlowDestination{WorkspaceID: "ws1", Kind: DestinationCloud, Clearance: ConfidentialityInternal},
		ProposedDerivedLabel: &derived,
	})
	if without.Allowed || !hasReason(without.Reasons, ReasonTrustElevation) {
		t.Fatalf("verified derivation should require verification: %+v", without)
	}
	with := EvaluateInformationFlow(InformationFlowInput{
		Source:               source,
		Destination:          FlowDestination{WorkspaceID: "ws1", Kind: DestinationCloud, Clearance: ConfidentialityInternal},
		ProposedDerivedLabel: &derived, VerificationEstablished: true,
	})
	if !with.Allowed {
		t.Fatalf("verified derivation should be allowed after verification: %+v", with)
	}
}

func hasReason(reasons []Reason, want Reason) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
