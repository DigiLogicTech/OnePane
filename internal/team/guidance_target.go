package team

import (
	"context"
	"encoding/json"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// A recipient addresses reasoning context, not visibility or execution authority.
// Validate against the frozen session roster inside the message transaction.
func validateGuidanceTarget(ctx context.Context, tx storage.Tx, ss Session, kind string, raw json.RawMessage) error {
	var content map[string]json.RawMessage
	if json.Unmarshal(raw, &content) != nil {
		return nil
	}
	value, exists := content["target_member_id"]
	if !exists {
		return nil
	}
	var target string
	if string(value) == "null" || json.Unmarshal(value, &target) != nil || len(target) > 128 {
		return ErrInvalid
	}
	if target == "" {
		return nil
	}
	if kind != "human" {
		return ErrInvalid
	}
	var mode, snapshotRaw string
	if err := tx.QueryRowContext(ctx, `SELECT execution_mode,snapshot_json FROM team_session_manifests WHERE session_id=? AND workspace_id=? AND team_id=?`, ss.ID, ss.WorkspaceID, ss.TeamID).Scan(&mode, &snapshotRaw); err != nil {
		return ErrInvalid
	}
	var snapshot SessionSnapshot
	if mode != "team" || json.Unmarshal([]byte(snapshotRaw), &snapshot) != nil || snapshot.SessionID != ss.ID || snapshot.TaskID != ss.TaskID || snapshot.TeamID != ss.TeamID {
		return ErrInvalid
	}
	for _, member := range snapshot.Members {
		if member.ID == target && member.MemberKind == "agent" && member.Status == "active" {
			return nil
		}
	}
	return ErrInvalid
}
