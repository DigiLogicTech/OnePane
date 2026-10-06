package policy

import (
	"context"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/system"
)

type AuthorityStore interface {
	Get(context.Context, string) (authority.Lease, error)
	ValidateSubject(context.Context, string, string) error
	TaskWorkspace(context.Context, string) (string, error)
}

type SystemStateStore interface {
	Get(context.Context) (system.State, error)
}

type WatchdogStore interface {
	Healthy(context.Context) bool
}

type Engine struct {
	authority AuthorityStore
	system    SystemStateStore
	watchdog  WatchdogStore
	clock     clock.Clock
}

func NewEngine(authorityStore AuthorityStore, systemStore SystemStateStore, clk clock.Clock) *Engine {
	return &Engine{authority: authorityStore, system: systemStore, clock: clk}
}

func (e *Engine) SetWatchdog(w WatchdogStore) {
	if e != nil {
		e.watchdog = w
	}
}

func (e *Engine) EvaluateAuthority(ctx context.Context, in AuthorityInput) Decision {
	d := Decision{
		Allowed:              false,
		LeaseID:              in.LeaseID,
		PolicyRevision:       BuiltinPolicyRevision,
		RequiredVerification: strongestVerification(normalizeVerification(in.MinimumVerification), baselineVerification(in.Action), riskVerification(in.Risk)),
		RequiredApproval:     strongestApproval(normalizeApproval(in.MinimumApproval), riskApproval(in.Risk)),
	}

	if e == nil || e.authority == nil || e.system == nil || e.clock == nil || !validAuthorityInput(in) {
		d.Reasons = append(d.Reasons, ReasonInvalidInput)
		return d
	}

	state, err := e.system.Get(ctx)
	if err != nil {
		d.Reasons = append(d.Reasons, ReasonSystemStateUnavailable)
		return d
	}
	if !systemModeAllows(state.Mode, in.Action) {
		d.Reasons = append(d.Reasons, ReasonSystemModeDenied)
		return d
	}
	if in.Action != authority.ActionObserve && in.Action != authority.ActionRead {
		if e.watchdog == nil || !e.watchdog.Healthy(ctx) {
			d.Reasons = append(d.Reasons, ReasonWatchdogUnavailable)
			return d
		}
	}

	lease, err := e.authority.Get(ctx, in.LeaseID)
	if err != nil {
		d.Reasons = append(d.Reasons, ReasonLeaseUnavailable)
		return d
	}
	d.LeaseRevision = lease.Revision

	if lease.WorkspaceID != in.WorkspaceID {
		d.Reasons = append(d.Reasons, ReasonLeaseWorkspaceMismatch)
	}
	if lease.PrincipalID != in.PrincipalID {
		d.Reasons = append(d.Reasons, ReasonLeasePrincipalMismatch)
	}
	if lease.CapabilityID != in.CapabilityID {
		d.Reasons = append(d.Reasons, ReasonLeaseCapabilityMismatch)
	}
	if lease.TaskID != nil && *lease.TaskID != in.TaskID {
		d.Reasons = append(d.Reasons, ReasonLeaseTaskMismatch)
	}

	switch lease.Status {
	case authority.StatusActive:
		// Continue with effective-time and usage checks below.
	case authority.StatusExpired:
		d.Reasons = append(d.Reasons, ReasonLeaseExpired)
	case authority.StatusExhausted:
		d.Reasons = append(d.Reasons, ReasonLeaseExhausted)
	case authority.StatusRevoked:
		d.Reasons = append(d.Reasons, ReasonLeaseRevoked)
	case authority.StatusInvalidated:
		d.Reasons = append(d.Reasons, ReasonLeaseInvalidated)
	default:
		d.Reasons = append(d.Reasons, ReasonLeaseInactive)
	}
	if e.clock.UnixMilli() >= lease.ExpiresAt {
		d.Reasons = appendReason(d.Reasons, ReasonLeaseExpired)
	}
	if lease.UsageLimit != nil && lease.UsageCount >= *lease.UsageLimit {
		d.Reasons = appendReason(d.Reasons, ReasonLeaseExhausted)
	}
	if !lease.Scope.AllowsAction(in.Action) {
		d.Reasons = append(d.Reasons, ReasonActionOutOfScope)
	}
	if !lease.Scope.AllowsResource(in.ResourceRef) {
		d.Reasons = append(d.Reasons, ReasonResourceOutOfScope)
	}

	if err := e.authority.ValidateSubject(ctx, in.WorkspaceID, in.PrincipalID); err != nil {
		d.Reasons = append(d.Reasons, ReasonSubjectIneligible)
	}
	if in.TaskID != "" {
		workspaceID, err := e.authority.TaskWorkspace(ctx, in.TaskID)
		if err != nil || workspaceID != in.WorkspaceID {
			d.Reasons = append(d.Reasons, ReasonTaskWorkspaceMismatch)
		}
	}

	if len(d.Reasons) == 0 {
		d.Allowed = true
	}
	return d
}

