package team

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

type Service struct {
	db     *sql.DB
	tx     storage.Transactor
	clock  clock.Clock
	ids    id.Generator
	events event.Store
	tasks  *task.Service
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, tasks *task.Service) *Service {
	return &Service{db: db, tx: tx, clock: clk, ids: id.Generator{}, events: event.Store{}, tasks: tasks}
}

func canonical(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid(raw) {
		return nil, ErrInvalid
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(v)
	return b, nil
}
func (s *Service) isHuman(ctx context.Context, principalID string) bool {
	var typ, status string
	err := s.db.QueryRowContext(ctx, `SELECT principal_type,status FROM principals WHERE id=?`, principalID).Scan(&typ, &status)
	return err == nil && typ == "human" && status == "active"
}
func (s *Service) principalType(ctx context.Context, principalID string) (string, bool) {
	var typ, status string
	err := s.db.QueryRowContext(ctx, `SELECT principal_type,status FROM principals WHERE id=?`, principalID).Scan(&typ, &status)
	return typ, err == nil && status == "active"
}
func (s *Service) memberActorAllowed(ctx context.Context, teamID string, memberID *string, actor string, humanOnly bool) bool {
	if memberID == nil {
		if humanOnly {
			return s.isHuman(ctx, actor)
		}
		_, ok := s.principalType(ctx, actor)
		return ok
	}
	var mt string
	var pid sql.NullString
	if s.db.QueryRowContext(ctx, `SELECT member_kind,principal_id FROM team_members WHERE id=? AND team_id=? AND status='active'`, *memberID, teamID).Scan(&mt, &pid) != nil {
		return false
	}
	if pid.Valid && pid.String == actor {
		return true
	}
	typ, ok := s.principalType(ctx, actor)
	if pid.Valid && ok && typ == "system" && mt != "human" {
		return true
	}
	if !ok {
		return false
	}
	if mt == "human" {
		return false
	}
	return typ == "system" || typ == "agent"
}

func (s *Service) workspaceMember(ctx context.Context, ws, p string) bool {
	var st string
	return s.db.QueryRowContext(ctx, `SELECT status FROM workspace_memberships WHERE workspace_id=? AND principal_id=?`, ws, p).Scan(&st) == nil && st == "active"
}
func (s *Service) emit(ctx context.Context, tx storage.Tx, ws, typ, aggType, aggID string, actor *string, payload any) error {
	eid, _ := s.ids.New("evt")
	raw, _ := json.Marshal(payload)
	return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &ws, Type: typ, AggregateType: aggType, AggregateID: aggID, ActorPrincipalID: actor, Payload: raw, OccurredAt: s.clock.UnixMilli()})
}

