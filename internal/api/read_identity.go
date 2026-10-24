package api

import "net/http"

// authorizeCurrentWorkspaceRead checks credential lifetime and current scopes
// after storage collection, before publishing a private read response.
func (s *Server) authorizeCurrentWorkspaceRead(w http.ResponseWriter, r *http.Request, original Identity, workspace string, capabilities ...string) bool {
	fresh, ok := s.authenticate(w, r)
	if !ok {
		return false
	}
	if fresh.PrincipalID != original.PrincipalID || fresh.CredentialID != original.CredentialID || fresh.PrincipalType != original.PrincipalType {
		writeError(w, http.StatusForbidden, "Identity changed during read")
		return false
	}
	for _, capability := range capabilities {
		if !s.authorize(w, r, fresh, workspace, capability) {
			return false
		}
	}
	return true
}
