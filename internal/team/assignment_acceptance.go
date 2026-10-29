package team

import (
	"context"
	"encoding/json"

	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

func (s *Service) acceptPlan(ctx context.Context, c AcceptPlanCommand) (Session, error) {
	decision, err := canonical(c.Decision)
	if err != nil {
		return Session{}, err
	}
	ss, err := s.Session(ctx, c.SessionID)
	if err != nil {
		return Session{}, err
	}
	now := s.clock.UnixMilli()
	did, _ := s.ids.New("tdecision")
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		// Read human membership, current candidate, objections and session in
		// the same write transaction that creates all execution records.
		var authorized int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM principals p JOIN workspace_memberships m ON m.principal_id=p.id AND m.workspace_id=? AND m.status='active' JOIN workspaces w ON w.id=m.workspace_id AND w.status='active' WHERE p.id=? AND p.principal_type='human' AND p.status='active'`, ss.WorkspaceID, c.AcceptedByPrincipalID).Scan(&authorized); err != nil {
			return err
		}
		if authorized != 1 {
			return ErrHumanRequired
		}
		var existingAssignments int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_assignment_roots WHERE session_id=?`, ss.ID).Scan(&existingAssignments); err != nil {
			return err
		}
		if existingAssignments != 0 {
			return ErrSessionState
		}
		var status, planStatus, body string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM team_sessions WHERE id=? AND workspace_id=? AND task_id=? AND team_id=?`, ss.ID, ss.WorkspaceID, ss.TaskID, ss.TeamID).Scan(&status); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT status,plan_json FROM team_plans WHERE id=? AND session_id=? AND workspace_id=?`, c.PlanID, ss.ID, ss.WorkspaceID).Scan(&planStatus, &body); err != nil {
			return ErrInvalid
		}
		if planStatus != "candidate" {
			return ErrInvalid
		}
		switch status {
		case "deliberating", "plan_proposed", "paused_for_deliberation", "team_review":
		default:
			return ErrSessionState
		}
		var blocking int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_objections WHERE session_id=? AND (plan_id IS NULL OR plan_id=?) AND status='open' AND severity IN ('blocking','critical')`, ss.ID, c.PlanID).Scan(&blocking); err != nil {
			return err
		}
		if blocking > 0 && !c.AcceptOpenRisk {
			return ErrOpenBlockingObjection
		}
		plan := Plan{ID: c.PlanID, SessionID: ss.ID, WorkspaceID: ss.WorkspaceID, Body: json.RawMessage(body)}
		if err := s.createAssignmentsTx(ctx, tx, ss, plan, c.AcceptedByPrincipalID, now); err != nil {
			return err
		}
		if blocking > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE team_objections SET status='accepted_risk',resolution_json=?,resolved_by_principal_id=?,resolved_at=? WHERE session_id=? AND (plan_id IS NULL OR plan_id=?) AND status='open' AND severity IN ('blocking','critical')`, string(decision), c.AcceptedByPrincipalID, now, ss.ID, c.PlanID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE team_plans SET status='accepted',accepted_by_principal_id=?,accepted_at=? WHERE id=? AND status='candidate'`, c.AcceptedByPrincipalID, now, c.PlanID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE team_plans SET status='superseded' WHERE session_id=? AND id<>? AND status='candidate'`, ss.ID, c.PlanID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE team_sessions SET status='plan_accepted',accepted_plan_id=?,revision=revision+1,updated_at=? WHERE id=?`, c.PlanID, now, ss.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO team_decisions(id,workspace_id,session_id,decision_type,decision_json,decided_by_principal_id,created_at) VALUES(?,?,?,'plan_acceptance',?,?,?)`, did, ss.WorkspaceID, ss.ID, string(decision), c.AcceptedByPrincipalID, now); err != nil {
			return err
		}
		return s.emit(ctx, tx, ss.WorkspaceID, "team.plan_accepted", "team_session", ss.ID, &c.AcceptedByPrincipalID, map[string]any{"plan_id": c.PlanID, "accepted_open_risk": blocking > 0})
	})
	if err != nil {
		return Session{}, err
	}
	tr, err := s.tasks.Get(ctx, ss.TaskID)
	if err != nil {
		return Session{}, err
	}
	if tr.State == task.StateCreated {
		_, err = s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: tr.ID, ExpectedRevision: tr.Revision, ActorPrincipalID: &c.AcceptedByPrincipalID, Reason: "team plan accepted"})
	} else if tr.State == task.StatePaused {
		_, err = s.tasks.Resume(ctx, task.TransitionCommand{TaskID: tr.ID, ExpectedRevision: tr.Revision, ActorPrincipalID: &c.AcceptedByPrincipalID, Reason: "revised team plan accepted"})
	}
	if err != nil {
		return Session{}, err
	}
	return s.Session(ctx, ss.ID)
}
