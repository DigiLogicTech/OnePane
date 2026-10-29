package projectworkspace

import (
	"context"
	"unicode/utf8"
)

// ActiveProjectWorkspaceScope observes only the current scope relationship.
// It reads no names, policy, layout or private content and grants no reusable
// authorization. Callers still check their original credential before output.
func (s *Service) ActiveProjectWorkspaceScope(ctx context.Context, tenantID, projectID, workspaceID string) (bool, error) {
	if s == nil || s.db == nil || tenantID == "" || projectID == "" {
		return false, ErrInvalidCommand
	}
	for _, value := range []string{tenantID, projectID, workspaceID} {
		if len(value) > 256 || !utf8.ValidString(value) {
			return false, ErrInvalidCommand
		}
	}
	var active bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM projects p JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
 WHERE p.id=? AND p.workspace_id=? AND p.status='active'
 AND (?='' OR EXISTS(SELECT 1 FROM project_workspaces named WHERE named.id=? AND named.project_id=p.id AND named.status='active'))
 )`, projectID, tenantID, workspaceID, workspaceID).Scan(&active)
	return active, err
}
