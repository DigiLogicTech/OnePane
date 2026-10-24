package api

import (
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"strings"
)

// All Library read paths must use the same artifact integrity/ownership
// predicate after the managed artifact has been independently SHA-256
// verified. Never let a quarantined, archived, foreign-Project or foreign-
// tenant blob masquerade as an authorised Library version.
func libraryArtifactMatchesProject(
	selected projectworkspace.LibraryVersion, raw artifact.Artifact,
	projectID, tenantWorkspaceID string,
) bool {
	selectedDigest, ok := librarySHA256Digest(selected.ContentHash)
	artifactDigest, artifactOK := librarySHA256Digest(raw.ContentHash)
	return projectID != "" && tenantWorkspaceID != "" &&
		selected.SizeBytes >= 0 && ok && artifactOK && strings.HasPrefix(raw.ContentHash, "sha256:") &&
		raw.Status == artifact.StatusActive &&
		raw.ProjectID != nil && *raw.ProjectID == projectID &&
		raw.WorkspaceID == tenantWorkspaceID &&
		artifactDigest == selectedDigest &&
		raw.SizeBytes == selected.SizeBytes
}

// Workspace publications store the 64 lowercase hexadecimal SHA-256 digits;
// managed artifacts and user imports store sha256:<digits>. Accept exactly
// these persisted forms without changing either record or accepting another
// algorithm, case folding, whitespace, truncated digests or repeated prefixes.
func librarySHA256Digest(value string) (string, bool) {
	digest := strings.TrimPrefix(value, "sha256:")
	if len(digest) != 64 {
		return "", false
	}
	for i := 0; i < len(digest); i++ {
		if (digest[i] < '0' || digest[i] > '9') && (digest[i] < 'a' || digest[i] > 'f') {
			return "", false
		}
	}
	return digest, true
}
