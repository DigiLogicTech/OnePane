package agentprofile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

var (
	ErrInvalid          = errors.New("invalid agent profile command")
	ErrNotFound         = errors.New("agent profile not found")
	ErrBuiltinReadOnly  = errors.New("built-in agent profiles are read-only")
	ErrRevisionConflict = errors.New("agent profile revision conflict")
)

type Profile struct {
	ID             string          `json:"id"`
	WorkspaceID    *string         `json:"workspace_id,omitempty"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	InstructionsMD string          `json:"instructions_md"`
	DefaultRole    string          `json:"default_role"`
	CapabilityID   string          `json:"capability_id"`
	ProtocolLevel  string          `json:"protocol_level"`
	SourceKind     string          `json:"source_kind"`
	Status         string          `json:"status"`
	Metadata       json.RawMessage `json:"metadata"`
	Revision       int64           `json:"revision"`
	CreatedBy      *string         `json:"created_by,omitempty"`
	CreatedAt      int64           `json:"created_at"`
	UpdatedAt      int64           `json:"updated_at"`
}

type CreateCommand struct {
	WorkspaceID    string
	Name           string
	Description    string
	InstructionsMD string
	DefaultRole    string
	CapabilityID   string
	ProtocolLevel  string
	Metadata       json.RawMessage
	CreatedBy      string
}

type UpdateCommand struct {
	ID               string
	WorkspaceID      string
	ExpectedRevision int64
	Name             string
	Description      string
	InstructionsMD   string
	DefaultRole      string
	CapabilityID     string
	ProtocolLevel    string
	Metadata         json.RawMessage
	ActorPrincipalID string
}

type Service struct {
	db     *sql.DB
	tx     storage.Transactor
	events event.Store
	ids    id.Generator
	clock  clock.Clock
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{db: db, tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk}
}

func canonical(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return json.RawMessage(`{}`)
	}
	out, _ := json.Marshal(obj)
	return out
}

func validProtocol(v string) bool {
	switch v {
	case "L0", "L1", "L2", "L3":
		return true
	}
	return false
}

func scan(row interface{ Scan(...any) error }) (Profile, error) {
	var p Profile
	var ws, created sql.NullString
	var meta string
	if err := row.Scan(&p.ID, &ws, &p.Name, &p.Description, &p.InstructionsMD, &p.DefaultRole, &p.CapabilityID, &p.ProtocolLevel, &p.SourceKind, &p.Status, &meta, &p.Revision, &created, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, err
	}
	if ws.Valid { p.WorkspaceID = &ws.String }
	if created.Valid { p.CreatedBy = &created.String }
	p.Metadata = json.RawMessage(meta)
	return p, nil
}

const columns = `id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at`

func (s *Service) Get(ctx context.Context, idv string) (Profile, error) {
	idv = strings.TrimSpace(idv)
	if strings.EqualFold(idv, "onepane-default") { idv = "agent.md" }
	if idv == "" { return Profile{}, ErrInvalid }
	return scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM agent_profiles WHERE id=?`, idv))
}

func (s *Service) Resolve(ctx context.Context, workspaceID, idv string) (Profile, error) {
	idv = strings.TrimSpace(idv)
	if idv == "" || strings.EqualFold(idv, "onepane-default") { idv = "agent.md" }
	var p Profile
	var err error
	if strings.TrimSpace(workspaceID) != "" {
		p, err = scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM agent_profiles WHERE id=? AND status='active' AND (workspace_id=? OR workspace_id IS NULL) ORDER BY CASE WHEN workspace_id=? THEN 0 ELSE 1 END LIMIT 1`, idv, workspaceID, workspaceID))
	} else {
		p, err = scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM agent_profiles WHERE id=? AND status='active' AND workspace_id IS NULL LIMIT 1`, idv))
	}
	return p, err
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM agent_profiles WHERE status='active' AND (workspace_id IS NULL OR workspace_id=?) ORDER BY source_kind,name,id`, strings.TrimSpace(workspaceID))
	if err != nil { return nil, err }
	defer rows.Close()
	out := []Profile{}
	for rows.Next() {
		p, err := scan(rows)
		if err != nil { return nil, err }
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) Create(ctx context.Context, c CreateCommand) (Profile, error) {
	c.WorkspaceID, c.Name, c.InstructionsMD, c.CreatedBy = strings.TrimSpace(c.WorkspaceID), strings.TrimSpace(c.Name), strings.TrimSpace(c.InstructionsMD), strings.TrimSpace(c.CreatedBy)
	if c.WorkspaceID == "" || c.Name == "" || c.InstructionsMD == "" || c.CreatedBy == "" { return Profile{}, ErrInvalid }
	if c.DefaultRole == "" { c.DefaultRole = "general" }
	if c.CapabilityID == "" { c.CapabilityID = "inference.general" }
	if c.ProtocolLevel == "" { c.ProtocolLevel = "L1" }
	if !validProtocol(c.ProtocolLevel) { return Profile{}, ErrInvalid }
	pid, _ := s.ids.New("aprofile")
	eid, _ := s.ids.New("evt")
	now := s.clock.UnixMilli()
	meta := canonical(c.Metadata)
	p := Profile{ID: pid, WorkspaceID: &c.WorkspaceID, Name: c.Name, Description: strings.TrimSpace(c.Description), InstructionsMD: c.InstructionsMD, DefaultRole: c.DefaultRole, CapabilityID: c.CapabilityID, ProtocolLevel: c.ProtocolLevel, SourceKind: "custom", Status: "active", Metadata: meta, Revision: 1, CreatedBy: &c.CreatedBy, CreatedAt: now, UpdatedAt: now}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_profiles(id,workspace_id,name,description,instructions_md,default_role,capability_id,protocol_level,source_kind,status,metadata_json,revision,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'active',?,1,?,?,?)`, p.ID, p.WorkspaceID, p.Name, p.Description, p.InstructionsMD, p.DefaultRole, p.CapabilityID, p.ProtocolLevel, p.SourceKind, string(p.Metadata), p.CreatedBy, now, now); err != nil { return err }
		payload, _ := json.Marshal(map[string]any{"profile_id": p.ID, "name": p.Name, "source_kind": p.SourceKind})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &c.WorkspaceID, Type: "agent_profile.created", AggregateType: "agent_profile", AggregateID: p.ID, ActorPrincipalID: &c.CreatedBy, Payload: payload, OccurredAt: now})
	})
	if err != nil { return Profile{}, err }
	return s.Get(ctx, p.ID)
}

