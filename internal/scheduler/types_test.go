package scheduler

import (
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"testing"
)

func label() policy.DataLabel {
	return policy.DataLabel{WorkspaceID: "ws", Confidentiality: policy.ConfidentialityPublic, Residency: policy.ResidencyAny, Trust: policy.TrustUserInstruction}
}
func cloudDest() policy.FlowDestination {
	return policy.FlowDestination{WorkspaceID: "ws", Kind: policy.DestinationCloud, Clearance: policy.ConfidentialityPublic}
}
func baseCandidate(id string) Candidate {
	return Candidate{ID: id, Kind: CandidateModel, Status: "ready", TrustState: "user_trusted", CostClass: CostFree, HardZeroIncrementalCost: true, Qualification: QualSupported, ProtocolLevel: "L2", CapabilityIDs: []string{"agent.reason"}, ContextMax: 64000, Destination: cloudDest(), AllowedResidency: []string{"any"}, Schedulable: true}
}
func TestRoutePrefersProvenZeroCost(t *testing.T) {
	paid := baseCandidate("paid")
	paid.CostClass = CostPaid
	free := baseCandidate("free")
	d, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label(), PreferZeroIncrementalCost: true}, []Candidate{paid, free})
	if err != nil {
		t.Fatal(err)
	}
	if d.Selected == nil || d.Selected.Candidate.ID != "free" {
		t.Fatalf("selected %#v", d.Selected)
	}
}
func TestRouteZeroCostFailsClosed(t *testing.T) {
	c := baseCandidate("omni")
	c.CostClass = CostFree
	c.HardZeroIncrementalCost = false
	_, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label(), RequireZeroIncrementalCost: true}, []Candidate{c})
	if err != ErrNoEligibleCandidate {
		t.Fatalf("got %v", err)
	}
}
func TestRouteUntestedRequiresOptIn(t *testing.T) {
	c := baseCandidate("new")
	c.Qualification = QualUntested
	c.CapabilityIDs = nil
	_, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L0", DataLabel: label()}, []Candidate{c})
	if err != ErrNoEligibleCandidate {
		t.Fatalf("got %v", err)
	}
	d, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L0", DataLabel: label(), AllowUntested: true}, []Candidate{c})
	if err != nil || d.Selected == nil {
		t.Fatalf("%v %#v", err, d)
	}
}
func TestRouteInformationFlowDenied(t *testing.T) {
	c := baseCandidate("cloud")
	secret := label()
	secret.Confidentiality = policy.ConfidentialitySecret
	_, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: secret}, []Candidate{c})
	if err != ErrNoEligibleCandidate {
		t.Fatalf("got %v", err)
	}
}

func TestRouteSubscriptionAllowanceProtectedByDefault(t *testing.T) {
	free := baseCandidate("free")
	sub := baseCandidate("chatgpt-plan")
	sub.CostClass = CostIncludedSubscription
	sub.HardZeroIncrementalCost = false

	d, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label()}, []Candidate{sub, free})
	if err != nil {
		t.Fatal(err)
	}
	if d.Selected == nil || d.Selected.Candidate.ID != "free" {
		t.Fatalf("subscription allowance should remain protected, selected %#v", d.Selected)
	}
	found := false
	for _, r := range d.Rejected {
		if r.CandidateID == "chatgpt-plan" && r.Reason == "candidate consumes protected subscription allowance" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected subscription candidate rejection: %#v", d.Rejected)
	}
}

func TestRouteSubscriptionAllowanceRequiresExplicitOptIn(t *testing.T) {
	sub := baseCandidate("chatgpt-plan")
	sub.CostClass = CostIncludedSubscription
	sub.HardZeroIncrementalCost = false

	_, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label()}, []Candidate{sub})
	if err != ErrNoEligibleCandidate {
		t.Fatalf("got %v", err)
	}

	d, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label(), AllowSubscriptionUsage: true}, []Candidate{sub})
	if err != nil || d.Selected == nil || d.Selected.Candidate.ID != "chatgpt-plan" {
		t.Fatalf("explicit subscription opt-in failed: %v %#v", err, d.Selected)
	}
}

func TestRouteZeroCostDoesNotConsumeSubscriptionAllowance(t *testing.T) {
	sub := baseCandidate("chatgpt-plan")
	sub.CostClass = CostIncludedSubscription
	sub.HardZeroIncrementalCost = false
	_, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label(), AllowSubscriptionUsage: true, RequireZeroIncrementalCost: true}, []Candidate{sub})
	if err != ErrNoEligibleCandidate {
		t.Fatalf("got %v", err)
	}
}

