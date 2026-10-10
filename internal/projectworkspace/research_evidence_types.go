package projectworkspace

// ResearchEvidenceSelection is a frozen human approval of metadata only.
// It is not a read lease, a model prompt or a Research seat attachment.
type ResearchEvidenceSelection struct {
 SessionID string `json:"session_id"`
 ProjectID string `json:"project_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 ResearchManifestSHA256 string `json:"research_manifest_sha256"`
 Evidence WorkspaceEvidencePacket `json:"evidence"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
}