func (s *Service) CreateTeam(ctx context.Context, c CreateTeamCommand) (Team, error) {
	if strings.TrimSpace(c.WorkspaceID) == "" || strings.TrimSpace(c.Name) == "" || !s.workspaceMember(ctx, c.WorkspaceID, c.CreatedBy) {
		return Team{}, ErrInvalid
	}
	tid, _ := s.ids.New("team")
	now := s.clock.UnixMilli()
	t := Team{ID: tid, WorkspaceID: c.WorkspaceID, Name: strings.TrimSpace(c.Name), Purpose: strings.TrimSpace(c.Purpose), Status: "active", CreatedBy: c.CreatedBy, Configuration: json.RawMessage(`{}`), Revision: 1, CreatedAt: now, UpdatedAt: now}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO teams(id,workspace_id,name,purpose,status,created_by,configuration_json,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,1,?,?)`, t.ID, t.WorkspaceID, t.Name, t.Purpose, t.Status, t.CreatedBy, string(t.Configuration), now, now); e != nil {
			return e
		}
		actor := c.CreatedBy
		return s.emit(ctx, tx, t.WorkspaceID, "team.created", "team", t.ID, &actor, map[string]any{"name": t.Name})
	})
	return t, err
}
func (s *Service) Team(ctx context.Context, idv string) (Team, error) {
	var t Team
	var cfg string
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,purpose,status,created_by,configuration_json,revision,created_at,updated_at FROM teams WHERE id=?`, idv).Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Purpose, &t.Status, &t.CreatedBy, &cfg, &t.Revision, &t.CreatedAt, &t.UpdatedAt)
	if err == nil {
		t.Configuration = json.RawMessage(cfg)
	}
	return t, err
}
func (s *Service) ListTeams(ctx context.Context, ws string) ([]Team, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,name,purpose,status,created_by,configuration_json,revision,created_at,updated_at FROM teams WHERE workspace_id=? ORDER BY status,name,id`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Team
	for rows.Next() {
		var t Team
		var cfg string
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.Name, &t.Purpose, &t.Status, &t.CreatedBy, &cfg, &t.Revision, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Configuration = json.RawMessage(cfg)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Service) UpdateConfiguration(ctx context.Context, c UpdateConfigurationCommand) (Team, error) {
	t, err := s.Team(ctx, c.TeamID)
	if err != nil { return Team{}, err }
	if c.ActorPrincipalID == "" || c.ExpectedRevision < 1 || !s.workspaceMember(ctx, t.WorkspaceID, c.ActorPrincipalID) { return Team{}, ErrInvalid }
	cfg, err := canonical(c.Configuration)
	if err != nil { return Team{}, err }
	now := s.clock.UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE teams SET configuration_json=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, string(cfg), now, t.ID, c.ExpectedRevision)
	if err != nil { return Team{}, err }
	n, _ := res.RowsAffected()
	if n != 1 { return Team{}, ErrInvalid }
	actor := c.ActorPrincipalID
	_ = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		return s.emit(ctx, tx, t.WorkspaceID, "team.configuration_updated", "team", t.ID, &actor, map[string]any{"revision": c.ExpectedRevision + 1})
	})
	return s.Team(ctx, t.ID)
}

func (s *Service) AddMember(ctx context.Context, c AddMemberCommand) (Member, error) {
	t, err := s.Team(ctx, c.TeamID)
	if err != nil {
		return Member{}, err
	}
	if t.Status != "active" || !s.workspaceMember(ctx, t.WorkspaceID, c.CreatedBy) {
		return Member{}, ErrInvalid
	}
	switch c.MemberKind {
	case "human", "agent", "supervisor":
	default:
		return Member{}, ErrInvalid
	}
	var activeMembers int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_members WHERE team_id=? AND status='active'`, t.ID).Scan(&activeMembers); err != nil || activeMembers >= 8 {
		return Member{}, ErrInvalid
	}
	if strings.TrimSpace(c.DisplayName) == "" || strings.TrimSpace(c.RoleName) == "" {
		return Member{}, ErrInvalid
	}
	if c.PrincipalID != nil {
		var typ, st string
		if err := s.db.QueryRowContext(ctx, `SELECT principal_type,status FROM principals WHERE id=?`, *c.PrincipalID).Scan(&typ, &st); err != nil || st != "active" || !s.workspaceMember(ctx, t.WorkspaceID, *c.PrincipalID) {
			return Member{}, ErrInvalid
		}
		if c.MemberKind == "human" && typ != "human" {
			return Member{}, ErrInvalid
		}
	}
	if c.CapabilityID == "" {
		c.CapabilityID = "inference.general"
	}
	if c.ProtocolLevel == "" {
		c.ProtocolLevel = "L1"
	}
	switch c.ProtocolLevel {
	case "L0", "L1", "L2", "L3":
	default:
		return Member{}, ErrInvalid
	}
	rp, err := canonical(c.RoutePolicy)
	if err != nil {
		return Member{}, err
	}
	cfg, err := canonical(c.Config)
	if err != nil {
		return Member{}, err
	}
	// Operator-mediated Web seats are consultation-only and cannot be
	// configured with a runnable agent protocol or principal identity.
	var manualSeat struct{
		ManualWeb *struct{
			Enabled bool `json:"enabled"`
			ProviderID string `json:"provider_id"`
			ModelLabel string `json:"model_label"`
		} `json:"manual_web"`
	}
	if err := json.Unmarshal(cfg, &manualSeat); err != nil {return Member{}, ErrInvalid}
	if manualSeat.ManualWeb != nil && manualSeat.ManualWeb.Enabled {
		if _,_,err:=ValidateManualWebProvider(manualSeat.ManualWeb.ProviderID,manualSeat.ManualWeb.ModelLabel);err!=nil {
			return Member{},ErrInvalid
		}
		if c.MemberKind!="agent" || c.PrincipalID!=nil || c.ProtocolLevel!="L0" || c.CapabilityID!="inference.general" {
			return Member{},ErrInvalid
		}
	}
	mid, _ := s.ids.New("tmember")
	now := s.clock.UnixMilli()
	m := Member{ID: mid, TeamID: t.ID, WorkspaceID: t.WorkspaceID, PrincipalID: c.PrincipalID, MemberKind: c.MemberKind, DisplayName: strings.TrimSpace(c.DisplayName), RoleName: strings.TrimSpace(c.RoleName), CapabilityID: c.CapabilityID, ProtocolLevel: c.ProtocolLevel, RoutePolicy: rp, Ordinal: c.Ordinal, Status: "active", Config: cfg, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO team_members(id,team_id,workspace_id,principal_id,member_kind,display_name,role_name,capability_id,protocol_level,route_policy_json,ordinal,status,config_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.TeamID, m.WorkspaceID, m.PrincipalID, m.MemberKind, m.DisplayName, m.RoleName, m.CapabilityID, m.ProtocolLevel, string(m.RoutePolicy), m.Ordinal, m.Status, string(m.Config), now, now); e != nil {
			return e
		}
		actor := c.CreatedBy
		return s.emit(ctx, tx, m.WorkspaceID, "team.member_added", "team", m.TeamID, &actor, map[string]any{"member_id": m.ID, "kind": m.MemberKind, "role": m.RoleName})
	})
	return m, err
}
func scanMember(row interface{ Scan(...any) error }) (Member, error) {
	var m Member
	var p sql.NullString
	var rp, cfg string
	err := row.Scan(&m.ID, &m.TeamID, &m.WorkspaceID, &p, &m.MemberKind, &m.DisplayName, &m.RoleName, &m.CapabilityID, &m.ProtocolLevel, &rp, &m.Ordinal, &m.Status, &cfg, &m.CreatedAt, &m.UpdatedAt)
	if p.Valid {
		m.PrincipalID = &p.String
	}
	m.RoutePolicy = json.RawMessage(rp)
	m.Config = json.RawMessage(cfg)
	return m, err
}
func (s *Service) ListMembers(ctx context.Context, teamID string) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,team_id,workspace_id,principal_id,member_kind,display_name,role_name,capability_id,protocol_level,route_policy_json,ordinal,status,config_json,created_at,updated_at FROM team_members WHERE team_id=? ORDER BY ordinal,id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		m, e := scanMember(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func profileIDFromMemberConfig(raw json.RawMessage) string {
	var cfg map[string]any
	if json.Unmarshal(raw, &cfg) != nil {
		return ""
	}
	for _, key := range []string{"agent_profile_id", "agent_profile"} {
		if v, ok := cfg[key].(string); ok && strings.TrimSpace(v) != "" {
			idv := strings.TrimSpace(v)
			if strings.EqualFold(idv, "onepane-default") {
				return "agent.md"
			}
			return idv
		}
	}
	return "agent.md"
}

func (s *Service) snapshotMembers(ctx context.Context, teamID, workspaceID string) ([]SnapshotMember, error) {
	members, err := s.ListMembers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	out := make([]SnapshotMember, 0, len(members))
	for _, m := range members {
		if m.Status != "active" {
			continue
		}
		sm := SnapshotMember{ID: m.ID, MemberKind: m.MemberKind, DisplayName: m.DisplayName, RoleName: m.RoleName, CapabilityID: m.CapabilityID, ProtocolLevel: m.ProtocolLevel, PrincipalID: m.PrincipalID, RoutePolicy: append(json.RawMessage(nil), m.RoutePolicy...), Config: append(json.RawMessage(nil), m.Config...), Ordinal: m.Ordinal, Status: m.Status}
		if m.MemberKind != "human" {
			profileID := profileIDFromMemberConfig(m.Config)
			if profileID != "" {
				var p SnapshotAgentProfile
				p.ID = profileID
				err := s.db.QueryRowContext(ctx, `SELECT name,instructions_md,default_role,revision FROM agent_profiles WHERE id=? AND status='active' AND (workspace_id=? OR workspace_id IS NULL) ORDER BY CASE WHEN workspace_id=? THEN 0 ELSE 1 END LIMIT 1`, profileID, workspaceID, workspaceID).Scan(&p.Name, &p.Instructions, &p.Role, &p.Revision)
				if err == nil {
					sm.Profile = &p
				} else if !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
			}
		}
		out = append(out, sm)
	}
	return out, nil
}

func researchConfiguration(raw json.RawMessage) (bool, ResearchSettings) {
	var cfg struct {
		ResearchMode bool             `json:"research_mode"`
		Research     ResearchSettings `json:"research"`
	}
	_ = json.Unmarshal(raw, &cfg)
	if cfg.ResearchMode {
		cfg.Research = NormalizeResearchSettings(cfg.Research)
	}
	return cfg.ResearchMode, cfg.Research
}

func (s *Service) SessionManifest(ctx context.Context, sessionID string) (SessionManifest, error) {
	var m SessionManifest
	var research int
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT session_id,workspace_id,team_id,execution_mode,research_mode,snapshot_json,snapshot_sha256,created_at FROM team_session_manifests WHERE session_id=?`, sessionID).Scan(&m.SessionID, &m.WorkspaceID, &m.TeamID, &m.ExecutionMode, &research, &raw, &m.SnapshotSHA256, &m.CreatedAt)
	m.ResearchMode = research != 0
	m.Snapshot = json.RawMessage(raw)
	return m, err
}

func (s *Service) SeatBinding(ctx context.Context, sessionID, memberID string) (SeatBinding, error) {
	var b SeatBinding
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT session_id,member_id,candidate_kind,candidate_id,candidate_snapshot_json,created_at FROM team_session_seat_bindings WHERE session_id=? AND member_id=?`, sessionID, memberID).Scan(&b.SessionID, &b.MemberID, &b.CandidateKind, &b.CandidateID, &raw, &b.CreatedAt)
	b.CandidateSnapshot = json.RawMessage(raw)
	return b, err
}

