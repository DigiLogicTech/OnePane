package team

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// Reconcile observed completions without replaying inference/tools. One durable
// notice per completed assignment survives duplicate ticks and process restarts.
func (s *Service) syncAssignmentNotices(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT a.task_id FROM team_execution_assignments a JOIN tasks t ON t.id=a.task_id AND t.workspace_id=a.workspace_id AND t.state='complete' WHERE NOT EXISTS(SELECT 1 FROM team_assignment_notices n WHERE n.task_id=a.task_id) AND EXISTS(SELECT 1 FROM checkpoints c JOIN verifications v ON v.id=c.verification_id AND v.task_id=t.id AND v.workspace_id=t.workspace_id AND v.status='pass' WHERE c.task_id=t.id AND c.workspace_id=t.workspace_id AND c.status='valid' AND c.invalidated_at IS NULL) ORDER BY a.created_at,a.task_id LIMIT 64`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, id := range ids {
		err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			var session, ws, member, key, checkpoint, verification, level string
			var round int64
			err := tx.QueryRowContext(ctx, `SELECT a.session_id,a.workspace_id,a.member_id,a.assignment_key,c.id,c.verification_id,v.achieved_level,s.round_number
			 FROM team_execution_assignments a JOIN tasks t ON t.id=a.task_id AND t.workspace_id=a.workspace_id AND t.state='complete'
			 JOIN team_sessions s ON s.id=a.session_id AND s.workspace_id=a.workspace_id
			 JOIN checkpoints c ON c.task_id=t.id AND c.workspace_id=t.workspace_id AND c.status='valid' AND c.invalidated_at IS NULL
			 JOIN verifications v ON v.id=c.verification_id AND v.workspace_id=t.workspace_id AND v.task_id=t.id AND v.status='pass'
			 WHERE a.task_id=? AND NOT EXISTS(SELECT 1 FROM team_assignment_notices n WHERE n.task_id=a.task_id)
			 ORDER BY c.created_at DESC,c.id DESC LIMIT 1`, id).Scan(&session, &ws, &member, &key, &checkpoint, &verification, &level, &round)
			if err == sql.ErrNoRows {
				return nil
			}
			if err != nil {
				return err
			}
			messageID, err := s.ids.New("tmsg")
			if err != nil {
				return err
			}
			label := level
			if level == "V0" {
				label += " (declaration only)"
			}
			content, _ := json.Marshal(map[string]string{"text": fmt.Sprintf("Observed completion of assignment %s. Verification: %s.", key, label), "task_id": id, "checkpoint_id": checkpoint, "verification_id": verification, "verification_level": level})
			now := s.clock.UnixMilli()
			if _, err = tx.ExecContext(ctx, `INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,message_kind,content_json,round_number,created_at) VALUES(?,?,?,?,'system',?,?,?)`, messageID, ws, session, member, string(content), round, now); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO team_assignment_notices(task_id,checkpoint_id,message_id) VALUES(?,?,?)`, id, checkpoint, messageID); err != nil {
				return err
			}
			return s.emit(ctx, tx, ws, "team.assignment_completion_observed", "team_session", session, nil, map[string]string{"task_id": id, "message_id": messageID, "checkpoint_id": checkpoint, "verification_level": level})
		})
		if err != nil {
			return err
		}
	}
	return nil
}
