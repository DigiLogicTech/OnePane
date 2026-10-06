package scheduler

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type CandidateKind string

const (
	CandidateModel        CandidateKind = "model_deployment"
	CandidateAgentRuntime CandidateKind = "agent_runtime"
)

type CostClass string

const (
	CostLocal                CostClass = "local"
	CostFree                 CostClass = "free"
	CostIncludedSubscription CostClass = "included_subscription"
	CostProviderManaged      CostClass = "provider_managed"
	CostPaid                 CostClass = "paid"
	CostUnknown              CostClass = "unknown"
)

type Qualification string

const (
	QualUntested     Qualification = "untested"
	QualIncompatible Qualification = "incompatible"
	QualLimited      Qualification = "limited"
	QualMediated     Qualification = "mediated"
	QualSupported    Qualification = "supported"
	QualVerified     Qualification = "verified"
)

type Candidate struct {
	ID                      string
	Kind                    CandidateKind
	DisplayName             string
	Provider                string
	Status                  string
	TrustState              string
	WorkspaceID             *string
	Local                   bool
	CostClass               CostClass
	HardZeroIncrementalCost bool
	Qualification           Qualification
	ProtocolLevel           string
	RoleNames               []string
	CapabilityIDs           []string
	ContextMax              int64
	Destination             policy.FlowDestination
	AllowedResidency        []string
	Schedulable             bool
	ToolCallback            bool
	Metadata                map[string]any
	NodeID                  string
	ComputeMode             string // cpu | gpu | hybrid | cloud | remote
	RuntimeBackend          string
	Resident                bool
	Cached                  bool
	NodeStatus              string
	NodeLoadPct             float64
}

type RouteRequest struct {
	WorkspaceID                string
	CapabilityID               string
	RoleName                   string
	ProtocolLevel              string
	ContextTokens              int64
	DataLabel                  policy.DataLabel
	AllowUntested              bool
	AllowLimited               bool
	AllowMediated              bool
	AllowDegraded              bool
	RequireZeroIncrementalCost bool
	PreferZeroIncrementalCost  bool
	// AllowSubscriptionUsage is deliberately false by default. Included plan
	// allowance is a bounded user resource (for example ChatGPT Work/Codex
	// usage), not equivalent to genuinely free/local compute.
	AllowSubscriptionUsage bool
	// AllowPotentialMonetarySpend is false by default. Paid, unknown,
	// provider-managed, and non-hard-stop "free" routes are excluded until a
	// caller explicitly authorizes possible spend. Future BudgetReservation
	// support will make this more granular.
	AllowPotentialMonetarySpend bool
	AllowedKinds                []CandidateKind
	// LocalOnly excludes cloud/provider and remote-node candidates when workspace policy disallows remote reasoning.
	LocalOnly bool
	// IncludeCandidateIDs constrains routing to an explicit operator/project
	// selection. An empty list preserves normal scheduler choice.
	IncludeCandidateIDs []string
	// ExcludeCandidateIDs lets bounded escalation/retry logic avoid immediately
	// selecting a deployment/runtime that already failed or explicitly asked to
	// escalate. Exclusion is deterministic and recorded in the route request.
	ExcludeCandidateIDs []string
	// ComputePreference lets a workspace prefer or constrain CPU/GPU placement
	// without changing model identity. Empty is equivalent to auto.
	ComputePreference string // auto | prefer_gpu | prefer_cpu | gpu_only | cpu_only
}

type Rejection struct {
	CandidateID string `json:"candidate_id"`
	Reason      string `json:"reason"`
}
type RankedCandidate struct {
	Candidate Candidate `json:"candidate"`
	Score     int       `json:"score"`
}
type Decision struct {
	Selected *RankedCandidate  `json:"selected,omitempty"`
	Ranked   []RankedCandidate `json:"ranked"`
	Rejected []Rejection       `json:"rejected"`
}

var ErrNoEligibleCandidate = errors.New("no eligible scheduler candidate")

