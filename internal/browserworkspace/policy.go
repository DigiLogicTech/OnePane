package browserworkspace

import (
	"fmt"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"strings"
)

// BrowserWorkspace is an interactive user surface, not an autonomous inference
// provider. Credentials/cookies remain inside the browser profile boundary.
type Policy struct {
	AutomationMode       string `json:"automation_mode"`
	CredentialBoundary   string `json:"credential_boundary"`
	CredentialExtraction string `json:"credential_extraction"`
	ImportMode           string `json:"import_mode"`
	SubscriptionUse      string `json:"subscription_use"`
}

func DefaultPolicy() Policy {
	return Policy{
		AutomationMode:       "human_only",
		CredentialBoundary:   "browser_profile_only",
		CredentialExtraction: "forbidden",
		ImportMode:           "explicit_user_import",
		SubscriptionUse:      "user_initiated_only",
	}
}
func (Policy) Schedulable() bool { return false }

type ImportKind string

const (
	ModelOutput ImportKind = "model_output"
	WebContent  ImportKind = "web_content"
)

func ImportLabel(workspaceID string, kind ImportKind) (policy.DataLabel, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return policy.DataLabel{}, fmt.Errorf("workspace required")
	}
	trust := policy.TrustUntrustedContent
	if kind == ModelOutput {
		trust = policy.TrustUnverifiedDerived
	} else if kind != WebContent {
		return policy.DataLabel{}, fmt.Errorf("unknown browser import kind")
	}
	return policy.DataLabel{WorkspaceID: workspaceID, Confidentiality: policy.ConfidentialityPublic, Residency: policy.ResidencyAny, Trust: trust}, nil
}