func (s *Service) ListSeatBindings(ctx context.Context, sessionID string) ([]SeatBinding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT session_id,member_id,candidate_kind,candidate_id,candidate_snapshot_json,created_at FROM team_session_seat_bindings WHERE session_id=? ORDER BY member_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeatBinding
	for rows.Next() {
		var b SeatBinding
		var raw string
		if err := rows.Scan(&b.SessionID, &b.MemberID, &b.CandidateKind, &b.CandidateID, &raw, &b.CreatedAt); err != nil {
			return nil, err
		}
		b.CandidateSnapshot = json.RawMessage(raw)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Service) BindSeat(ctx context.Context, sessionID, memberID, candidateKind, candidateID string, candidateSnapshot json.RawMessage) (SeatBinding, error) {
	ss, err := s.Session(ctx, sessionID)
	if err != nil {
		return SeatBinding{}, err
	}
	if candidateKind != "model_deployment" && candidateKind != "agent_runtime" || strings.TrimSpace(candidateID) == "" {
		return SeatBinding{}, ErrInvalid
	}
	var teamID string
	if err := s.db.QueryRowContext(ctx, `SELECT team_id FROM team_members WHERE id=? AND status='active'`, memberID).Scan(&teamID); err != nil || teamID != ss.TeamID {
		return SeatBinding{}, ErrInvalid
	}
	raw, err := canonical(candidateSnapshot)
	if err != nil {
		return SeatBinding{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO team_session_seat_bindings(session_id,member_id,candidate_kind,candidate_id,candidate_snapshot_json,created_at) VALUES(?,?,?,?,?,?)`, sessionID, memberID, candidateKind, candidateID, string(raw), now); err != nil {
			return err
		}
		return s.emit(ctx, tx, ss.WorkspaceID, "team.seat_bound", "team_session", sessionID, nil, map[string]any{"member_id": memberID, "candidate_kind": candidateKind, "candidate_id": candidateID})
	})
	if err != nil {
		return SeatBinding{}, err
	}
	return s.SeatBinding(ctx, sessionID, memberID)
}

func (s *Service) StartSession(ctx context.Context, c StartSessionCommand) (Session, error) {
	t, err := s.Team(ctx, c.TeamID)
	if err != nil {
		return Session{}, err
	}
	taskRow, err := s.tasks.Get(ctx, c.TaskID)
	if err != nil {
		return Session{}, err
	}
	if taskRow.WorkspaceID != t.WorkspaceID || taskRow.State != task.StateCreated || !s.isHuman(ctx, c.CreatedBy) || !s.workspaceMember(ctx, t.WorkspaceID, c.CreatedBy) {
		return Session{}, ErrInvalid
	}
	mode := strings.ToLower(strings.TrimSpace(c.ExecutionMode))
	if mode == "" {
		mode = "team"
	}
	if mode != "team" && mode != "council" {
		return Session{}, ErrInvalid
	}
	cfg, err := canonical(c.Config)
	if err != nil {
		return Session{}, err
	}
	members, err := s.snapshotMembers(ctx, t.ID, t.WorkspaceID)
	if err != nil {
		return Session{}, err
	}
	for _, member := range members {
		var cfg struct {ManualWeb *struct {Enabled bool `json:"enabled"`} `json:"manual_web"`}
		if err:=json.Unmarshal(member.Config,&cfg);err!=nil{return Session{},ErrInvalid}
		if cfg.ManualWeb!=nil && cfg.ManualWeb.Enabled && mode!="council"{
			return Session{},ErrInvalid
		}
	}
	researchMode, research := researchConfiguration(t.Configuration)
	if mode != "council" {
		researchMode = false
	}
	sid, _ := s.ids.New("tsession")
	now := s.clock.UnixMilli()
	snapshot := SessionSnapshot{SessionID: sid, TaskID: taskRow.ID, TaskObjective: taskRow.Objective, TeamID: t.ID, TeamRevision: t.Revision, TeamConfiguration: append(json.RawMessage(nil), t.Configuration...), SessionConfiguration: append(json.RawMessage(nil), cfg...), ExecutionMode: mode, ResearchMode: researchMode, Research: research, GatewayTargetID: c.GatewayTargetID, Members: members}
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		return Session{}, err
	}
	sum := sha256.Sum256(snapshotRaw)
	manifestSHA := hex.EncodeToString(sum[:])
	ss := Session{ID: sid, WorkspaceID: t.WorkspaceID, TeamID: t.ID, TaskID: taskRow.ID, Status: "deliberating", GatewayTargetID: c.GatewayTargetID, RoundNumber: 0, Revision: 1, Config: cfg, CreatedBy: c.CreatedBy, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if c.GatewayTargetID != nil {
			var ws string
			if e := tx.QueryRowContext(ctx, `SELECT workspace_id FROM gateway_targets WHERE id=?`, *c.GatewayTargetID).Scan(&ws); e != nil || ws != ss.WorkspaceID {
				return ErrWorkspaceMismatch
			}
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO team_sessions(id,workspace_id,team_id,task_id,status,accepted_plan_id,gateway_target_id,round_number,config_json,revision,created_by,created_at,updated_at) VALUES(?,?,?,?,?,NULL,?,0,?,1,?,?,?)`, ss.ID, ss.WorkspaceID, ss.TeamID, ss.TaskID, ss.Status, ss.GatewayTargetID, string(ss.Config), ss.CreatedBy, now, now); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO task_execution_profiles(task_id,workspace_id,execution_mode,team_id,team_session_id,config_json,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?, ?,?,?)`, ss.TaskID, ss.WorkspaceID, mode, ss.TeamID, ss.ID, string(ss.Config), ss.CreatedBy, now, now); e != nil {
			return e
		}
		researchInt := 0
		if researchMode {
			researchInt = 1
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO team_session_manifests(session_id,workspace_id,team_id,execution_mode,research_mode,snapshot_json,snapshot_sha256,created_at) VALUES(?,?,?,?,?,?,?,?)`, ss.ID, ss.WorkspaceID, ss.TeamID, mode, researchInt, string(snapshotRaw), manifestSHA, now); e != nil {
			return e
		}
		actor := c.CreatedBy
		return s.emit(ctx, tx, ss.WorkspaceID, "team.session_started", "team_session", ss.ID, &actor, map[string]any{"task_id": ss.TaskID, "team_id": ss.TeamID, "execution_mode": mode, "research_mode": researchMode, "manifest_sha256": manifestSHA})
	})
	return ss, err
}
func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var s0 Session
	var plan, gt sql.NullString
	var cfg string
	err := row.Scan(&s0.ID, &s0.WorkspaceID, &s0.TeamID, &s0.TaskID, &s0.Status, &plan, &gt, &s0.RoundNumber, &cfg, &s0.Revision, &s0.CreatedBy, &s0.CreatedAt, &s0.UpdatedAt)
	if plan.Valid {
		s0.AcceptedPlanID = &plan.String
	}
	if gt.Valid {
		s0.GatewayTargetID = &gt.String
	}
	s0.Config = json.RawMessage(cfg)
	return s0, err
}
func (s *Service) Session(ctx context.Context, idv string) (Session, error) {
	return scanSession(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,team_id,task_id,status,accepted_plan_id,gateway_target_id,round_number,config_json,revision,created_by,created_at,updated_at FROM team_sessions WHERE id=?`, idv))
}
func (s *Service) SessionByTask(ctx context.Context, taskID string) (Session, error) {
	return scanSession(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,team_id,task_id,status,accepted_plan_id,gateway_target_id,round_number,config_json,revision,created_by,created_at,updated_at FROM team_sessions WHERE task_id=?`, taskID))
}