func (s *Service) Update(ctx context.Context, c UpdateCommand) (Profile, error) {
	c.ID, c.WorkspaceID, c.ActorPrincipalID = strings.TrimSpace(c.ID), strings.TrimSpace(c.WorkspaceID), strings.TrimSpace(c.ActorPrincipalID)
	if c.ID == "" || c.WorkspaceID == "" || c.ActorPrincipalID == "" || c.ExpectedRevision < 1 || strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.InstructionsMD) == "" { return Profile{}, ErrInvalid }
	if c.DefaultRole == "" { c.DefaultRole = "general" }
	if c.CapabilityID == "" { c.CapabilityID = "inference.general" }
	if c.ProtocolLevel == "" { c.ProtocolLevel = "L1" }
	if !validProtocol(c.ProtocolLevel) { return Profile{}, ErrInvalid }
	now := s.clock.UnixMilli()
	eid, _ := s.ids.New("evt")
	meta := canonical(c.Metadata)
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var source string
		var rev int64
		var ws sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT source_kind,revision,workspace_id FROM agent_profiles WHERE id=?`, c.ID).Scan(&source, &rev, &ws); err != nil { return err }
		if source == "builtin" { return ErrBuiltinReadOnly }
		if !ws.Valid || ws.String != c.WorkspaceID { return ErrInvalid }
		if rev != c.ExpectedRevision { return ErrRevisionConflict }
		res, err := tx.ExecContext(ctx, `UPDATE agent_profiles SET name=?,description=?,instructions_md=?,default_role=?,capability_id=?,protocol_level=?,metadata_json=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, strings.TrimSpace(c.Name), strings.TrimSpace(c.Description), strings.TrimSpace(c.InstructionsMD), c.DefaultRole, c.CapabilityID, c.ProtocolLevel, string(meta), now, c.ID, c.ExpectedRevision)
		if err != nil { return err }
		n, _ := res.RowsAffected(); if n != 1 { return ErrRevisionConflict }
		payload, _ := json.Marshal(map[string]any{"profile_id": c.ID, "revision": c.ExpectedRevision + 1})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &c.WorkspaceID, Type: "agent_profile.updated", AggregateType: "agent_profile", AggregateID: c.ID, ActorPrincipalID: &c.ActorPrincipalID, Payload: payload, OccurredAt: now})
	})
	if err != nil { return Profile{}, err }
	return s.Get(ctx, c.ID)
}

func (s *Service) Archive(ctx context.Context, idv, workspaceID, actor string) (Profile, error) {
	idv, workspaceID, actor = strings.TrimSpace(idv), strings.TrimSpace(workspaceID), strings.TrimSpace(actor)
	if idv == "" || workspaceID == "" || actor == "" { return Profile{}, ErrInvalid }
	now := s.clock.UnixMilli()
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var source string
		var ws sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT source_kind,workspace_id FROM agent_profiles WHERE id=?`, idv).Scan(&source, &ws); err != nil { return err }
		if source == "builtin" { return ErrBuiltinReadOnly }
		if !ws.Valid || ws.String != workspaceID { return ErrInvalid }
		_, err := tx.ExecContext(ctx, `UPDATE agent_profiles SET status='archived',revision=revision+1,updated_at=? WHERE id=?`, now, idv)
		return err
	})
	if err != nil { return Profile{}, err }
	return s.Get(ctx, idv)
}

func (p Profile) PromptBlock() string {
	return fmt.Sprintf("Agent Profile: %s\nRole: %s\nInstructions:\n%s\n\nProfile instructions affect reasoning only. They do not grant tools, secrets, filesystem, network, node, approval, or other authority.", p.Name, p.DefaultRole, p.InstructionsMD)
}
