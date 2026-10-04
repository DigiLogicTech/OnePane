package policy

import "github.com/DigiLogicTech/OnePane/internal/authority"

const BuiltinPolicyRevision int64 = 1

type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

type VerificationLevel string

const (
	VerificationV0 VerificationLevel = "V0"
	VerificationV1 VerificationLevel = "V1"
	VerificationV2 VerificationLevel = "V2"
	VerificationV3 VerificationLevel = "V3"
	VerificationV4 VerificationLevel = "V4"
	VerificationV5 VerificationLevel = "V5"
)

type ApprovalLevel string

const (
	ApprovalNone     ApprovalLevel = "none"
	ApprovalApprover ApprovalLevel = "approver"
	ApprovalAdmin    ApprovalLevel = "admin"
)

type Reason string

const (
	ReasonInvalidInput            Reason = "invalid_input"
	ReasonSystemStateUnavailable  Reason = "system_state_unavailable"
	ReasonSystemModeDenied        Reason = "system_mode_denied"
	ReasonWatchdogUnavailable     Reason = "watchdog_unavailable"
	ReasonLeaseUnavailable        Reason = "lease_unavailable"
	ReasonLeaseWorkspaceMismatch  Reason = "lease_workspace_mismatch"
	ReasonLeasePrincipalMismatch  Reason = "lease_principal_mismatch"
	ReasonLeaseTaskMismatch       Reason = "lease_task_mismatch"
	ReasonLeaseCapabilityMismatch Reason = "lease_capability_mismatch"
	ReasonLeaseInactive           Reason = "lease_inactive"
	ReasonLeaseRevoked            Reason = "lease_revoked"
	ReasonLeaseInvalidated        Reason = "lease_invalidated"
	ReasonLeaseExpired            Reason = "lease_expired"
	ReasonLeaseExhausted          Reason = "lease_exhausted"
	ReasonSubjectIneligible       Reason = "subject_ineligible"
	ReasonTaskWorkspaceMismatch   Reason = "task_workspace_mismatch"
	ReasonActionOutOfScope        Reason = "action_out_of_scope"
	ReasonResourceOutOfScope      Reason = "resource_out_of_scope"
	ReasonCrossWorkspace          Reason = "cross_workspace"
	ReasonConfidentiality         Reason = "confidentiality_denied"
	ReasonResidency               Reason = "residency_denied"
	ReasonDeclassification        Reason = "declassification_denied"
	ReasonTrustElevation          Reason = "trust_elevation_denied"
)

type AuthorityInput struct {
	LeaseID             string
	WorkspaceID         string
	PrincipalID         string
	TaskID              string
	CapabilityID        string
	Action              authority.ActionMode
	ResourceRef         string
	Risk                RiskLevel
	MinimumVerification VerificationLevel
	MinimumApproval     ApprovalLevel
}

type Decision struct {
	Allowed              bool
	Reasons              []Reason
	LeaseID              string
	LeaseRevision        int64
	PolicyRevision       int64
	RequiredVerification VerificationLevel
	RequiredApproval     ApprovalLevel
}

type Confidentiality string

const (
	ConfidentialityPublic       Confidentiality = "PUBLIC"
	ConfidentialityInternal     Confidentiality = "INTERNAL"
	ConfidentialityConfidential Confidentiality = "CONFIDENTIAL"
	ConfidentialitySecret       Confidentiality = "SECRET"
)

type Residency string

const (
	ResidencyAny          Residency = "ANY"
	ResidencyTrustedNodes Residency = "TRUSTED_NODES"
	ResidencyOriginNode   Residency = "ORIGIN_NODE"
)

type TrustClass string

const (
	TrustTrustedControl    TrustClass = "TRUSTED_CONTROL"
	TrustTrustedProcedure  TrustClass = "TRUSTED_PROCEDURE"
	TrustAuthoritativeData TrustClass = "AUTHORITATIVE_DATA"
	TrustUserInstruction   TrustClass = "USER_INSTRUCTION"
	TrustUntrustedContent  TrustClass = "UNTRUSTED_CONTENT"
	TrustUnverifiedDerived TrustClass = "UNVERIFIED_DERIVED"
	TrustVerifiedDerived   TrustClass = "VERIFIED_DERIVED"
)

type DataLabel struct {
	WorkspaceID     string          `json:"workspace_id"`
	Confidentiality Confidentiality `json:"confidentiality"`
	Residency       Residency       `json:"residency"`
	Trust           TrustClass      `json:"trust"`
	OriginNodeID    string          `json:"origin_node_id,omitempty"`
}

type DestinationKind string

const (
	DestinationOriginNode  DestinationKind = "origin_node"
	DestinationTrustedNode DestinationKind = "trusted_node"
	DestinationCloud       DestinationKind = "cloud"
	DestinationUntrusted   DestinationKind = "untrusted"
)

type FlowDestination struct {
	WorkspaceID string
	Kind        DestinationKind
	NodeID      string
	Clearance   Confidentiality
}

type InformationFlowInput struct {
	Source                  DataLabel
	Destination             FlowDestination
	ProposedDerivedLabel    *DataLabel
	VerificationEstablished bool
}

type InformationFlowDecision struct {
	Allowed        bool
	Reasons        []Reason
	EffectiveLabel DataLabel
	PolicyRevision int64
}