func ValidateCostClass(v CostClass) bool {
	switch v {
	case CostLocal, CostFree, CostIncludedSubscription, CostProviderManaged, CostPaid, CostUnknown:
		return true
	}
	return false
}
func ValidateQualification(v Qualification) bool {
	switch v {
	case QualUntested, QualIncompatible, QualLimited, QualMediated, QualSupported, QualVerified:
		return true
	}
	return false
}
func zeroIncremental(c Candidate) bool {
	switch c.CostClass {
	case CostLocal:
		return true
	case CostFree:
		return c.HardZeroIncrementalCost
	default:
		// Included subscription allowance has no per-request API charge, but it
		// consumes a finite user entitlement and is therefore not treated as
		// zero-cost capacity by the autonomous scheduler.
		return false
	}
}
func protocolRank(v string) int {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "L0":
		return 0
	case "L1":
		return 1
	case "L2":
		return 2
	case "L3":
		return 3
	default:
		return -1
	}
}
func qualRank(v Qualification) int {
	switch v {
	case QualVerified:
		return 5
	case QualSupported:
		return 4
	case QualMediated:
		return 3
	case QualLimited:
		return 2
	case QualUntested:
		return 1
	default:
		return 0
	}
}
func resourceTier(c Candidate) int {
	// Lower is preferred. Subscription allowance is a fallback resource even
	// after opt-in; it must not outrank an otherwise eligible local/proven-$0
	// route merely because the subscription model has a stronger qualification.
	if zeroIncremental(c) {
		return 0
	}
	if c.CostClass == CostIncludedSubscription {
		return 1
	}
	if c.CostClass == CostProviderManaged || (c.CostClass == CostFree && !c.HardZeroIncrementalCost) {
		return 2
	}
	return 3
}

func costScore(v CostClass, hard bool) int {
	switch v {
	case CostLocal:
		return 320
	case CostFree:
		if hard {
			return 310
		}
		return 120
	case CostIncludedSubscription:
		// Only considered after explicit opt-in; rank below proven free/local
		// capacity so subscription allowance is preserved whenever possible.
		return 160
	case CostProviderManaged:
		return 100
	case CostUnknown:
		return 40
	case CostPaid:
		return 0
	}
	return 0
}
func containsString(xs []string, v string) bool {
	if strings.TrimSpace(v) == "" {
		return true
	}
	for _, x := range xs {
		if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}
func kindAllowed(allowed []CandidateKind, k CandidateKind) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, v := range allowed {
		if v == k {
			return true
		}
	}
	return false
}
func residencyListed(xs []string, r policy.Residency) bool {
	if len(xs) == 0 {
		return false
	}
	s := strings.ToLower(string(r))
	for _, v := range xs {
		if strings.ToLower(v) == s {
			return true
		}
	}
	return false
}

