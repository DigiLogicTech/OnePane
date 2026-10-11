package projectorchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// workspaceExecutionProfile is operator-controlled configuration, not part of
// an untrusted LLM proposal. It is deliberately not copied into model context.
func workspaceExecutionProfile(settings string) (mode, teamID string) {
	mode = "direct"
	var cfg struct {
		Orchestration struct {
			Mode string `json:"mode"`
			TeamID string `json:"team_id"`
			Team struct { TeamID string `json:"team_id"` } `json:"team"`
			Council struct { TeamID string `json:"team_id"` } `json:"council"`
		} `json:"orchestration"`
		Routing struct {
			Enabled *bool `json:"enabled"`
		} `json:"routing"`
	}
	if json.Unmarshal([]byte(settings), &cfg) != nil {
		return mode, ""
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Orchestration.Mode)) {
	case "team":
		mode = "team"
		teamID = strings.TrimSpace(cfg.Orchestration.Team.TeamID)
	case "council":
		mode = "council"
		teamID = strings.TrimSpace(cfg.Orchestration.Council.TeamID)
	default:
		return mode, ""
	}
	if cfg.Routing.Enabled != nil && !*cfg.Routing.Enabled {
		return "direct", ""
	}
	if teamID == "" {
		teamID = strings.TrimSpace(cfg.Orchestration.TeamID)
	}
	return mode, teamID
}

// resolveOrchestratorWorkspaceProfile re-reads the final canonical Workspace
// after model suggestion validation. Model output cannot set execution mode
// or borrow an earlier Workspace's Team/Council assignment.
func resolveOrchestratorWorkspaceProfile(ctx context.Context, db *sql.DB,
	projectID, operatorSelected, current string, proposed any,
) (workspaceID, mode, teamID string, err error) {
	// An explicit operator selector must itself be revalidated here. The
	// model-suggestion helper only ignores model proposals when explicitly
	// pinned; it cannot substitute for validating the operator's chosen ID.
	if requested := strings.TrimSpace(operatorSelected); requested != "" {
		workspaceID = requested
	} else {
		workspaceID = selectModelProposedWorkspace(ctx, db,
			projectID, "", current, proposed)
	}
	if workspaceID == "" {
		return "", "direct", "", nil
	}
	if db == nil {
		return "", "", "", fmt.Errorf("%w: Workspace configuration database unavailable", ErrInvalid)
	}
	var settings string
	err = db.QueryRowContext(ctx,`SELECT pw.ai_settings_json
		FROM project_workspaces pw JOIN projects p ON p.id=pw.project_id
		WHERE pw.id=? AND pw.project_id=? AND pw.status='active'
		AND p.status='active'`, workspaceID, projectID).Scan(&settings)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: final Workspace execution configuration unavailable: %v", ErrInvalid, err)
	}
	mode, teamID = workspaceExecutionProfile(settings)
	return workspaceID, mode, teamID, nil
}