func validAuthorityInput(in AuthorityInput) bool {
	return strings.TrimSpace(in.LeaseID) != "" && strings.TrimSpace(in.WorkspaceID) != "" &&
		strings.TrimSpace(in.PrincipalID) != "" && strings.TrimSpace(in.CapabilityID) != "" &&
		strings.TrimSpace(in.ResourceRef) != "" && authority.ValidActionMode(in.Action) &&
		validRisk(in.Risk) && (in.MinimumVerification == "" || validVerification(in.MinimumVerification)) &&
		(in.MinimumApproval == "" || validApproval(in.MinimumApproval))
}

func systemModeAllows(mode system.Mode, action authority.ActionMode) bool {
	switch mode {
	case system.ModeNormal:
		return true
	case system.ModeSafe, system.ModeReadOnly:
		return action == authority.ActionObserve || action == authority.ActionRead
	case system.ModeCommissioning, system.ModeRecovery, system.ModeDegraded:
		return action == authority.ActionObserve || action == authority.ActionRead || action == authority.ActionExecuteSandboxed
	case system.ModeMaintenance:
		return action != authority.ActionExternalSend
	case system.ModeUninitialized, system.ModeBootstrap:
		return false
	default:
		return false
	}
}

func baselineVerification(action authority.ActionMode) VerificationLevel {
	switch action {
	case authority.ActionObserve, authority.ActionRead, authority.ActionExecuteSandboxed:
		return VerificationV1
	case authority.ActionMutate, authority.ActionExternalSend:
		return VerificationV2
	default:
		return VerificationV5
	}
}

func riskVerification(risk RiskLevel) VerificationLevel {
	switch risk {
	case RiskLow:
		return VerificationV1
	case RiskMedium:
		return VerificationV2
	case RiskHigh:
		return VerificationV3
	case RiskCritical:
		return VerificationV5
	default:
		return VerificationV5
	}
}

func riskApproval(risk RiskLevel) ApprovalLevel {
	switch risk {
	case RiskLow, RiskMedium:
		return ApprovalNone
	case RiskHigh:
		return ApprovalApprover
	case RiskCritical:
		return ApprovalAdmin
	default:
		return ApprovalAdmin
	}
}

func strongestVerification(levels ...VerificationLevel) VerificationLevel {
	best := VerificationV0
	for _, level := range levels {
		if verificationRank(level) > verificationRank(best) {
			best = level
		}
	}
	return best
}

func strongestApproval(levels ...ApprovalLevel) ApprovalLevel {
	best := ApprovalNone
	for _, level := range levels {
		if approvalRank(level) > approvalRank(best) {
			best = level
		}
	}
	return best
}

func verificationRank(v VerificationLevel) int {
	switch v {
	case VerificationV0:
		return 0
	case VerificationV1:
		return 1
	case VerificationV2:
		return 2
	case VerificationV3:
		return 3
	case VerificationV4:
		return 4
	case VerificationV5:
		return 5
	default:
		return 99
	}
}

func approvalRank(a ApprovalLevel) int {
	switch a {
	case ApprovalNone:
		return 0
	case ApprovalApprover:
		return 1
	case ApprovalAdmin:
		return 2
	default:
		return 99
	}
}

func validRisk(r RiskLevel) bool {
	return r == RiskLow || r == RiskMedium || r == RiskHigh || r == RiskCritical
}

func validVerification(v VerificationLevel) bool {
	return v == VerificationV0 || v == VerificationV1 || v == VerificationV2 || v == VerificationV3 || v == VerificationV4 || v == VerificationV5
}

func validApproval(a ApprovalLevel) bool {
	return a == ApprovalNone || a == ApprovalApprover || a == ApprovalAdmin
}

func appendReason(reasons []Reason, reason Reason) []Reason {
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}
func normalizeVerification(v VerificationLevel) VerificationLevel {
	if v == "" {
		return VerificationV0
	}
	return v
}

func normalizeApproval(a ApprovalLevel) ApprovalLevel {
	if a == "" {
		return ApprovalNone
	}
	return a
}
