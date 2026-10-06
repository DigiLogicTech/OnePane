package botruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type credentialValidator interface {
	ValidateBotCredentialRef(context.Context, string, string) error
}

type Service struct {
	db          *sql.DB
	tx          storage.Transactor
	clock       clock.Clock
	ids         id.Generator
	events      event.Store
	secrets     SecretResolver
	credentials credentialValidator
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, secrets SecretResolver) *Service {
	s := &Service{db: db, tx: tx, clock: clk, ids: id.Generator{}, events: event.Store{}, secrets: secrets}
	if v, ok := secrets.(credentialValidator); ok {
		s.credentials = v
	}
	return s
}

func (s *Service) humanMember(ctx context.Context, ws, p string) bool {
	var typ, pst, mst string
	err := s.db.QueryRowContext(ctx, `SELECT p.principal_type,p.status,m.status FROM principals p JOIN workspace_memberships m ON m.principal_id=p.id WHERE p.id=? AND m.workspace_id=?`, p, ws).Scan(&typ, &pst, &mst)
	return err == nil && typ == "human" && pst == "active" && mst == "active"
}
func (s *Service) emit(ctx context.Context, tx storage.Tx, ws, typ, agg, idv string, actor string, payload any) error {
	eid, _ := s.ids.New("evt")
	raw, _ := json.Marshal(payload)
	return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &ws, Type: typ, AggregateType: agg, AggregateID: idv, ActorPrincipalID: &actor, Payload: raw, OccurredAt: s.clock.UnixMilli()})
}
func canon(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(v)
	return b, nil
}