func Route(req RouteRequest, candidates []Candidate) (Decision, error) {
	var out Decision
	if strings.TrimSpace(req.WorkspaceID) == "" || strings.TrimSpace(req.CapabilityID) == "" || protocolRank(req.ProtocolLevel) < 0 {
		return out, fmt.Errorf("invalid route request")
	}
	if err := policy.ValidateDataLabel(req.DataLabel); err != nil || req.DataLabel.WorkspaceID != req.WorkspaceID {
		return out, fmt.Errorf("invalid route data label")
	}
	excluded := map[string]struct{}{}
	for _, id := range req.ExcludeCandidateIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			excluded[id] = struct{}{}
		}
	}
	included := map[string]struct{}{}
	for _, id := range req.IncludeCandidateIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			included[id] = struct{}{}
		}
	}
	for _, c := range candidates {
		reject := func(reason string) { out.Rejected = append(out.Rejected, Rejection{CandidateID: c.ID, Reason: reason}) }
		if len(included) > 0 {
			if _, ok := included[c.ID]; !ok {
				reject("not_selected_by_request")
				continue
			}
		}
		if _, ok := excluded[c.ID]; ok {
			reject("excluded_by_request")
			continue
		}
		if req.LocalOnly && !c.Local {
			reject("remote candidate disallowed by workspace policy")
			continue
		}
		if strings.TrimSpace(c.ID) == "" || !kindAllowed(req.AllowedKinds, c.Kind) {
			reject("candidate kind is not allowed")
			continue
		}
		if !c.Schedulable {
			reject("candidate is not schedulable")
			continue
		}
		if c.WorkspaceID != nil && *c.WorkspaceID != req.WorkspaceID {
			reject("candidate belongs to another workspace")
			continue
		}
		if strings.EqualFold(c.Status, "degraded") && !req.AllowDegraded {
			reject("candidate is degraded")
			continue
		}
		if !ValidateCostClass(c.CostClass) {
			reject("candidate has unknown cost classification")
			continue
		}
		if c.CostClass == CostIncludedSubscription && !req.AllowSubscriptionUsage {
			reject("candidate consumes protected subscription allowance")
			continue
		}
		maySpend := c.CostClass == CostPaid || c.CostClass == CostProviderManaged || c.CostClass == CostUnknown || (c.CostClass == CostFree && !c.HardZeroIncrementalCost)
		if maySpend && !req.AllowPotentialMonetarySpend {
			reject("candidate may incur monetary cost")
			continue
		}
		if req.RequireZeroIncrementalCost && !zeroIncremental(c) {
			reject("candidate is not proven zero incremental cost")
			continue
		}
		if !containsString(c.CapabilityIDs, req.CapabilityID) && c.Qualification != QualUntested {
			reject("capability is not qualified")
			continue
		}
		if len(c.RoleNames) > 0 && !containsString(c.RoleNames, req.RoleName) {
			reject("role is not supported")
			continue
		}
		if c.ProtocolLevel != "" && protocolRank(c.ProtocolLevel) < protocolRank(req.ProtocolLevel) {
			reject("protocol level is insufficient")
			continue
		}
		if c.ContextMax > 0 && req.ContextTokens > c.ContextMax {
			reject("context exceeds candidate limit")
			continue
		}
		switch strings.ToLower(strings.TrimSpace(req.ComputePreference)) {
		case "gpu_only":
			if c.ComputeMode != "gpu" && c.ComputeMode != "hybrid" {
				reject("workspace requires GPU placement")
				continue
			}
		case "cpu_only":
			if c.ComputeMode != "cpu" {
				reject("workspace requires CPU placement")
				continue
			}
		}
		switch c.Qualification {
		case QualIncompatible:
			reject("candidate is incompatible")
			continue
		case QualUntested:
			if !req.AllowUntested {
				reject("candidate is untested")
				continue
			}
		case QualLimited:
			if !req.AllowLimited {
				reject("candidate qualification is limited")
				continue
			}
		case QualMediated:
			if !req.AllowMediated {
				reject("candidate requires mediation")
				continue
			}
		case QualSupported, QualVerified:
		default:
			reject("candidate qualification is invalid")
			continue
		}
		if !residencyListed(c.AllowedResidency, req.DataLabel.Residency) {
			reject("candidate policy does not allow source residency")
			continue
		}
		flow := policy.EvaluateInformationFlow(policy.InformationFlowInput{Source: req.DataLabel, Destination: c.Destination})
		if !flow.Allowed {
			reject("information-flow policy denied candidate")
			continue
		}
		score := qualRank(c.Qualification)*100 + costScore(c.CostClass, c.HardZeroIncrementalCost)
		if c.Local {
			score += 60
		}
		if c.Resident {
			score += 45
		} else if c.Cached {
			score += 20
		}
		if strings.EqualFold(c.NodeStatus, "busy") {
			score -= 25
		}
		if c.NodeLoadPct > 0 {
			score -= int(c.NodeLoadPct / 5)
		}
		switch strings.ToLower(strings.TrimSpace(req.ComputePreference)) {
		case "prefer_gpu":
			if c.ComputeMode == "gpu" || c.ComputeMode == "hybrid" {
				score += 35
			}
		case "prefer_cpu":
			if c.ComputeMode == "cpu" {
				score += 35
			}
		}
		if !strings.EqualFold(c.Status, "degraded") {
			score += 20
		}
		if req.PreferZeroIncrementalCost && zeroIncremental(c) {
			score += 250
		}
		if c.ContextMax > 0 && req.ContextTokens > 0 {
			margin := c.ContextMax - req.ContextTokens
			if margin > 0 {
				bonus := int(margin / 8192)
				if bonus > 40 {
					bonus = 40
				}
				score += bonus
			}
		}
		out.Ranked = append(out.Ranked, RankedCandidate{Candidate: c, Score: score})
	}
	sort.SliceStable(out.Ranked, func(i, j int) bool {
		ti, tj := resourceTier(out.Ranked[i].Candidate), resourceTier(out.Ranked[j].Candidate)
		if ti != tj {
			return ti < tj
		}
		if out.Ranked[i].Score != out.Ranked[j].Score {
			return out.Ranked[i].Score > out.Ranked[j].Score
		}
		if out.Ranked[i].Candidate.Kind != out.Ranked[j].Candidate.Kind {
			return out.Ranked[i].Candidate.Kind < out.Ranked[j].Candidate.Kind
		}
		return out.Ranked[i].Candidate.ID < out.Ranked[j].Candidate.ID
	})
	sort.Slice(out.Rejected, func(i, j int) bool {
		if out.Rejected[i].CandidateID != out.Rejected[j].CandidateID {
			return out.Rejected[i].CandidateID < out.Rejected[j].CandidateID
		}
		return out.Rejected[i].Reason < out.Rejected[j].Reason
	})
	if len(out.Ranked) == 0 {
		return out, ErrNoEligibleCandidate
	}
	sel := out.Ranked[0]
	out.Selected = &sel
	return out, nil
}
