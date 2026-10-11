package api

import (
 "github.com/DigiLogicTech/OnePane/internal/artifact"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// All Library read paths must use the same artifact integrity/ownership
// predicate after the managed artifact has been independently SHA-256
// verified. Never let a quarantined, archived, foreign-Project or foreign-
// tenant blob masquerade as an authorised Library version.
func libraryArtifactMatchesProject(
 selected projectworkspace.LibraryVersion,raw artifact.Artifact,
 projectID,tenantWorkspaceID string,
)bool{
 return projectID!=""&&tenantWorkspaceID!=""&&
  selected.SizeBytes>=0&&selected.ContentHash!=""&&
  raw.Status==artifact.StatusActive&&
  raw.ProjectID!=nil&&*raw.ProjectID==projectID&&
  raw.WorkspaceID==tenantWorkspaceID&&
  raw.ContentHash==selected.ContentHash&&
  raw.SizeBytes==selected.SizeBytes
}
