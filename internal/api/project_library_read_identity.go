package api

import (
	"net/http"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// Revalidate the same credential and the exact current immutable version after
// slow artifact verification/opening (and after collecting a bounded preview).
// A previously resolved grant is not a transferable content-read permission.
func (s *Server) authorizeCurrentLibraryRead(w http.ResponseWriter, r *http.Request, original Identity, lib projectLibraryService, p projectworkspace.Project, workspace string, selected projectworkspace.LibraryVersion) bool {
	if !s.authorizeCurrentWorkspaceRead(w, r, original, p.WorkspaceID, "project.read") {
		return false
	}
	project, err := s.projects.Project(r.Context(), p.ID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return false
	}
	if project.ID != p.ID || project.WorkspaceID != p.WorkspaceID || project.Status != "active" {
		writeError(w, http.StatusForbidden, "Project changed during read")
		return false
	}
	current, err := lib.ResolveWorkspaceLibraryVersion(r.Context(), p.ID, workspace, selected.AssetID, selected.Version)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return false
	}
	if current != selected {
		writeError(w, http.StatusForbidden, "Library version changed during read")
		return false
	}
	return s.authorizeCurrentWorkspaceRead(w, r, original, p.WorkspaceID, "project.read")
}
