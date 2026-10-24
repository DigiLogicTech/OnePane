// Package assurancecriteria defines the data contract shared by planning and verification.
package assurancecriteria

import "encoding/json"

type JSONCheck struct {
	Pointer  string          `json:"pointer"`
	Operator string          `json:"operator"` // exists, not_empty, equals, type
	Value    json.RawMessage `json:"value,omitempty"`
	Type     string          `json:"type,omitempty"`
}

type ArtifactRequirement struct {
	MediaType   string `json:"media_type,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

type ObservationRequirement struct {
	Role            string      `json:"role"` // direct | integration
	SubjectRef      string      `json:"subject_ref,omitempty"`
	ObservationType string      `json:"observation_type,omitempty"`
	ProbeToolID     string      `json:"probe_tool_id,omitempty"`
	AdapterID       string      `json:"adapter_id,omitempty"`
	ValueChecks     []JSONCheck `json:"value_checks,omitempty"`
}

type Criteria struct {
	ResultChecks []JSONCheck              `json:"result_checks,omitempty"`
	Artifacts    []ArtifactRequirement    `json:"artifacts,omitempty"`
	Observations []ObservationRequirement `json:"observations,omitempty"`
}