func (s *Service) CreateConnection(ctx context.Context, c CreateConnectionCommand) (Connection, error) {
	p, ok := PresetByID(c.PresetID)
	if !ok || !s.humanMember(ctx, c.WorkspaceID, c.CreatedBy) {
		return Connection{}, ErrInvalid
	}
	name := strings.TrimSpace(c.DisplayName)
	if name == "" {
		name = p.DisplayName
	}
	cfg, err := canon(c.Config)
	if err != nil {
		return Connection{}, ErrInvalid
	}
	out := Connection{WorkspaceID: c.WorkspaceID, PresetID: p.ID, DisplayName: name, SourceKind: p.SourceKind, AccessMode: p.AccessMode, Config: cfg, Status: "active", CreatedBy: c.CreatedBy, Revision: 1}
	if p.AccessMode == "harness_api" {
		if p.ID != "hermes" {
			return Connection{}, fmt.Errorf("%w: unsupported native harness bot preset", ErrInvalid)
		}
		base := strings.TrimSpace(c.BaseURL)
		if base == "" {
			base = "http://127.0.0.1:8642"
		}
		if _, err := safeBase(base); err != nil {
			return Connection{}, err
		}
		if c.CredentialRef == nil || s.credentials == nil {
			return Connection{}, fmt.Errorf("%w: Hermes API_SERVER_KEY credential required", ErrInvalid)
		}
		if err := s.credentials.ValidateBotCredentialRef(ctx, *c.CredentialRef, p.ID); err != nil {
			return Connection{}, err
		}
		base = strings.TrimRight(base, "/")
		out.BaseURL = &base
		out.CredentialRef = c.CredentialRef
	} else if p.AccessMode == "relay_api" {
		if strings.TrimSpace(c.BaseURL) == "" {
			return Connection{}, fmt.Errorf("%w: relay base_url required", ErrInvalid)
		}
		if _, err := safeBase(c.BaseURL); err != nil {
			return Connection{}, err
		}
		base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
		out.BaseURL = &base
		if c.CredentialRef != nil {
			if s.credentials == nil {
				return Connection{}, fmt.Errorf("%w: bot credential validator unavailable", ErrInvalid)
			}
			if err := s.credentials.ValidateBotCredentialRef(ctx, *c.CredentialRef, p.ID); err != nil {
				return Connection{}, err
			}
			out.CredentialRef = c.CredentialRef
		}
	} else if p.AccessMode == "hosted_surface" {
		if c.CredentialRef != nil || strings.TrimSpace(c.BaseURL) != "" {
			return Connection{}, fmt.Errorf("%w: hosted bot connections do not proxy credentials or base URLs", ErrInvalid)
		}
	}
	out.ID, _ = s.ids.New("botconn")
	now := s.clock.UnixMilli()
	out.CreatedAt = now
	out.UpdatedAt = now
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO bot_connections(id,workspace_id,preset_id,display_name,source_kind,access_mode,source_ref,base_url,credential_ref,config_json,status,created_by,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?, ?,1,?,?)`, out.ID, out.WorkspaceID, out.PresetID, out.DisplayName, out.SourceKind, out.AccessMode, out.SourceRef, out.BaseURL, out.CredentialRef, string(out.Config), out.Status, out.CreatedBy, now, now)
		if e != nil {
			return e
		}
		return s.emit(ctx, tx, out.WorkspaceID, "bot.connection_created", "bot_connection", out.ID, c.CreatedBy, map[string]any{"preset_id": out.PresetID, "access_mode": out.AccessMode})
	})
	return out, err
}

func scanConnection(row interface{ Scan(...any) error }) (Connection, error) {
	var c Connection
	var src, base, cred sql.NullString
	var cfg string
	err := row.Scan(&c.ID, &c.WorkspaceID, &c.PresetID, &c.DisplayName, &c.SourceKind, &c.AccessMode, &src, &base, &cred, &cfg, &c.Status, &c.CreatedBy, &c.Revision, &c.CreatedAt, &c.UpdatedAt)
	if src.Valid {
		c.SourceRef = &src.String
	}
	if base.Valid {
		c.BaseURL = &base.String
	}
	if cred.Valid {
		c.CredentialRef = &cred.String
	}
	c.Config = json.RawMessage(cfg)
	return c, err
}
func (s *Service) Connection(ctx context.Context, idv string) (Connection, error) {
	return scanConnection(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,preset_id,display_name,source_kind,access_mode,source_ref,base_url,credential_ref,config_json,status,created_by,revision,created_at,updated_at FROM bot_connections WHERE id=?`, idv))
}
func (s *Service) ListConnections(ctx context.Context, ws string) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,preset_id,display_name,source_kind,access_mode,source_ref,base_url,credential_ref,config_json,status,created_by,revision,created_at,updated_at FROM bot_connections WHERE workspace_id=? ORDER BY display_name,id`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		c, e := scanConnection(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func validLaunch(p Preset, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("%w: launch_url must be absolute HTTPS", ErrInvalid)
	}
	if p.ID == "custom-hosted-bot" {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range p.AllowedLaunchHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return nil
		}
	}
	return fmt.Errorf("%w: launch host %s is not allowed for %s", ErrInvalid, host, p.ID)
}

func (s *Service) CreateBot(ctx context.Context, c CreateBotCommand) (Bot, error) {
	conn, err := s.Connection(ctx, c.ConnectionID)
	if err != nil {
		return Bot{}, err
	}
	if conn.WorkspaceID != c.WorkspaceID || conn.Status != "active" || !s.humanMember(ctx, c.WorkspaceID, c.CreatedBy) {
		return Bot{}, ErrInvalid
	}
	p, _ := PresetByID(conn.PresetID)
	remote := strings.TrimSpace(c.RemoteBotID)
	name := strings.TrimSpace(c.DisplayName)
	if remote == "" || name == "" {
		return Bot{}, ErrInvalid
	}
	if p.ID == "hermes" && !safeProfile.MatchString(remote) {
		return Bot{}, fmt.Errorf("%w: invalid Hermes profile name", ErrInvalid)
	}
	var launch *string
	if conn.AccessMode == "hosted_surface" {
		if c.LaunchURL == nil || strings.TrimSpace(*c.LaunchURL) == "" {
			return Bot{}, fmt.Errorf("%w: hosted bot launch_url required", ErrInvalid)
		}
		if err := validLaunch(p, *c.LaunchURL); err != nil {
			return Bot{}, err
		}
		v := strings.TrimSpace(*c.LaunchURL)
		launch = &v
	} else if c.LaunchURL != nil {
		return Bot{}, fmt.Errorf("%w: launch_url is only valid for hosted bots", ErrInvalid)
	}
	caps, err := canon(c.Capabilities)
	if err != nil {
		return Bot{}, ErrInvalid
	}
	if string(caps) == "{}" {
		caps, _ = json.Marshal(map[string]any{"in_app_chat": p.InAppChat, "persistent_sessions": p.PersistentSessions, "native_tools": p.NativeTools, "native_memory": p.NativeMemory, "group_chat": p.GroupChat, "routines": p.Routines, "authority": "native_runtime"})
	}
	meta, err := canon(c.Metadata)
	if err != nil {
		return Bot{}, ErrInvalid
	}
	now := s.clock.UnixMilli()
	bid, _ := s.ids.New("bot")
	b := Bot{ID: bid, WorkspaceID: c.WorkspaceID, ConnectionID: conn.ID, RemoteBotID: remote, DisplayName: name, Description: strings.TrimSpace(c.Description), AvatarURL: c.AvatarURL, LaunchURL: launch, Capabilities: caps, Metadata: meta, Status: "active", CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO bot_profiles(id,workspace_id,connection_id,remote_bot_id,display_name,description,avatar_url,launch_url,capabilities_json,metadata_json,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?, ?,?)`, b.ID, b.WorkspaceID, b.ConnectionID, b.RemoteBotID, b.DisplayName, b.Description, b.AvatarURL, b.LaunchURL, string(b.Capabilities), string(b.Metadata), b.Status, now, now)
		if e != nil {
			return e
		}
		return s.emit(ctx, tx, b.WorkspaceID, "bot.profile_registered", "bot", b.ID, c.CreatedBy, map[string]any{"remote_bot_id": b.RemoteBotID, "preset_id": conn.PresetID})
	})
	return b, err
}
func scanBot(row interface{ Scan(...any) error }) (Bot, error) {
	var b Bot
	var av, launch sql.NullString
	var caps, meta string
	err := row.Scan(&b.ID, &b.WorkspaceID, &b.ConnectionID, &b.RemoteBotID, &b.DisplayName, &b.Description, &av, &launch, &caps, &meta, &b.Status, &b.CreatedAt, &b.UpdatedAt)
	if av.Valid {
		b.AvatarURL = &av.String
	}
	if launch.Valid {
		b.LaunchURL = &launch.String
	}
	b.Capabilities = json.RawMessage(caps)
	b.Metadata = json.RawMessage(meta)
	return b, err
}
func (s *Service) Bot(ctx context.Context, idv string) (Bot, error) {
	return scanBot(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,connection_id,remote_bot_id,display_name,description,avatar_url,launch_url,capabilities_json,metadata_json,status,created_at,updated_at FROM bot_profiles WHERE id=?`, idv))
}
func (s *Service) ListBots(ctx context.Context, ws string) ([]Bot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,connection_id,remote_bot_id,display_name,description,avatar_url,launch_url,capabilities_json,metadata_json,status,created_at,updated_at FROM bot_profiles WHERE workspace_id=? ORDER BY status,display_name,id`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bot
	for rows.Next() {
		b, e := scanBot(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Service) adapter(c Connection) (Adapter, error) {
	switch c.AccessMode {
	case "harness_api":
		if c.PresetID != "hermes" {
			return nil, ErrInvalid
		}
		return HTTPAdapter{Mode: "hermes", Secrets: s.secrets}, nil
	case "relay_api":
		return HTTPAdapter{Mode: "relay", Secrets: s.secrets}, nil
	default:
		return nil, ErrHostedSurface
	}
}

func (s *Service) CreateSession(ctx context.Context, c CreateSessionCommand) (Session, error) {
	b, err := s.Bot(ctx, c.BotID)
	if err != nil {
		return Session{}, err
	}
	if b.Status != "active" || !s.humanMember(ctx, b.WorkspaceID, c.CreatedBy) {
		return Session{}, ErrInvalid
	}
	conn, err := s.Connection(ctx, b.ConnectionID)
	if err != nil {
		return Session{}, err
	}
	kind := "scratch"
	if c.Canonical {
		kind = "canonical"
		var existingID string
		if e := s.db.QueryRowContext(ctx, `SELECT id FROM bot_sessions WHERE bot_id=? AND created_by=? AND session_kind='canonical' AND status='active'`, b.ID, c.CreatedBy).Scan(&existingID); e == nil {
			return s.Session(ctx, existingID)
		}
	}
	sid, _ := s.ids.New("botsession")
	var remote *string
	if conn.AccessMode != "hosted_surface" {
		a, e := s.adapter(conn)
		if e != nil {
			return Session{}, e
		}
		rid, e := a.EnsureSession(ctx, conn, b, sid, c.Title, c.Canonical)
		if e != nil {
			return Session{}, e
		}
		remote = &rid
	}
	meta := json.RawMessage(`{"authority":"native_runtime","onepane_autonomous":false}`)
	title := strings.TrimSpace(c.Title)
	if title == "" && c.Canonical {
		title = "Bot Chat"
	}
	now := s.clock.UnixMilli()
	out := Session{ID: sid, WorkspaceID: b.WorkspaceID, BotID: b.ID, RemoteSessionID: remote, Title: title, SessionKind: kind, Status: "active", CreatedBy: c.CreatedBy, Metadata: meta, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO bot_sessions(id,workspace_id,bot_id,remote_session_id,title,session_kind,status,created_by,metadata_json,created_at,updated_at,last_message_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,NULL)`, out.ID, out.WorkspaceID, out.BotID, out.RemoteSessionID, out.Title, out.SessionKind, out.Status, out.CreatedBy, string(out.Metadata), now, now)
		if e != nil {
			return e
		}
		return s.emit(ctx, tx, out.WorkspaceID, "bot.session_created", "bot_session", out.ID, c.CreatedBy, map[string]any{"bot_id": out.BotID, "session_kind": out.SessionKind})
	})
	return out, err
}
func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var v Session
	var remote sql.NullString
	var meta string
	var last sql.NullInt64
	err := row.Scan(&v.ID, &v.WorkspaceID, &v.BotID, &remote, &v.Title, &v.SessionKind, &v.Status, &v.CreatedBy, &meta, &v.CreatedAt, &v.UpdatedAt, &last)
	if remote.Valid {
		v.RemoteSessionID = &remote.String
	}
	if last.Valid {
		v.LastMessageAt = &last.Int64
	}
	v.Metadata = json.RawMessage(meta)
	return v, err
}
func (s *Service) Session(ctx context.Context, idv string) (Session, error) {
	return scanSession(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,bot_id,remote_session_id,title,session_kind,status,created_by,metadata_json,created_at,updated_at,last_message_at FROM bot_sessions WHERE id=?`, idv))
}
func (s *Service) ListSessions(ctx context.Context, botID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,bot_id,remote_session_id,title,session_kind,status,created_by,metadata_json,created_at,updated_at,last_message_at FROM bot_sessions WHERE bot_id=? ORDER BY updated_at DESC,id`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		v, e := scanSession(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func scanMessage(row interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var raw string
	var rid sql.NullString
	err := row.Scan(&m.ID, &m.WorkspaceID, &m.SessionID, &m.Role, &raw, &rid, &m.DeliveryStatus, &m.CreatedAt)
	m.Content = json.RawMessage(raw)
	if rid.Valid {
		m.RemoteMessageID = &rid.String
	}
	return m, err
}
func (s *Service) ListMessages(ctx context.Context, sid string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,session_id,role,content_json,remote_message_id,delivery_status,created_at FROM bot_messages WHERE session_id=? ORDER BY created_at,id LIMIT ?`, sid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, e := scanMessage(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) SendMessage(ctx context.Context, c SendMessageCommand) (SendResult, error) {
	ss, err := s.Session(ctx, c.SessionID)
	if err != nil {
		return SendResult{}, err
	}
	if ss.Status != "active" || !s.humanMember(ctx, ss.WorkspaceID, c.CreatedBy) {
		return SendResult{}, ErrInvalid
	}
	b, err := s.Bot(ctx, ss.BotID)
	if err != nil {
		return SendResult{}, err
	}
	conn, err := s.Connection(ctx, b.ConnectionID)
	if err != nil {
		return SendResult{}, err
	}
	if conn.AccessMode == "hosted_surface" {
		return SendResult{LaunchURL: b.LaunchURL}, ErrHostedSurface
	}
	text := strings.TrimSpace(c.Text)
	if text == "" || len(text) > 200000 {
		return SendResult{}, ErrInvalid
	}
	key := strings.TrimSpace(c.IdempotencyKey)
	if key != "" {
		if prev, ok := s.turnByKey(ctx, ss.WorkspaceID, key); ok {
			return s.resultForTurn(ctx, prev)
		}
	}
	tid, _ := s.ids.New("botturn")
	if key == "" {
		key = tid
	}
	mid, _ := s.ids.New("botmsg")
	now := s.clock.UnixMilli()
	raw, _ := json.Marshal(map[string]any{"text": text})
	user := Message{ID: mid, WorkspaceID: ss.WorkspaceID, SessionID: ss.ID, Role: "user", Content: raw, DeliveryStatus: "pending", CreatedAt: now}
	turn := Turn{ID: tid, WorkspaceID: ss.WorkspaceID, SessionID: ss.ID, UserMessageID: mid, Status: "pending", IdempotencyKey: key, CreatedAt: now, UpdatedAt: now}
	if err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO bot_messages(id,workspace_id,session_id,role,content_json,remote_message_id,delivery_status,created_at) VALUES(?,?,?,?,?,NULL,'pending',?)`, user.ID, user.WorkspaceID, user.SessionID, user.Role, string(user.Content), now); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO bot_turns(id,workspace_id,session_id,user_message_id,response_message_id,status,idempotency_key,remote_run_id,receipt_json,error_text,created_at,updated_at,completed_at) VALUES(?,?,?,?,NULL,'pending',?,NULL,NULL,NULL,?,?,NULL)`, turn.ID, turn.WorkspaceID, turn.SessionID, turn.UserMessageID, turn.IdempotencyKey, now, now); e != nil {
			return e
		}
		return s.emit(ctx, tx, ss.WorkspaceID, "bot.turn_requested", "bot_turn", turn.ID, c.CreatedBy, map[string]any{"session_id": ss.ID, "bot_id": b.ID})
	}); err != nil {
		return SendResult{}, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE bot_turns SET status='sending',updated_at=? WHERE id=? AND status='pending'`, s.clock.UnixMilli(), turn.ID)
	if err != nil {
		return SendResult{}, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		if err != nil {
			return SendResult{}, err
		}
		return SendResult{}, fmt.Errorf("bot turn %s was not pending", turn.ID)
	}
	if ss.RemoteSessionID == nil || strings.TrimSpace(*ss.RemoteSessionID) == "" {
		return s.failTurn(ctx, turn, user, c.CreatedBy, "remote session missing", false)
	}
	a, err := s.adapter(conn)
	if err != nil {
		return s.failTurn(ctx, turn, user, c.CreatedBy, err.Error(), false)
	}
	reply, sendErr := a.Send(ctx, conn, b, *ss.RemoteSessionID, text, key)
	if sendErr != nil {
		return s.failTurn(ctx, turn, user, c.CreatedBy, sendErr.Error(), isUnknown(sendErr))
	}
	aid, _ := s.ids.New("botmsg")
	done := s.clock.UnixMilli()
	araw, _ := json.Marshal(map[string]any{"text": reply.Text})
	assistant := Message{ID: aid, WorkspaceID: ss.WorkspaceID, SessionID: ss.ID, Role: "assistant", Content: araw, DeliveryStatus: "received", CreatedAt: done}
	if reply.RemoteMessageID != "" {
		assistant.RemoteMessageID = &reply.RemoteMessageID
	}
	turn.ResponseMessageID = &aid
	turn.Status = "succeeded"
	turn.UpdatedAt = done
	turn.CompletedAt = &done
	if reply.RemoteRunID != "" {
		turn.RemoteRunID = &reply.RemoteRunID
	}
	turn.Receipt = reply.Receipt
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.ExecContext(ctx, `UPDATE bot_messages SET delivery_status='sent' WHERE id=?`, user.ID); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO bot_messages(id,workspace_id,session_id,role,content_json,remote_message_id,delivery_status,created_at) VALUES(?,?,?,?,?,?, 'received',?)`, assistant.ID, assistant.WorkspaceID, assistant.SessionID, assistant.Role, string(assistant.Content), assistant.RemoteMessageID, done); e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, `UPDATE bot_turns SET response_message_id=?,status='succeeded',remote_run_id=?,receipt_json=?,updated_at=?,completed_at=? WHERE id=? AND status='sending'`, assistant.ID, turn.RemoteRunID, string(turn.Receipt), done, done, turn.ID)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return fmt.Errorf("bot turn %s lost sending state", turn.ID)
		}
		if _, e := tx.ExecContext(ctx, `UPDATE bot_sessions SET updated_at=?,last_message_at=? WHERE id=?`, done, done, ss.ID); e != nil {
			return e
		}
		return s.emit(ctx, tx, ss.WorkspaceID, "bot.turn_succeeded", "bot_turn", turn.ID, c.CreatedBy, map[string]any{"session_id": ss.ID, "response_message_id": assistant.ID})
	})
	if err != nil {
		return SendResult{}, err
	}
	user.DeliveryStatus = "sent"
	return SendResult{Turn: turn, UserMessage: user, AssistantMessage: &assistant}, nil
}
func (s *Service) failTurn(ctx context.Context, t Turn, u Message, actor, msg string, unknown bool) (SendResult, error) {
	status := "failed"
	delivery := "failed"
	ret := fmt.Errorf("bot turn failed: %s", msg)
	if unknown {
		status = "unknown"
		delivery = "unknown"
		ret = fmt.Errorf("%w: %s", ErrUnknownOutcome, msg)
	}
	now := s.clock.UnixMilli()
	t.Status = status
	t.UpdatedAt = now
	t.CompletedAt = &now
	t.ErrorText = &msg
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.ExecContext(ctx, `UPDATE bot_messages SET delivery_status=? WHERE id=?`, delivery, u.ID); e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, `UPDATE bot_turns SET status=?,error_text=?,updated_at=?,completed_at=? WHERE id=? AND status='sending'`, status, msg, now, now, t.ID)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return fmt.Errorf("bot turn %s lost sending state", t.ID)
		}
		return s.emit(ctx, tx, t.WorkspaceID, "bot.turn_"+status, "bot_turn", t.ID, actor, map[string]any{"session_id": t.SessionID, "error": msg})
	})
	if err != nil {
		return SendResult{}, err
	}
	u.DeliveryStatus = delivery
	return SendResult{Turn: t, UserMessage: u}, ret
}
func (s *Service) turnByKey(ctx context.Context, ws, key string) (Turn, bool) {
	var t Turn
	var resp, run, receipt, errtxt sql.NullString
	var done sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,session_id,user_message_id,response_message_id,status,idempotency_key,remote_run_id,receipt_json,error_text,created_at,updated_at,completed_at FROM bot_turns WHERE workspace_id=? AND idempotency_key=?`, ws, key).Scan(&t.ID, &t.WorkspaceID, &t.SessionID, &t.UserMessageID, &resp, &t.Status, &t.IdempotencyKey, &run, &receipt, &errtxt, &t.CreatedAt, &t.UpdatedAt, &done)
	if err != nil {
		return Turn{}, false
	}
	if resp.Valid {
		t.ResponseMessageID = &resp.String
	}
	if run.Valid {
		t.RemoteRunID = &run.String
	}
	if receipt.Valid {
		t.Receipt = json.RawMessage(receipt.String)
	}
	if errtxt.Valid {
		t.ErrorText = &errtxt.String
	}
	if done.Valid {
		t.CompletedAt = &done.Int64
	}
	return t, true
}
func (s *Service) resultForTurn(ctx context.Context, t Turn) (SendResult, error) {
	u, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,session_id,role,content_json,remote_message_id,delivery_status,created_at FROM bot_messages WHERE id=?`, t.UserMessageID))
	if err != nil {
		return SendResult{}, err
	}
	var a *Message
	if t.ResponseMessageID != nil {
		m, e := scanMessage(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,session_id,role,content_json,remote_message_id,delivery_status,created_at FROM bot_messages WHERE id=?`, *t.ResponseMessageID))
		if e == nil {
			a = &m
		}
	}
	return SendResult{Turn: t, UserMessage: u, AssistantMessage: a}, nil
}
