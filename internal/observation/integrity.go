package observation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type integrityEnvelope struct {
	WorkspaceID       string          `json:"workspace_id"`
	SubjectRef        string          `json:"subject_ref"`
	ObservationType   string          `json:"observation_type"`
	ProbeToolID       string          `json:"probe_tool_id"`
	ProbeToolVersion  string          `json:"probe_tool_version"`
	SourcePrincipalID *string         `json:"source_principal_id,omitempty"`
	AdapterID         *string         `json:"adapter_id,omitempty"`
	AdapterVersion    *string         `json:"adapter_version,omitempty"`
	Value             json.RawMessage `json:"value"`
	Confidentiality   string          `json:"confidentiality"`
	Residency         string          `json:"residency"`
	Trust             string          `json:"trust"`
	OriginNodeID      string          `json:"origin_node_id,omitempty"`
	ObservedAt        int64           `json:"observed_at"`
}

func IntegrityHash(o Observation) (string, error) {
	envelope := integrityEnvelope{
		WorkspaceID: o.WorkspaceID, SubjectRef: o.SubjectRef, ObservationType: o.ObservationType,
		ProbeToolID: o.ProbeToolID, ProbeToolVersion: o.ProbeToolVersion,
		SourcePrincipalID: o.SourcePrincipalID, AdapterID: o.AdapterID, AdapterVersion: o.AdapterVersion,
		Value: o.Value, Confidentiality: string(o.Label.Confidentiality), Residency: string(o.Label.Residency),
		Trust: string(o.Label.Trust), OriginNodeID: o.Label.OriginNodeID, ObservedAt: o.ObservedAt,
	}
	b, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("marshal observation integrity envelope: %w", err)
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:]), nil
}
