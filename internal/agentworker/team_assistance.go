package agentworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

const teamAssistanceBindingSQL = `SELECT a.session_id,a.plan_id,a.assignment_key,a.member_id,s.round_number
 FROM team_execution_assignments a
 JOIN team_sessions s ON s.id=a.session_id AND s.workspace_id=a.workspace_id AND s.accepted_plan_id=a.plan_id AND s.status IN ('plan_accepted','executing')
 JOIN team_members m ON m.id=a.member_id AND m.team_id=s.team_id AND m.workspace_id=a.workspace_id AND m.status='active' AND m.member_kind!='human'
 JOIN task_execution_profiles ep ON ep.task_id=a.task_id AND ep.workspace_id=a.workspace_id AND ep.team_session_id=a.session_id AND ep.execution_mode='team'
 JOIN agent_worker_steps st ON st.id=? AND st.run_id=? AND st.step_kind='human' AND st.status='waiting' AND st.proposal_type='human'
 WHERE a.task_id=? AND a.workspace_id=?`

// materializeTeamAssistance runs only inside the already-authorized human wait
// transaction. Assignment identity comes from immutable storage, never from the
// model response. The caller commits this record, notice, journal and Task /
// Attempt / Worker state together. A notice requests input; it grants no power.
func (s *Service) materializeTeamAssistance(ctx context.Context, tx storage.Tx, run Run, stepID string, continuation json.RawMessage) error {
	var installed int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='team_execution_assignments')`).Scan(&installed); err != nil {
		return err
	}
	if installed == 0 {
		return nil
	}
	var session, plan, key, member string
	var round int64
	err := tx.QueryRowContext(ctx, teamAssistanceBindingSQL, stepID, run.ID, run.TaskID, run.WorkspaceID).Scan(&session, &plan, &key, &member, &round)
	if errors.Is(err, sql.ErrNoRows) {
		// Require has already rejected marked/orphaned/changed Team bindings. An
		// ordinary Task has no accepted assignment and needs no Team help record.
		var associated int
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_execution_assignments WHERE task_id=?)`, run.TaskID).Scan(&associated); err != nil {
			return err
		}
		if associated != 0 {
			return ErrInvalidWorkerState
		}
		return nil
	}
	if err != nil {
		return err
	}
	var input struct {
		Question string `json:"human_input_required"`
	}
	if json.Unmarshal(continuation, &input) != nil || len(input.Question) == 0 || len(input.Question) > 512 || !utf8.ValidString(input.Question) || strings.TrimSpace(input.Question) == "" {
		return ErrInvalidWorkerState
	}
	requestID, err := s.ids.New("thelp")
	if err != nil {
		return err
	}
	messageID, err := s.ids.New("tmsg")
	if err != nil {
		return err
	}
	eventID, err := s.ids.New("evt")
	if err != nil {
		return err
	}
	now := s.clock.UnixMilli()
	content, err := json.Marshal(map[string]string{"text": "Assignment " + key + " needs human input.", "assistance_request_id": requestID, "task_id": run.TaskID, "target_member_id": member})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) VALUES(?,?,?,?,?,'system',?,?,?)`, messageID, run.WorkspaceID, session, member, WorkerPrincipal, string(content), round, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO team_assistance_requests(id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, requestID, run.WorkspaceID, session, plan, key, member, run.TaskID, run.AttemptID, run.ID, stepID, messageID, WorkerPrincipal, input.Question, now); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"request_id": requestID, "session_id": session, "plan_id": plan, "assignment_key": key, "task_id": run.TaskID, "attempt_id": run.AttemptID, "run_id": run.ID, "wait_step_id": stepID, "message_id": messageID})
	if err != nil {
		return err
	}
	actor := WorkerPrincipal
	return s.events.Append(ctx, tx, event.Event{ID: eventID, WorkspaceID: &run.WorkspaceID, Type: "team.assistance_requested", AggregateType: "team_session", AggregateID: session, ActorPrincipalID: &actor, Payload: payload, OccurredAt: now})
}