func (s *Service) PostMessage(ctx context.Context, c PostMessageCommand) (Message, error) {
	ss, err := s.Session(ctx, c.SessionID)
	if err != nil {
		return Message{}, err
	}
	if ss.Status != "deliberating" && ss.Status != "plan_proposed" && ss.Status != "paused_for_deliberation" && ss.Status != "team_review" {
		return Message{}, ErrSessionState
	}
	if c.Kind == "" {
		c.Kind = "human"
	}
	switch c.Kind {
	case "human", "agent", "supervisor", "system":
	default:
		return Message{}, ErrInvalid
	}
	if c.Kind == "human" && !s.isHuman(ctx, c.AuthorPrincipalID) {
		return Message{}, ErrHumanRequired
	}
	if !s.workspaceMember(ctx, ss.WorkspaceID, c.AuthorPrincipalID) {
		return Message{}, ErrInvalid
	}
	raw, err := canonical(c.Content)
	if err != nil {
		return Message{}, err
	}
	if c.AuthorMemberID != nil {
		var teamID string
		if err := s.db.QueryRowContext(ctx, `SELECT team_id FROM team_members WHERE id=?`, *c.AuthorMemberID).Scan(&teamID); err != nil || teamID != ss.TeamID {
			return Message{}, ErrInvalid
		}
	}
	if !s.memberActorAllowed(ctx, ss.TeamID, c.AuthorMemberID, c.AuthorPrincipalID, c.Kind == "human") {
		return Message{}, ErrInvalid
	}
	mid, _ := s.ids.New("tmsg")
	now := s.clock.UnixMilli()
	m := Message{ID: mid, WorkspaceID: ss.WorkspaceID, SessionID: ss.ID, AuthorMemberID: c.AuthorMemberID, AuthorPrincipalID: &c.AuthorPrincipalID, Kind: c.Kind, Content: raw, ReplyToMessageID: c.ReplyToMessageID, RoundNumber: ss.RoundNumber, CreatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,reply_to_message_id,round_number,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, m.ID, m.WorkspaceID, m.SessionID, m.AuthorMemberID, m.AuthorPrincipalID, m.Kind, string(m.Content), m.ReplyToMessageID, m.RoundNumber, m.CreatedAt); e != nil {
			return e
		}
		actor := c.AuthorPrincipalID
		return s.emit(ctx, tx, m.WorkspaceID, "team.message_posted", "team_session", m.SessionID, &actor, map[string]any{"message_id": m.ID, "kind": m.Kind, "round": m.RoundNumber})
	})
	return m, err
}
func (s *Service) ListMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,reply_to_message_id,round_number,created_at FROM team_messages WHERE session_id=? ORDER BY created_at,id LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var am, ap, rp sql.NullString
		var raw string
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.SessionID, &am, &ap, &m.Kind, &raw, &rp, &m.RoundNumber, &m.CreatedAt); err != nil {
			return nil, err
		}
		if am.Valid {
			m.AuthorMemberID = &am.String
		}
		if ap.Valid {
			m.AuthorPrincipalID = &ap.String
		}
		if rp.Valid {
			m.ReplyToMessageID = &rp.String
		}
		m.Content = json.RawMessage(raw)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) RequestRound(ctx context.Context, c RequestRoundCommand) ([]TurnRequest, error) {
	ss, err := s.Session(ctx, c.SessionID)
	if err != nil {
		return nil, err
	}
	if ss.Status != "deliberating" && ss.Status != "plan_proposed" && ss.Status != "paused_for_deliberation" && ss.Status != "team_review" {
		return nil, ErrSessionState
	}
	if !s.isHuman(ctx, c.RequestedByPrincipalID) && !s.workspaceMember(ctx, ss.WorkspaceID, c.RequestedByPrincipalID) {
		return nil, ErrInvalid
	}
	members, err := s.ListMembers(ctx, ss.TeamID)
	if err != nil {
		return nil, err
	}
	var researchMode bool
	var research ResearchSettings
	if manifest, manifestErr := s.SessionManifest(ctx, ss.ID); manifestErr == nil {
		var snapshot SessionSnapshot
		if err := json.Unmarshal(manifest.Snapshot, &snapshot); err != nil {
			return nil, err
		}
		researchMode = snapshot.ResearchMode && snapshot.ExecutionMode == "council"
		research = snapshot.Research
		members = members[:0]
		for _, sm := range snapshot.Members {
			members = append(members, Member{ID: sm.ID, TeamID: ss.TeamID, WorkspaceID: ss.WorkspaceID, PrincipalID: sm.PrincipalID, MemberKind: sm.MemberKind, DisplayName: sm.DisplayName, RoleName: sm.RoleName, CapabilityID: sm.CapabilityID, ProtocolLevel: sm.ProtocolLevel, RoutePolicy: append(json.RawMessage(nil), sm.RoutePolicy...), Ordinal: sm.Ordinal, Status: sm.Status, Config: append(json.RawMessage(nil), sm.Config...)})
		}
	} else if !errors.Is(manifestErr, sql.ErrNoRows) {
		return nil, manifestErr
	}
	want := map[string]bool{}
	if !(researchMode && research.RequireAllSeats) || c.ExactMemberSelection {
		for _, v := range c.MemberIDs {
			want[v] = true
		}
	}
	now := s.clock.UnixMilli()
	var out []TurnRequest
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		res, e := tx.ExecContext(ctx, `UPDATE team_sessions SET round_number=round_number+1,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, now, ss.ID, ss.Revision)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrSessionState
		}
		round := ss.RoundNumber + 1
		phase := strings.TrimSpace(c.ResearchPhase)
		if researchMode {
			expectedPhase := ResearchPhaseForRound(research, round)
			if phase == "" {
				phase = expectedPhase
			}
			if expectedPhase == "" || phase != expectedPhase {
				return ErrSessionState
			}
		} else {
			phase = ""
		}
		for _, m := range members {
			if m.Status != "active" || m.MemberKind == "human" {
				continue
			}
			if len(want) > 0 && !want[m.ID] {
				continue
			}
			idv, _ := s.ids.New("tturn")
			tr := TurnRequest{ID: idv, WorkspaceID: ss.WorkspaceID, SessionID: ss.ID, MemberID: m.ID, Status: "pending", TriggerMessageID: c.TriggerMessageID, RoundNumber: round, ResearchPhase: phase, CreatedAt: now, UpdatedAt: now}
			if _, e := tx.ExecContext(ctx, `INSERT INTO team_turn_requests(id,workspace_id,session_id,member_id,trigger_message_id,status,round_number,research_phase,attempt_count,retry_after,created_at,updated_at) VALUES(?,?,?,?,?,'pending',?,?,0,NULL,?,?)`, tr.ID, tr.WorkspaceID, tr.SessionID, tr.MemberID, tr.TriggerMessageID, tr.RoundNumber, tr.ResearchPhase, now, now); e != nil {
				return e
			}
			out = append(out, tr)
		}
		actor := c.RequestedByPrincipalID
		return s.emit(ctx, tx, ss.WorkspaceID, "team.round_requested", "team_session", ss.ID, &actor, map[string]any{"round": round, "phase": phase, "turns": len(out), "research_mode": researchMode, "require_all_seats": researchMode && research.RequireAllSeats})
	})
	return out, err
}

func (s *Service) ProposePlan(ctx context.Context, c ProposePlanCommand) (Plan, error) {
	ss, err := s.Session(ctx, c.SessionID)
	if err != nil {
		return Plan{}, err
	}
	if ss.Status != "deliberating" && ss.Status != "plan_proposed" && ss.Status != "paused_for_deliberation" && ss.Status != "team_review" {
		return Plan{}, ErrSessionState
	}
	if !s.workspaceMember(ctx, ss.WorkspaceID, c.ProposedByPrincipalID) {
		return Plan{}, ErrInvalid
	}
	if !s.memberActorAllowed(ctx, ss.TeamID, c.ProposedByMemberID, c.ProposedByPrincipalID, false) {
		return Plan{}, ErrInvalid
	}
	body, err := canonical(c.Plan)
	if err != nil {
		return Plan{}, err
	}
	pid, _ := s.ids.New("tplan")
	now := s.clock.UnixMilli()
	var version int64
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if e := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM team_plans WHERE session_id=?`, ss.ID).Scan(&version); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE team_plans SET status='superseded' WHERE session_id=? AND status='candidate'`, ss.ID); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO team_plans(id,workspace_id,session_id,version,status,plan_json,proposed_by_member_id,proposed_by_principal_id,created_at) VALUES(?,?,?,?,'candidate',?,?,?,?)`, pid, ss.WorkspaceID, ss.ID, version, string(body), c.ProposedByMemberID, c.ProposedByPrincipalID, now); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE team_sessions SET status='plan_proposed',revision=revision+1,updated_at=? WHERE id=?`, now, ss.ID); e != nil {
			return e
		}
		actor := c.ProposedByPrincipalID
		return s.emit(ctx, tx, ss.WorkspaceID, "team.plan_proposed", "team_session", ss.ID, &actor, map[string]any{"plan_id": pid, "version": version})
	})
	return Plan{ID: pid, WorkspaceID: ss.WorkspaceID, SessionID: ss.ID, Version: version, Status: "candidate", Body: body, ProposedByMemberID: c.ProposedByMemberID, ProposedByPrincipalID: &c.ProposedByPrincipalID, CreatedAt: now}, err
}
func (s *Service) RaiseObjection(ctx context.Context, c RaiseObjectionCommand) (Objection, error) {
	ss, err := s.Session(ctx, c.SessionID)
	if err != nil {
		return Objection{}, err
	}
	if !s.workspaceMember(ctx, ss.WorkspaceID, c.ActorPrincipalID) || !s.memberActorAllowed(ctx, ss.TeamID, c.RaisedByMemberID, c.ActorPrincipalID, false) {
		return Objection{}, ErrInvalid
	}
	switch c.Severity {
	case "note", "concern", "blocking", "critical":
	default:
		return Objection{}, ErrInvalid
	}
	detail, err := canonical(c.Detail)
	if err != nil {
		return Objection{}, err
	}
	oid, _ := s.ids.New("tobj")
	now := s.clock.UnixMilli()
	o := Objection{ID: oid, WorkspaceID: ss.WorkspaceID, SessionID: ss.ID, PlanID: c.PlanID, RaisedByMemberID: c.RaisedByMemberID, Severity: c.Severity, Status: "open", Summary: strings.TrimSpace(c.Summary), Detail: detail, CreatedAt: now}
	if o.Summary == "" {
		return Objection{}, ErrInvalid
	}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO team_objections(id,workspace_id,session_id,plan_id,raised_by_member_id,severity,status,summary,detail_json,created_at) VALUES(?,?,?,?,?,?,'open',?,?,?)`, o.ID, o.WorkspaceID, o.SessionID, o.PlanID, o.RaisedByMemberID, o.Severity, o.Summary, string(o.Detail), o.CreatedAt); e != nil {
			return e
		}
		actor := c.ActorPrincipalID
		return s.emit(ctx, tx, o.WorkspaceID, "team.objection_raised", "team_session", o.SessionID, &actor, map[string]any{"objection_id": o.ID, "severity": o.Severity, "plan_id": o.PlanID})
	})
	return o, err
}
func (s *Service) ResolveObjection(ctx context.Context, c ResolveObjectionCommand) (Objection, error) {
	if !s.isHuman(ctx, c.ResolvedByPrincipalID) {
		return Objection{}, ErrHumanRequired
	}
	switch c.Status {
	case "resolved", "accepted_risk", "dismissed":
	default:
		return Objection{}, ErrInvalid
	}
	res, err := canonical(c.Resolution)
	if err != nil {
		return Objection{}, err
	}
	now := s.clock.UnixMilli()
	var ws, session string
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if e := tx.QueryRowContext(ctx, `SELECT workspace_id,session_id FROM team_objections WHERE id=? AND status='open'`, c.ObjectionID).Scan(&ws, &session); e != nil {
			return e
		}
		if !s.workspaceMember(ctx, ws, c.ResolvedByPrincipalID) {
			return ErrInvalid
		}
		r, e := tx.ExecContext(ctx, `UPDATE team_objections SET status=?,resolution_json=?,resolved_by_principal_id=?,resolved_at=? WHERE id=? AND status='open'`, c.Status, string(res), c.ResolvedByPrincipalID, now, c.ObjectionID)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrInvalid
		}
		actor := c.ResolvedByPrincipalID
		return s.emit(ctx, tx, ws, "team.objection_resolved", "team_session", session, &actor, map[string]any{"objection_id": c.ObjectionID, "status": c.Status})
	})
	if err != nil {
		return Objection{}, err
	}
	return s.Objection(ctx, c.ObjectionID)
}
func (s *Service) Objection(ctx context.Context, idv string) (Objection, error) {
	var o Objection
	var plan, raised, resolver sql.NullString
	var detail string
	var resolution sql.NullString
	var resolved sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,session_id,plan_id,raised_by_member_id,severity,status,summary,detail_json,resolution_json,resolved_by_principal_id,created_at,resolved_at FROM team_objections WHERE id=?`, idv).Scan(&o.ID, &o.WorkspaceID, &o.SessionID, &plan, &raised, &o.Severity, &o.Status, &o.Summary, &detail, &resolution, &resolver, &o.CreatedAt, &resolved)
	if plan.Valid {
		o.PlanID = &plan.String
	}
	if raised.Valid {
		o.RaisedByMemberID = &raised.String
	}
	if resolver.Valid {
		o.ResolvedByPrincipalID = &resolver.String
	}
	o.Detail = json.RawMessage(detail)
	if resolution.Valid {
		o.Resolution = json.RawMessage(resolution.String)
	}
	if resolved.Valid {
		o.ResolvedAt = &resolved.Int64
	}
	return o, err
}

func (s *Service) AcceptPlan(ctx context.Context, c AcceptPlanCommand) (Session, error) {
	ss, err := s.Session(ctx, c.SessionID)
	if err != nil {
		return Session{}, err
	}
	if !s.isHuman(ctx, c.AcceptedByPrincipalID) || !s.workspaceMember(ctx, ss.WorkspaceID, c.AcceptedByPrincipalID) {
		return Session{}, ErrHumanRequired
	}
	var planStatus string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM team_plans WHERE id=? AND session_id=?`, c.PlanID, ss.ID).Scan(&planStatus); err != nil || planStatus != "candidate" {
		return Session{}, ErrInvalid
	}
	var blocking int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_objections WHERE session_id=? AND (plan_id IS NULL OR plan_id=?) AND status='open' AND severity IN ('blocking','critical')`, ss.ID, c.PlanID).Scan(&blocking); err != nil {
		return Session{}, err
	}
	if blocking > 0 && !c.AcceptOpenRisk {
		return Session{}, ErrOpenBlockingObjection
	}
	decision, err := canonical(c.Decision)
	if err != nil {
		return Session{}, err
	}
	now := s.clock.UnixMilli()
	did, _ := s.ids.New("tdecision")
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if blocking > 0 {
			if _, e := tx.ExecContext(ctx, `UPDATE team_objections SET status='accepted_risk',resolution_json=?,resolved_by_principal_id=?,resolved_at=? WHERE session_id=? AND (plan_id IS NULL OR plan_id=?) AND status='open' AND severity IN ('blocking','critical')`, string(decision), c.AcceptedByPrincipalID, now, ss.ID, c.PlanID); e != nil {
				return e
			}
		}
		if _, e := tx.ExecContext(ctx, `UPDATE team_plans SET status='accepted',accepted_by_principal_id=?,accepted_at=? WHERE id=? AND status='candidate'`, c.AcceptedByPrincipalID, now, c.PlanID); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE team_plans SET status='superseded' WHERE session_id=? AND id<>? AND status='candidate'`, ss.ID, c.PlanID); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE team_sessions SET status='plan_accepted',accepted_plan_id=?,revision=revision+1,updated_at=? WHERE id=?`, c.PlanID, now, ss.ID); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO team_decisions(id,workspace_id,session_id,decision_type,decision_json,decided_by_principal_id,created_at) VALUES(?,?,?,'plan_acceptance',?,?,?)`, did, ss.WorkspaceID, ss.ID, string(decision), c.AcceptedByPrincipalID, now); e != nil {
			return e
		}
		actor := c.AcceptedByPrincipalID
		return s.emit(ctx, tx, ss.WorkspaceID, "team.plan_accepted", "team_session", ss.ID, &actor, map[string]any{"plan_id": c.PlanID, "accepted_open_risk": blocking > 0})
	})
	if err != nil {
		return Session{}, err
	}
	tr, err := s.tasks.Get(ctx, ss.TaskID)
	if err != nil {
		return Session{}, err
	}
	if tr.State == task.StateCreated {
		if _, err = s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: tr.ID, ExpectedRevision: tr.Revision, ActorPrincipalID: &c.AcceptedByPrincipalID, Reason: "team plan accepted"}); err != nil {
			return Session{}, err
		}
	} else if tr.State == task.StatePaused {
		if _, err = s.tasks.Resume(ctx, task.TransitionCommand{TaskID: tr.ID, ExpectedRevision: tr.Revision, ActorPrincipalID: &c.AcceptedByPrincipalID, Reason: "revised team plan accepted"}); err != nil {
			return Session{}, err
		}
	}
	return s.Session(ctx, ss.ID)
}

