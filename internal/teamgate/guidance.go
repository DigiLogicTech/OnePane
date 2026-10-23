package teamgate

import (
	"context"
	"encoding/json"
)

type HumanGuidance struct {
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
}

// Only current human members' bounded guidance reaches the next reasoning
// step. Agent messages/checkpoint results cannot masquerade as instructions.
// Call Require first: this projection does not authorize Task execution.
func Guidance(ctx context.Context, q Queryer, workspaceID, taskID string) ([]HumanGuidance, error) {
	var installed int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='team_execution_assignments'`).Scan(&installed); err != nil {
		return nil, err
	}
	if installed == 0 {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT m.id,m.content_json FROM task_execution_profiles ep
	 JOIN team_sessions s ON s.id=ep.team_session_id AND s.workspace_id=ep.workspace_id
	 JOIN team_messages m ON m.session_id=s.id AND m.workspace_id=s.workspace_id AND m.message_kind='human'
	 JOIN principals p ON p.id=m.author_principal_id AND p.status='active' AND p.principal_type='human'
	 JOIN workspace_memberships wm ON wm.principal_id=p.id AND wm.workspace_id=s.workspace_id AND wm.status='active'
	 LEFT JOIN team_execution_assignments a ON a.task_id=ep.task_id AND a.session_id=s.id AND a.plan_id=s.accepted_plan_id
	 WHERE ep.task_id=? AND ep.workspace_id=? AND ep.execution_mode='team'
	 AND s.status IN ('plan_accepted','executing')
	 AND (COALESCE(json_extract(m.content_json,'$.target_member_id'),'')='' OR json_extract(m.content_json,'$.target_member_id')=a.member_id)
	 ORDER BY m.message_sequence DESC LIMIT 8`, taskID, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HumanGuidance{}
	used := 0
	for rows.Next() {
		var value HumanGuidance
		var raw string
		if err := rows.Scan(&value.MessageID, &raw); err != nil {
			return nil, err
		}
		var content struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(raw), &content) != nil || content.Text == "" || len(content.Text) > 8000 {
			continue
		}
		if used+len(content.Text) > 16<<10 {
			continue
		}
		used += len(content.Text)
		value.Text = content.Text
		out = append(out, value)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
