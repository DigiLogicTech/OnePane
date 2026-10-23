package team

import (
	"context"
	"database/sql"
	"encoding/json"
)

// LiveRoom exposes observed public coordination metadata, never profile
// instructions, model reasoning, raw provider errors or tool payloads.
type LiveRoom struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspace_id"`
	TeamID      string           `json:"team_id"`
	TaskID      string           `json:"task_id"`
	Status      string           `json:"status"`
	TaskState   string           `json:"task_state"`
	Round       int64            `json:"round"`
	ObservedAt  int64            `json:"observed_at"`
	Members     []LiveMember     `json:"members"`
	Assignments []LiveAssignment `json:"assignments"`
}
type LiveMember struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Role          string `json:"role"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	TurnID        string `json:"turn_id,omitempty"`
	ModelID       string `json:"model_id,omitempty"`
	CandidateKind string `json:"candidate_kind,omitempty"`
	Round         int64  `json:"round"`
	UpdatedAt     int64  `json:"updated_at"`
	ComputeState  string `json:"compute_state,omitempty"`
	ComputeMode   string `json:"compute_mode,omitempty"`
	Backend       string `json:"backend,omitempty"`
}

func (s *Service) LiveRoom(ctx context.Context, sessionID string) (LiveRoom, error) {
	ss, err := s.Session(ctx, sessionID)
	if err != nil {
		return LiveRoom{}, err
	}
	out := LiveRoom{ID: ss.ID, WorkspaceID: ss.WorkspaceID, TeamID: ss.TeamID, TaskID: ss.TaskID, Status: ss.Status, Round: ss.RoundNumber, ObservedAt: s.clock.UnixMilli(), Members: []LiveMember{}}
	if err = s.db.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id=? AND workspace_id=?`, ss.TaskID, ss.WorkspaceID).Scan(&out.TaskState); err != nil {
		return LiveRoom{}, err
	}
	var members []SnapshotMember
	manifest, err := s.SessionManifest(ctx, ss.ID)
	if err == nil {
		var frozen SessionSnapshot
		if json.Unmarshal(manifest.Snapshot, &frozen) != nil || frozen.SessionID != ss.ID || frozen.TeamID != ss.TeamID || frozen.TaskID != ss.TaskID {
			return LiveRoom{}, ErrInvalid
		}
		members = frozen.Members
	} else if err == sql.ErrNoRows {
		legacy, e := s.ListMembers(ctx, ss.TeamID)
		if e != nil {
			return LiveRoom{}, e
		}
		for _, m := range legacy {
			members = append(members, SnapshotMember{ID: m.ID, DisplayName: m.DisplayName, RoleName: m.RoleName, MemberKind: m.MemberKind, Status: m.Status})
		}
	} else {
		return LiveRoom{}, err
	}
	if len(members) > 8 {
		return LiveRoom{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, m := range members {
		if m.ID == "" || seen[m.ID] {
			return LiveRoom{}, ErrInvalid
		}
		seen[m.ID] = true
		v := LiveMember{ID: m.ID, Name: roomText(m.DisplayName), Role: roomText(m.RoleName), Kind: m.MemberKind, Status: "idle"}
		if m.Status != "active" {
			v.Status = "inactive"
		}
		var status string
		var model, kind sql.NullString
		err = s.db.QueryRowContext(ctx, `SELECT id,status,selected_candidate_id,selected_candidate_kind,round_number,updated_at FROM team_turn_requests WHERE session_id=? AND workspace_id=? AND member_id=? ORDER BY round_number DESC,created_at DESC,id DESC LIMIT 1`, ss.ID, ss.WorkspaceID, m.ID).Scan(&v.TurnID, &status, &model, &kind, &v.Round, &v.UpdatedAt)
		if err != nil && err != sql.ErrNoRows {
			return LiveRoom{}, err
		}
		if err == nil {
			switch status {
			case "pending", "running", "succeeded", "failed", "blocked", "paused", "cancelled":
				v.Status = status
			default:
				v.Status = "unknown"
			}
			if model.Valid {
				v.ModelID = roomText(model.String)
			}
			if kind.Valid {
				v.CandidateKind = roomText(kind.String)
			}
			// Correlate only the harness worker's exact scoped turn. Never
			// publish queue payloads, credentials, or raw failure strings.
			var state, claim string
			e := s.db.QueryRowContext(ctx, `SELECT q.state,q.resource_claim_json
			 FROM inference_requests i JOIN runtime_execution_queue q ON q.inference_request_id=i.id
			 WHERE i.workspace_id=? AND i.task_id=? AND i.principal_id='system:team-worker'
			 AND json_extract(i.context_manifest_json,'$.session_id')=?
			 AND json_extract(i.context_manifest_json,'$.turn_id')=?
			 AND json_extract(i.context_manifest_json,'$.member_id')=?
			 AND q.workspace_id=i.workspace_id AND q.task_id=i.task_id
			 ORDER BY i.created_at DESC,i.id DESC LIMIT 1`, ss.WorkspaceID, ss.TaskID, ss.ID, v.TurnID, m.ID).Scan(&state, &claim)
			if e != nil && e != sql.ErrNoRows {
				return LiveRoom{}, e
			}
			if e == nil {
				switch state {
				case "queued", "waiting_runtime", "waiting_swap", "loading", "ready", "running", "draining", "completed", "failed", "cancelled":
					v.ComputeState = state
				default:
					v.ComputeState = "unknown"
				}
				var c struct {
					Mode    string `json:"compute_mode"`
					Backend string `json:"backend"`
				}
				if json.Unmarshal([]byte(claim), &c) == nil {
					v.ComputeMode = roomText(c.Mode)
					v.Backend = roomText(c.Backend)
				}
			}
		}
		out.Members = append(out.Members, v)
	}
	out.Assignments, err = s.liveAssignments(ctx, ss)
	if err != nil {
		return LiveRoom{}, err
	}
	return out, nil
}

func roomText(v string) string {
	r := []rune(v)
	if len(r) > 256 {
		return string(r[:256]) + "…"
	}
	return v
}