func (s *Service) PauseResearchRound(ctx context.Context, c PauseResearchRoundCommand) (Session, error) {
	ss, err := s.Session(ctx, c.SessionID)
	if err != nil {
		return Session{}, err
	}
	if ss.Status != "deliberating" || ss.RoundNumber <= 0 || strings.TrimSpace(c.TurnID) == "" || strings.TrimSpace(c.ActorPrincipalID) == "" {
		return Session{}, ErrSessionState
	}
	manifest, err := s.SessionManifest(ctx, ss.ID)
	if err != nil || !manifest.ResearchMode || manifest.ExecutionMode != "council" {
		return Session{}, ErrSessionState
	}
	if !s.workspaceMember(ctx, ss.WorkspaceID, c.ActorPrincipalID) {
		return Session{}, ErrInvalid
	}
	now := s.clock.UnixMilli()
	reason := strings.TrimSpace(c.Reason)
	if reason == "" {
		reason = "research provider unavailable"
	}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		res, e := tx.ExecContext(ctx, `UPDATE team_turn_requests
			SET status='blocked',error_text=?,attempt_count=attempt_count+1,retry_after=?,updated_at=?,completed_at=?
			WHERE id=? AND session_id=? AND round_number=? AND status='running' AND response_message_id IS NULL`,
			reason, c.RetryAfter, now, now, c.TurnID, ss.ID, ss.RoundNumber)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrSessionState
		}
		res, e = tx.ExecContext(ctx, `UPDATE team_sessions SET status='paused_for_deliberation',revision=revision+1,updated_at=? WHERE id=? AND status='deliberating'`, now, ss.ID)
		if e != nil {
			return e
		}
		n, _ = res.RowsAffected()
		if n != 1 {
			return ErrSessionState
		}
		actor := c.ActorPrincipalID
		return s.emit(ctx, tx, ss.WorkspaceID, "team.research_round_paused", "team_session", ss.ID, &actor, map[string]any{
			"round": ss.RoundNumber, "reason": reason, "retry_after": c.RetryAfter, "turn_id": c.TurnID,
		})
	})
	if err != nil {
		return Session{}, err
	}
	return s.Session(ctx, ss.ID)
}