func TestRoutePrefersFreeBeforeOptedInSubscription(t *testing.T) {
	free := baseCandidate("omniroute-free")
	sub := baseCandidate("chatgpt-plan")
	sub.CostClass = CostIncludedSubscription
	sub.HardZeroIncrementalCost = false
	d, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label(), AllowSubscriptionUsage: true, PreferZeroIncrementalCost: true}, []Candidate{sub, free})
	if err != nil {
		t.Fatal(err)
	}
	if d.Selected == nil || d.Selected.Candidate.ID != "omniroute-free" {
		t.Fatalf("expected free capacity before subscription allowance: %#v", d.Selected)
	}
}

func TestRoutePotentialSpendProtectedByDefault(t *testing.T) {
	for _, tc := range []struct {
		name  string
		class CostClass
		hard  bool
	}{
		{"paid", CostPaid, false},
		{"provider-managed", CostProviderManaged, false},
		{"unknown", CostUnknown, false},
		{"soft-free", CostFree, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := baseCandidate(tc.name)
			c.CostClass = tc.class
			c.HardZeroIncrementalCost = tc.hard
			_, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label()}, []Candidate{c})
			if err != ErrNoEligibleCandidate {
				t.Fatalf("got %v", err)
			}
			d, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label(), AllowPotentialMonetarySpend: true}, []Candidate{c})
			if err != nil || d.Selected == nil {
				t.Fatalf("explicit monetary opt-in failed: %v %#v", err, d.Selected)
			}
		})
	}
}

func TestRouteSubscriptionOptInDoesNotAuthorizeMoney(t *testing.T) {
	paid := baseCandidate("paid")
	paid.CostClass = CostPaid
	paid.HardZeroIncrementalCost = false
	_, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label(), AllowSubscriptionUsage: true}, []Candidate{paid})
	if err != ErrNoEligibleCandidate {
		t.Fatalf("subscription permission must not imply monetary permission: %v", err)
	}
}

func TestSubscriptionRemainsFallbackEvenWhenBetterQualified(t *testing.T) {
	free := baseCandidate("free-supported")
	free.Qualification = QualSupported
	sub := baseCandidate("subscription-verified")
	sub.CostClass = CostIncludedSubscription
	sub.HardZeroIncrementalCost = false
	sub.Qualification = QualVerified

	d, err := Route(RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label(), AllowSubscriptionUsage: true}, []Candidate{sub, free})
	if err != nil {
		t.Fatal(err)
	}
	if d.Selected == nil || d.Selected.Candidate.ID != "free-supported" {
		t.Fatalf("subscription must remain fallback while a proven zero-cost route is eligible: %#v", d.Selected)
	}
}

func TestRouteHonorsExplicitCandidateExclusion(t *testing.T) {
	first := baseCandidate("first")
	second := baseCandidate("second")
	d, err := Route(RouteRequest{
		WorkspaceID:         "ws",
		CapabilityID:        "agent.reason",
		ProtocolLevel:       "L1",
		DataLabel:           label(),
		ExcludeCandidateIDs: []string{"first"},
	}, []Candidate{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if d.Selected == nil || d.Selected.Candidate.ID != "second" {
		t.Fatalf("expected second candidate after exclusion, got %#v", d.Selected)
	}
	found := false
	for _, r := range d.Rejected {
		if r.CandidateID == "first" && r.Reason == "excluded_by_request" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected explicit exclusion in decision: %#v", d.Rejected)
	}
}

func TestRouteHonorsExplicitCandidateSelection(t *testing.T) {
	req := RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label(), IncludeCandidateIDs: []string{"chosen"}}
	candidates := []Candidate{
		baseCandidate("other"),
		baseCandidate("chosen"),
	}
	d, err := Route(req, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if d.Selected == nil || d.Selected.Candidate.ID != "chosen" {
		t.Fatalf("selected=%+v", d.Selected)
	}
	found := false
	for _, r := range d.Rejected {
		if r.CandidateID == "other" && r.Reason == "not_selected_by_request" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rejected=%+v", d.Rejected)
	}
}

func TestRouteLocalOnlyRejectsRemoteCandidate(t *testing.T) {
	local := baseCandidate("local")
	local.Local = true
	remote := baseCandidate("remote")
	remote.Local = false
	req := RouteRequest{WorkspaceID: "ws", CapabilityID: "agent.reason", ProtocolLevel: "L1", DataLabel: label(), LocalOnly: true}
	decision, err := Route(req, []Candidate{remote, local})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Selected == nil || decision.Selected.Candidate.ID != "local" {
		t.Fatalf("expected local candidate, got %#v", decision.Selected)
	}
}
