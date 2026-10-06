package policy

import "strings"

func EvaluateInformationFlow(in InformationFlowInput) InformationFlowDecision {
	d := InformationFlowDecision{
		Allowed:        false,
		EffectiveLabel: in.Source,
		PolicyRevision: BuiltinPolicyRevision,
	}
	if !validLabel(in.Source) || !validDestination(in.Destination) {
		d.Reasons = append(d.Reasons, ReasonInvalidInput)
		return d
	}
	if in.Source.WorkspaceID != in.Destination.WorkspaceID {
		d.Reasons = append(d.Reasons, ReasonCrossWorkspace)
	}
	if confidentialityRank(in.Source.Confidentiality) > confidentialityRank(in.Destination.Clearance) {
		d.Reasons = append(d.Reasons, ReasonConfidentiality)
	}
	if in.Destination.Kind == DestinationUntrusted && in.Source.Confidentiality != ConfidentialityPublic {
		d.Reasons = appendReason(d.Reasons, ReasonConfidentiality)
	}
	if !residencyAllows(in.Source, in.Destination) {
		d.Reasons = append(d.Reasons, ReasonResidency)
	}

	if in.ProposedDerivedLabel != nil {
		proposed := *in.ProposedDerivedLabel
		d.EffectiveLabel = proposed
		if !validLabel(proposed) {
			d.Reasons = appendReason(d.Reasons, ReasonInvalidInput)
		} else {
			if proposed.WorkspaceID != in.Source.WorkspaceID {
				d.Reasons = appendReason(d.Reasons, ReasonCrossWorkspace)
			}
			if confidentialityRank(proposed.Confidentiality) < confidentialityRank(in.Source.Confidentiality) ||
				residencyRank(proposed.Residency) < residencyRank(in.Source.Residency) {
				d.Reasons = appendReason(d.Reasons, ReasonDeclassification)
			}
			if in.Source.Residency == ResidencyOriginNode && proposed.OriginNodeID != in.Source.OriginNodeID {
				d.Reasons = appendReason(d.Reasons, ReasonDeclassification)
			}
			if !trustDerivationAllowed(in.Source.Trust, proposed.Trust, in.VerificationEstablished) {
				d.Reasons = appendReason(d.Reasons, ReasonTrustElevation)
			}
		}
	}

	if len(d.Reasons) == 0 {
		d.Allowed = true
	}
	return d
}

func InheritLabels(labels ...DataLabel) (DataLabel, bool) {
	if len(labels) == 0 {
		return DataLabel{}, false
	}
	out := labels[0]
	if !validLabel(out) {
		return DataLabel{}, false
	}
	out.Trust = TrustUnverifiedDerived
	for _, label := range labels[1:] {
		if !validLabel(label) || label.WorkspaceID != out.WorkspaceID {
			return DataLabel{}, false
		}
		if confidentialityRank(label.Confidentiality) > confidentialityRank(out.Confidentiality) {
			out.Confidentiality = label.Confidentiality
		}
		if residencyRank(label.Residency) > residencyRank(out.Residency) {
			out.Residency = label.Residency
			out.OriginNodeID = label.OriginNodeID
		} else if label.Residency == ResidencyOriginNode && out.Residency == ResidencyOriginNode && label.OriginNodeID != out.OriginNodeID {
			return DataLabel{}, false
		}
	}
	if out.Residency != ResidencyOriginNode {
		out.OriginNodeID = labels[0].OriginNodeID
	}
	return out, true
}

func residencyAllows(source DataLabel, dest FlowDestination) bool {
	switch source.Residency {
	case ResidencyAny:
		return true
	case ResidencyTrustedNodes:
		return dest.Kind == DestinationOriginNode || dest.Kind == DestinationTrustedNode
	case ResidencyOriginNode:
		return dest.Kind == DestinationOriginNode && source.OriginNodeID != "" && dest.NodeID == source.OriginNodeID
	default:
		return false
	}
}

func trustDerivationAllowed(source, proposed TrustClass, verified bool) bool {
	if proposed == source || proposed == TrustUnverifiedDerived {
		return true
	}
	return proposed == TrustVerifiedDerived && verified
}

func validLabel(label DataLabel) bool {
	if strings.TrimSpace(label.WorkspaceID) == "" || !validConfidentiality(label.Confidentiality) ||
		!validResidency(label.Residency) || !validTrust(label.Trust) {
		return false
	}
	return label.Residency != ResidencyOriginNode || strings.TrimSpace(label.OriginNodeID) != ""
}

func validDestination(dest FlowDestination) bool {
	if strings.TrimSpace(dest.WorkspaceID) == "" || !validConfidentiality(dest.Clearance) {
		return false
	}
	switch dest.Kind {
	case DestinationOriginNode, DestinationTrustedNode:
		return strings.TrimSpace(dest.NodeID) != ""
	case DestinationCloud, DestinationUntrusted:
		return true
	default:
		return false
	}
}

func validConfidentiality(c Confidentiality) bool {
	return c == ConfidentialityPublic || c == ConfidentialityInternal || c == ConfidentialityConfidential || c == ConfidentialitySecret
}

func confidentialityRank(c Confidentiality) int {
	switch c {
	case ConfidentialityPublic:
		return 0
	case ConfidentialityInternal:
		return 1
	case ConfidentialityConfidential:
		return 2
	case ConfidentialitySecret:
		return 3
	default:
		return 99
	}
}

func validResidency(r Residency) bool {
	return r == ResidencyAny || r == ResidencyTrustedNodes || r == ResidencyOriginNode
}

func residencyRank(r Residency) int {
	switch r {
	case ResidencyAny:
		return 0
	case ResidencyTrustedNodes:
		return 1
	case ResidencyOriginNode:
		return 2
	default:
		return 99
	}
}

func validTrust(t TrustClass) bool {
	switch t {
	case TrustTrustedControl, TrustTrustedProcedure, TrustAuthoritativeData, TrustUserInstruction,
		TrustUntrustedContent, TrustUnverifiedDerived, TrustVerifiedDerived:
		return true
	default:
		return false
	}
}