func (s *Service) RetryResearchRound(ctx context.Context, c RetryResearchRoundCommand) (Session, error) {
	ss, err := s.Session(ctx, c.SessionID)
	if err != nil {
		return Session{}, err
	}
	if ss.Status != "paused_for_deliberation" || ss.RoundNumber <= 0 {
		return Session{}, ErrSessionState
	}
	manifest, err := s.SessionManifest(ctx, ss.ID)
	if err != nil || !manifest.ResearchMode || manifest.ExecutionMode != "council" {
		return Session{}, ErrSessionState
	}
	if strings.TrimSpace(c.RequestedByPrincipalID) == "" || !s.workspaceMember(ctx, ss.WorkspaceID, c.RequestedByPrincipalID) {
		return Session{}, ErrInvalid
	}
	if !c.Automatic && !s.isHuman(ctx, c.RequestedByPrincipalID) {
		return Session{}, ErrHumanRequired
	}
	now := s.clock.UnixMilli()
	if c.Automatic {
		var eligible int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_turn_requests
			WHERE session_id=? AND round_number=? AND response_message_id IS NULL
			AND status IN ('blocked','failed') AND retry_after IS NOT NULL AND retry_after<=?`,
			ss.ID, ss.RoundNumber, now).Scan(&eligible); err != nil {
			return Session{}, err
		}
		if eligible == 0 {
			return Session{}, ErrSessionState
		}
	}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.ExecContext(ctx, `UPDATE team_turn_requests
			SET status='pending',error_text=NULL,retry_after=NULL,completed_at=NULL,updated_at=?
			WHERE session_id=? AND round_number=? AND response_message_id IS NULL
			AND status IN ('pending','blocked','failed')`, now, ss.ID, ss.RoundNumber); e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, `UPDATE team_sessions SET status='deliberating',revision=revision+1,updated_at=? WHERE id=? AND status='paused_for_deliberation'`, now, ss.ID)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrSessionState
		}
		actor := c.RequestedByPrincipalID
		return s.emit(ctx, tx, ss.WorkspaceID, "team.research_round_retried", "team_session", ss.ID, &actor, map[string]any{
			"round": ss.RoundNumber, "automatic": c.Automatic,
		})
	})
	if err != nil {
		return Session{}, err
	}
	return s.Session(ctx, ss.ID)
}

func (s *Service) ReopenDeliberation(ctx context.Context, c ReopenCommand) (Session, error) {
	ss, err := s.Session(ctx, c.SessionID)
	if err != nil {
		return Session{}, err
	}
	if !s.isHuman(ctx, c.RequestedByPrincipalID) || !s.workspaceMember(ctx, ss.WorkspaceID, c.RequestedByPrincipalID) {
		return Session{}, ErrHumanRequired
	}
	tr, err := s.tasks.Get(ctx, ss.TaskID)
	if err != nil {
		return Session{}, err
	}
	if tr.State == task.StateRunning || tr.State == task.StateReady {
		if _, err := s.tasks.Pause(ctx, task.TransitionCommand{TaskID: tr.ID, ExpectedRevision: tr.Revision, ActorPrincipalID: &c.RequestedByPrincipalID, Reason: c.Reason}); err != nil {
			return Session{}, err
		}
	}
	now := s.clock.UnixMilli()
	_, err = s.db.ExecContext(ctx, `UPDATE team_sessions SET status='paused_for_deliberation',accepted_plan_id=NULL,revision=revision+1,updated_at=? WHERE id=?`, now, ss.ID)
	if err != nil {
		return Session{}, err
	}
	return s.Session(ctx, ss.ID)
}

// Admission guard for TaskService. Team-mode Tasks fail closed until an accepted plan exists.
func (s *Service) AllowReady(ctx context.Context, t task.Task) error {
	return s.allowAdmission(ctx, t.ID)
}
func (s *Service) AllowStart(ctx context.Context, t task.Task) error {
	return s.allowAdmission(ctx, t.ID)
}
func manualWebCouncilExecutionDisabled(mode string, cfg json.RawMessage) bool {
	if mode != "council" { return false }
	var policy struct {
		ManualWebOnly bool `json:"manual_web_only"`
	}
	if json.Unmarshal(cfg, &policy) != nil { return true } // fail closed on corrupt Council profile
	return policy.ManualWebOnly
}
func (s *Service) allowAdmission(ctx context.Context, taskID string) error {
	var mode, cfg string
	var session sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT execution_mode,team_session_id,config_json FROM task_execution_profiles WHERE task_id=?`, taskID).Scan(&mode, &session, &cfg)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if manualWebCouncilExecutionDisabled(mode, json.RawMessage(cfg)) {
		return ErrManualWebCouncilExecution
	}
	if mode != "team" {
		return nil
	}
	if !session.Valid {
		return ErrPlanRequired
	}
	var st string
	var plan sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT status,accepted_plan_id FROM team_sessions WHERE id=?`, session.String).Scan(&st, &plan); err != nil {
		return err
	}
	if !plan.Valid || (st != "plan_accepted" && st != "executing") {
		return ErrPlanRequired
	}
	return nil
}

func (s *Service) SyncTaskStates(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,t.state,s.status FROM team_sessions s JOIN tasks t ON t.id=s.task_id WHERE s.status NOT IN ('completed','cancelled')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct{ id, ts, ss string }
	var xs []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.ts, &x.ss); err != nil {
			return err
		}
		xs = append(xs, x)
	}
	now := s.clock.UnixMilli()
	for _, x := range xs {
		next := ""
		switch task.State(x.ts) {
		case task.StateRunning:
			if x.ss == "plan_accepted" {
				next = "executing"
			}
		case task.StateCompletionRequested, task.StateVerifying:
			if x.ss == "executing" {
				next = "team_review"
			}
		case task.StateComplete:
			next = "completed"
		case task.StateCancelled, task.StateFailed:
			next = "cancelled"
		}
		if next != "" && next != x.ss {
			_, _ = s.db.ExecContext(ctx, `UPDATE team_sessions SET status=?,revision=revision+1,updated_at=? WHERE id=?`, next, now, x.id)
		}
	}
	return rows.Err()
}

func (s *Service) ListPlans(ctx context.Context, sessionID string) ([]Plan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,session_id,version,status,plan_json,proposed_by_member_id,proposed_by_principal_id,accepted_by_principal_id,created_at,accepted_at FROM team_plans WHERE session_id=? ORDER BY version`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Plan
	for rows.Next() {
		var p Plan
		var body string
		var pm, pp, ap sql.NullString
		var aa sql.NullInt64
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.SessionID, &p.Version, &p.Status, &body, &pm, &pp, &ap, &p.CreatedAt, &aa); err != nil {
			return nil, err
		}
		p.Body = json.RawMessage(body)
		if pm.Valid {
			p.ProposedByMemberID = &pm.String
		}
		if pp.Valid {
			p.ProposedByPrincipalID = &pp.String
		}
		if ap.Valid {
			p.AcceptedByPrincipalID = &ap.String
		}
		if aa.Valid {
			p.AcceptedAt = &aa.Int64
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Service) ListObjections(ctx context.Context, sessionID string) ([]Objection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM team_objections WHERE session_id=? ORDER BY created_at,id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Objection
	for rows.Next() {
		var idv string
		if err := rows.Scan(&idv); err != nil {
			return nil, err
		}
		o, err := s.Objection(ctx, idv)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *Service) ListDecisions(ctx context.Context, sessionID string) ([]Decision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,session_id,decision_type,decision_json,decided_by_principal_id,created_at FROM team_decisions WHERE session_id=? ORDER BY created_at,id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		var d Decision
		var raw string
		if err := rows.Scan(&d.ID, &d.WorkspaceID, &d.SessionID, &d.DecisionType, &raw, &d.DecidedBy, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Body = json.RawMessage(raw)
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Service) ListTurns(ctx context.Context, sessionID string) ([]TurnRequest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,session_id,member_id,trigger_message_id,status,selected_candidate_kind,selected_candidate_id,response_message_id,budget_reservation_id,error_text,round_number,research_phase,attempt_count,retry_after,created_at,updated_at,completed_at FROM team_turn_requests WHERE session_id=? ORDER BY round_number,created_at,id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TurnRequest
	for rows.Next() {
		var t TurnRequest
		var trig, kind, cid, msg, bres, et sql.NullString
		var retry, done sql.NullInt64
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.SessionID, &t.MemberID, &trig, &t.Status, &kind, &cid, &msg, &bres, &et, &t.RoundNumber, &t.ResearchPhase, &t.AttemptCount, &retry, &t.CreatedAt, &t.UpdatedAt, &done); err != nil {
			return nil, err
		}
		if trig.Valid {
			t.TriggerMessageID = &trig.String
		}
		if kind.Valid {
			t.SelectedCandidateKind = &kind.String
		}
		if cid.Valid {
			t.SelectedCandidateID = &cid.String
		}
		if msg.Valid {
			t.ResponseMessageID = &msg.String
		}
		if bres.Valid {
			t.BudgetReservationID = &bres.String
		}
		if et.Valid {
			t.ErrorText = &et.String
		}
		if retry.Valid {
			t.RetryAfter = &retry.Int64
		}
		if done.Valid {
			t.CompletedAt = &done.Int64
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
