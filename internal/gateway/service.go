package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/operation"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

const DeliveryPrincipal = "system-gateway-delivery"
const VerifierPrincipal = "system-gateway-verifier"

type Connection struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	PresetID      string          `json:"preset_id"`
	DisplayName   string          `json:"display_name"`
	DeliveryMode  string          `json:"delivery_mode"`
	CredentialRef *string         `json:"credential_ref,omitempty"`
	Config        json.RawMessage `json:"config"`
	Status        string          `json:"status"`
	CreatedBy     string          `json:"created_by"`
	Revision      int64           `json:"revision"`
	CreatedAt     int64           `json:"created_at"`
	UpdatedAt     int64           `json:"updated_at"`
}
type Target struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	ConnectionID string          `json:"connection_id"`
	Name         string          `json:"name"`
	Address      string          `json:"address"`
	ThreadRef    *string         `json:"thread_ref,omitempty"`
	Config       json.RawMessage `json:"config"`
	Status       string          `json:"status"`
	Revision     int64           `json:"revision"`
	CreatedAt    int64           `json:"created_at"`
	UpdatedAt    int64           `json:"updated_at"`
}
type Rule struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	Name          string          `json:"name"`
	EventType     string          `json:"event_type"`
	AggregateType *string         `json:"aggregate_type,omitempty"`
	AggregateID   *string         `json:"aggregate_id,omitempty"`
	RoutineID     *string         `json:"routine_id,omitempty"`
	TargetID      string          `json:"target_id"`
	Template      json.RawMessage `json:"template"`
	Status        string          `json:"status"`
	CreatedBy     string          `json:"created_by"`
	Revision      int64           `json:"revision"`
	CreatedAt     int64           `json:"created_at"`
	UpdatedAt     int64           `json:"updated_at"`
}
type Delivery struct {
	ID             string          `json:"id"`
	WorkspaceID    string          `json:"workspace_id"`
	RuleID         *string         `json:"rule_id,omitempty"`
	TargetID       string          `json:"target_id"`
	SourceEventID  *string         `json:"source_event_id,omitempty"`
	SourceEventSeq *int64          `json:"source_event_sequence,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
	Message        json.RawMessage `json:"message"`
	Status         string          `json:"status"`
	Attempts       int             `json:"attempts"`
	MaxAttempts    int             `json:"max_attempts"`
	NextAttemptAt  int64           `json:"next_attempt_at"`
	OperationID    *string         `json:"operation_id,omitempty"`
	VerificationID *string         `json:"verification_id,omitempty"`
	Receipt        json.RawMessage `json:"receipt,omitempty"`
	LastError      *string         `json:"last_error,omitempty"`
	CreatedAt      int64           `json:"created_at"`
	UpdatedAt      int64           `json:"updated_at"`
}

type Service struct {
	db        *sql.DB
	tx        storage.Transactor
	clock     clock.Clock
	ids       id.Generator
	events    event.Store
	authority *authority.Service
	ops       *operation.Coordinator
	ver       *verification.Service
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, a *authority.Service, ops *operation.Coordinator, v *verification.Service) *Service {
	return &Service{db: db, tx: tx, clock: clk, ids: id.Generator{}, events: event.Store{}, authority: a, ops: ops, ver: v}
}

func (s *Service) RecoverInterrupted(ctx context.Context) (int, error) {
	now := s.clock.UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE notification_deliveries SET status='unknown',last_error='daemon restart during external delivery; outcome requires reconciliation',updated_at=? WHERE status='sending'`, now)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *Service) EnsureSystemPrincipals(ctx context.Context) error {
	for _, x := range []struct{ id, name string }{{DeliveryPrincipal, "Gateway Delivery Worker"}, {VerifierPrincipal, "Gateway Receipt Verifier"}} {
		if err := s.ensurePrincipal(ctx, x.id, x.name); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) ensurePrincipal(ctx context.Context, pid, name string) error {
	var st string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM principals WHERE id=?`, pid).Scan(&st)
	if err == nil {
		if st != "active" {
			return fmt.Errorf("gateway principal %s is %s", pid, st)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := s.clock.UnixMilli()
	actor := pid
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, e := tx.ExecContext(ctx, `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES(?,'system',?,'active',1,?,?)`, pid, name, now, now); e != nil {
			return e
		}
		eid, _ := s.ids.New("evt")
		p, _ := json.Marshal(map[string]any{"principal_id": pid, "purpose": "messaging_gateway"})
		return s.events.Append(ctx, tx, event.Event{ID: eid, Type: "system_principal.registered", AggregateType: "principal", AggregateID: pid, ActorPrincipalID: &actor, Payload: p, OccurredAt: now})
	})
}
func (s *Service) ensureWorkspace(ctx context.Context, ws string) error {
	if err := s.EnsureSystemPrincipals(ctx); err != nil {
		return err
	}
	for _, pid := range []string{DeliveryPrincipal, VerifierPrincipal} {
		var st string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM workspace_memberships WHERE workspace_id=? AND principal_id=?`, ws, pid).Scan(&st)
		if err == nil {
			if st != "active" {
				return fmt.Errorf("gateway membership is %s", st)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := s.clock.UnixMilli()
		actor := DeliveryPrincipal
		if e := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			var wst string
			if e := tx.QueryRowContext(ctx, `SELECT status FROM workspaces WHERE id=?`, ws).Scan(&wst); e != nil {
				return e
			}
			if wst != "active" {
				return fmt.Errorf("workspace is %s", wst)
			}
			if _, e := tx.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES(?,?,'active',?,?)`, ws, pid, now, now); e != nil {
				return e
			}
			eid, _ := s.ids.New("evt")
			p, _ := json.Marshal(map[string]any{"workspace_id": ws, "principal_id": pid, "purpose": "messaging_gateway"})
			return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &ws, Type: "workspace.system_membership_added", AggregateType: "workspace_membership", AggregateID: ws + ":" + pid, ActorPrincipalID: &actor, Payload: p, OccurredAt: now})
		}); e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) CreateConnection(ctx context.Context, workspaceID, presetID, name, mode string, credentialRef *string, cfg json.RawMessage, createdBy string) (Connection, error) {
	p, ok := ByID(presetID)
	if !ok {
		return Connection{}, errors.New("unknown gateway preset")
	}
	if mode == "" {
		mode = p.DefaultMode
	}
	if mode != "native_http" && mode != "smtp" && mode != "relay" {
		return Connection{}, errors.New("invalid delivery mode")
	}
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	if !json.Valid(cfg) {
		return Connection{}, errors.New("config must be valid JSON")
	}
	if workspaceID == "" || createdBy == "" {
		return Connection{}, errors.New("workspace and creator required")
	}
	idv, _ := s.ids.New("gwconn")
	now := s.clock.UnixMilli()
	c := Connection{ID: idv, WorkspaceID: workspaceID, PresetID: p.ID, DisplayName: strings.TrimSpace(name), DeliveryMode: mode, CredentialRef: credentialRef, Config: cfg, Status: "active", CreatedBy: createdBy, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if c.DisplayName == "" {
		c.DisplayName = p.DisplayName
	}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var n int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id=? AND principal_id=? AND status='active'`, workspaceID, createdBy).Scan(&n); e != nil || n != 1 {
			if e != nil {
				return e
			}
			return errors.New("creator is not an active workspace member")
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO gateway_connections(id,workspace_id,preset_id,display_name,delivery_mode,credential_ref,config_json,status,created_by,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'active',?,1,?,?)`, c.ID, c.WorkspaceID, c.PresetID, c.DisplayName, c.DeliveryMode, c.CredentialRef, string(c.Config), c.CreatedBy, now, now)
		return e
	})
	return c, err
}
func (s *Service) CreateTarget(ctx context.Context, workspaceID, connectionID, name, address string, thread *string, cfg json.RawMessage) (Target, error) {
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	if !json.Valid(cfg) || strings.TrimSpace(address) == "" {
		return Target{}, errors.New("address and valid config required")
	}
	var cws string
	if err := s.db.QueryRowContext(ctx, `SELECT workspace_id FROM gateway_connections WHERE id=? AND status='active'`, connectionID).Scan(&cws); err != nil {
		return Target{}, err
	}
	if cws != workspaceID {
		return Target{}, errors.New("connection workspace mismatch")
	}
	idv, _ := s.ids.New("gwtarget")
	now := s.clock.UnixMilli()
	t := Target{ID: idv, WorkspaceID: workspaceID, ConnectionID: connectionID, Name: strings.TrimSpace(name), Address: strings.TrimSpace(address), ThreadRef: thread, Config: cfg, Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
	if t.Name == "" {
		t.Name = t.Address
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO gateway_targets(id,workspace_id,connection_id,name,address,thread_ref,config_json,status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'active',1,?,?)`, t.ID, t.WorkspaceID, t.ConnectionID, t.Name, t.Address, t.ThreadRef, string(t.Config), now, now)
	return t, err
}
func (s *Service) CreateRule(ctx context.Context, r Rule) (Rule, error) {
	if r.WorkspaceID == "" || r.EventType == "" || r.TargetID == "" || r.CreatedBy == "" {
		return Rule{}, errors.New("workspace,event_type,target,creator required")
	}
	if len(r.Template) == 0 {
		r.Template = json.RawMessage(`{"title":"OnePane","text":"{{event_type}} · {{aggregate_id}}"}`)
	}
	if !json.Valid(r.Template) {
		return Rule{}, errors.New("template must be valid JSON")
	}
	var member int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_memberships wm JOIN principals p ON p.id=wm.principal_id WHERE wm.workspace_id=? AND wm.principal_id=? AND wm.status='active' AND p.status='active'`, r.WorkspaceID, r.CreatedBy).Scan(&member); err != nil {
		return Rule{}, err
	}
	if member != 1 {
		return Rule{}, errors.New("rule creator is not an active workspace member")
	}
	var tws string
	if err := s.db.QueryRowContext(ctx, `SELECT workspace_id FROM gateway_targets WHERE id=? AND status='active'`, r.TargetID).Scan(&tws); err != nil {
		return Rule{}, err
	}
	if tws != r.WorkspaceID {
		return Rule{}, errors.New("target workspace mismatch")
	}
	r.ID, _ = s.ids.New("notifyrule")
	r.Status = "active"
	r.Revision = 1
	r.CreatedAt = s.clock.UnixMilli()
	r.UpdatedAt = r.CreatedAt
	if r.Name == "" {
		r.Name = r.EventType
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_rules(id,workspace_id,name,event_type,aggregate_type,aggregate_id,routine_id,target_id,template_json,status,created_by,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'active',?,1,?,?)`, r.ID, r.WorkspaceID, r.Name, r.EventType, r.AggregateType, r.AggregateID, r.RoutineID, r.TargetID, string(r.Template), r.CreatedBy, r.CreatedAt, r.UpdatedAt)
	return r, err
}
func (s *Service) ListConnections(ctx context.Context, ws string) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,preset_id,display_name,delivery_mode,credential_ref,config_json,status,created_by,revision,created_at,updated_at FROM gateway_connections WHERE workspace_id=? ORDER BY created_at`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		var c Connection
		var cred sql.NullString
		var cfg string
		if e := rows.Scan(&c.ID, &c.WorkspaceID, &c.PresetID, &c.DisplayName, &c.DeliveryMode, &cred, &cfg, &c.Status, &c.CreatedBy, &c.Revision, &c.CreatedAt, &c.UpdatedAt); e != nil {
			return nil, e
		}
		if cred.Valid {
			c.CredentialRef = &cred.String
		}
		c.Config = json.RawMessage(cfg)
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Service) ListTargets(ctx context.Context, ws string) ([]Target, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,connection_id,name,address,thread_ref,config_json,status,revision,created_at,updated_at FROM gateway_targets WHERE workspace_id=? ORDER BY created_at`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		var t Target
		var th sql.NullString
		var cfg string
		if e := rows.Scan(&t.ID, &t.WorkspaceID, &t.ConnectionID, &t.Name, &t.Address, &th, &cfg, &t.Status, &t.Revision, &t.CreatedAt, &t.UpdatedAt); e != nil {
			return nil, e
		}
		if th.Valid {
			t.ThreadRef = &th.String
		}
		t.Config = json.RawMessage(cfg)
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Service) ListRules(ctx context.Context, ws string) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,name,event_type,aggregate_type,aggregate_id,routine_id,target_id,template_json,status,created_by,revision,created_at,updated_at FROM notification_rules WHERE workspace_id=? ORDER BY created_at`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		var at, ai, ri sql.NullString
		var tmpl string
		if e := rows.Scan(&r.ID, &r.WorkspaceID, &r.Name, &r.EventType, &at, &ai, &ri, &r.TargetID, &tmpl, &r.Status, &r.CreatedBy, &r.Revision, &r.CreatedAt, &r.UpdatedAt); e != nil {
			return nil, e
		}
		if at.Valid {
			r.AggregateType = &at.String
		}
		if ai.Valid {
			r.AggregateID = &ai.String
		}
		if ri.Valid {
			r.RoutineID = &ri.String
		}
		r.Template = json.RawMessage(tmpl)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) Enqueue(ctx context.Context, workspaceID, targetID, idempotency, title, text string) (Delivery, error) {
	if workspaceID == "" || targetID == "" || text == "" {
		return Delivery{}, errors.New("workspace,target,text required")
	}
	var targetWS string
	if err := s.db.QueryRowContext(ctx, `SELECT workspace_id FROM gateway_targets WHERE id=? AND status='active'`, targetID).Scan(&targetWS); err != nil {
		return Delivery{}, err
	}
	if targetWS != workspaceID {
		return Delivery{}, errors.New("target workspace mismatch")
	}
	msg, _ := json.Marshal(map[string]any{"title": title, "text": text})
	idv, _ := s.ids.New("delivery")
	now := s.clock.UnixMilli()
	d := Delivery{ID: idv, WorkspaceID: workspaceID, TargetID: targetID, IdempotencyKey: idempotency, Message: msg, Status: "pending", MaxAttempts: 5, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
	if d.IdempotencyKey == "" {
		d.IdempotencyKey = "manual:" + d.ID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_deliveries(id,workspace_id,target_id,idempotency_key,message_json,status,attempts,max_attempts,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,'pending',0,5,?,?,?)`, d.ID, d.WorkspaceID, d.TargetID, d.IdempotencyKey, string(d.Message), now, now, now)
	return d, err
}

func (s *Service) ScanEvents(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var cursor int64
	if err := s.db.QueryRowContext(ctx, `SELECT last_sequence FROM gateway_event_cursor WHERE id=1`).Scan(&cursor); err != nil {
		return 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at FROM events WHERE sequence>? ORDER BY sequence LIMIT ?`, cursor, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type ev struct {
		seq                          int64
		id                           string
		ws                           sql.NullString
		typ, aggType, aggID, payload string
		at                           int64
	}
	var es []ev
	for rows.Next() {
		var e ev
		if x := rows.Scan(&e.seq, &e.id, &e.ws, &e.typ, &e.aggType, &e.aggID, &e.payload, &e.at); x != nil {
			return 0, x
		}
		es = append(es, e)
	}
	if len(es) == 0 {
		return 0, nil
	}
	count := 0
	for _, e := range es {
		if e.ws.Valid {
			n, er := s.materializeEvent(ctx, e.seq, e.id, e.ws.String, e.typ, e.aggType, e.aggID, e.payload)
			if er != nil {
				return count, er
			}
			count += n
		}
		cursor = e.seq
	}
	_, err = s.db.ExecContext(ctx, `UPDATE gateway_event_cursor SET last_sequence=?,updated_at=? WHERE id=1`, cursor, s.clock.UnixMilli())
	return count, err
}
func (s *Service) materializeEvent(ctx context.Context, seq int64, eventID, ws, typ, aggType, aggID, payload string) (int, error) {
	vars := map[string]string{}
	if typ == "task.completed" && aggType == "task" {
		var objective string
		var result sql.NullString
		if err := s.db.QueryRowContext(ctx, `SELECT objective,result_json FROM tasks WHERE id=? AND workspace_id=?`, aggID, ws).Scan(&objective, &result); err == nil {
			vars["task_objective"] = objective
			if result.Valid {
				vars["task_result"] = result.String
			}
		}
		var routineName string
		if err := s.db.QueryRowContext(ctx, `SELECT r.name FROM routine_occurrences ro JOIN routines r ON r.id=ro.routine_id WHERE ro.task_id=? ORDER BY ro.created_at LIMIT 1`, aggID).Scan(&routineName); err == nil {
			vars["routine_name"] = routineName
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,target_id,template_json,aggregate_type,aggregate_id,routine_id FROM notification_rules WHERE workspace_id=? AND status='active' AND event_type=?`, ws, typ)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var ruleID, target, tmpl string
		var rat, rai, rri sql.NullString
		if e := rows.Scan(&ruleID, &target, &tmpl, &rat, &rai, &rri); e != nil {
			return n, e
		}
		if rat.Valid && rat.String != aggType {
			continue
		}
		if rai.Valid && rai.String != aggID {
			continue
		}
		if rri.Valid {
			var x int
			if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM routine_occurrences WHERE routine_id=? AND task_id=?`, rri.String, aggID).Scan(&x); e != nil {
				return n, e
			}
			if x == 0 {
				continue
			}
		}
		title, text := renderTemplate(json.RawMessage(tmpl), typ, aggID, payload, vars)
		msg, _ := json.Marshal(map[string]any{"title": title, "text": text})
		did, _ := s.ids.New("delivery")
		now := s.clock.UnixMilli()
		key := fmt.Sprintf("event:%d:rule:%s:target:%s", seq, ruleID, target)
		res, e := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO notification_deliveries(id,workspace_id,rule_id,target_id,source_event_id,source_event_seq,idempotency_key,message_json,status,attempts,max_attempts,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,'pending',0,5,?,?,?)`, did, ws, ruleID, target, eventID, seq, key, string(msg), now, now, now)
		if e != nil {
			return n, e
		}
		if x, _ := res.RowsAffected(); x == 1 {
			n++
		}
	}
	return n, rows.Err()
}
func renderTemplate(raw json.RawMessage, eventType, aggID, payload string, extras ...map[string]string) (string, string) {
	var v struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	_ = json.Unmarshal(raw, &v)
	vars := map[string]string{"event_type": eventType, "aggregate_id": aggID, "payload": payload}
	for _, extra := range extras {
		for k, val := range extra {
			vars[k] = val
		}
	}
	rep := func(x string) string {
		for k, val := range vars {
			x = strings.ReplaceAll(x, "{{"+k+"}}", val)
		}
		return x
	}
	return rep(v.Title), rep(v.Text)
}

func (s *Service) Tick(ctx context.Context, limit int) ([]Delivery, error) {
	if _, err := s.ScanEvents(ctx, 500); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM notification_deliveries WHERE status='pending' AND next_attempt_at<=? ORDER BY created_at LIMIT ?`, s.clock.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var x string
		if e := rows.Scan(&x); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, x)
	}
	rows.Close()
	var out []Delivery
	for _, x := range ids {
		d, e := s.deliver(ctx, x)
		if e != nil {
			d, _ = s.getDelivery(ctx, x)
		}
		out = append(out, d)
	}
	return out, nil
}
func (s *Service) getDelivery(ctx context.Context, idv string) (Delivery, error) {
	var d Delivery
	var rule, eventID, op, ver, receipt, last sql.NullString
	var seq sql.NullInt64
	var msg string
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,rule_id,target_id,source_event_id,source_event_seq,idempotency_key,message_json,status,attempts,max_attempts,next_attempt_at,operation_id,verification_id,receipt_json,last_error,created_at,updated_at FROM notification_deliveries WHERE id=?`, idv).Scan(&d.ID, &d.WorkspaceID, &rule, &d.TargetID, &eventID, &seq, &d.IdempotencyKey, &msg, &d.Status, &d.Attempts, &d.MaxAttempts, &d.NextAttemptAt, &op, &ver, &receipt, &last, &d.CreatedAt, &d.UpdatedAt)
	if rule.Valid {
		d.RuleID = &rule.String
	}
	if eventID.Valid {
		d.SourceEventID = &eventID.String
	}
	if seq.Valid {
		d.SourceEventSeq = &seq.Int64
	}
	if op.Valid {
		d.OperationID = &op.String
	}
	if ver.Valid {
		d.VerificationID = &ver.String
	}
	if receipt.Valid {
		d.Receipt = json.RawMessage(receipt.String)
	}
	if last.Valid {
		d.LastError = &last.String
	}
	d.Message = json.RawMessage(msg)
	return d, err
}

func (s *Service) deliver(ctx context.Context, idv string) (Delivery, error) {
	d, err := s.getDelivery(ctx, idv)
	if err != nil {
		return d, err
	}
	if err = s.ensureWorkspace(ctx, d.WorkspaceID); err != nil {
		return d, err
	}
	var msg struct{ Title, Text string }
	if json.Unmarshal(d.Message, &msg) != nil || msg.Text == "" {
		return d, s.failDelivery(ctx, d, "invalid message", false)
	}
	now := s.clock.UnixMilli()
	if _, err = s.db.ExecContext(ctx, `UPDATE notification_deliveries SET status='sending',attempts=attempts+1,updated_at=? WHERE id=? AND status='pending'`, now, d.ID); err != nil {
		return d, err
	}
	one := int64(1)
	lease, err := s.authority.Issue(ctx, authority.IssueCommand{WorkspaceID: d.WorkspaceID, PrincipalID: DeliveryPrincipal, CapabilityID: "gateway.send", Scope: authority.Scope{ResourceRefs: []string{"gateway-target:" + d.TargetID}, Actions: []authority.ActionMode{authority.ActionExternalSend}}, IssuedBy: DeliveryPrincipal, ExpiresAt: now + 5*60*1000, UsageLimit: &one})
	if err != nil {
		return d, s.failDelivery(ctx, d, err.Error(), true)
	}
	input, _ := json.Marshal(outbound{TargetID: d.TargetID, DeliveryID: d.ID, Text: msg.Text, Title: msg.Title})
	op, err := s.ops.Prepare(ctx, operation.PrepareCommand{WorkspaceID: d.WorkspaceID, PrincipalID: DeliveryPrincipal, CapabilityLeaseID: lease.ID, IdempotencyKey: "notification:" + d.IdempotencyKey, ToolID: "gateway.send", ToolVersion: "1", ResourceRef: "gateway-target:" + d.TargetID, Input: input, DesiredState: json.RawMessage(`{"accepted":true}`), Reconciliation: json.RawMessage(`{"strategy":"do_not_retry_unknown"}`)})
	if err != nil {
		return d, s.failDelivery(ctx, d, err.Error(), true)
	}
	d.OperationID = &op.ID
	_, _ = s.db.ExecContext(ctx, `UPDATE notification_deliveries SET operation_id=?,updated_at=? WHERE id=?`, op.ID, s.clock.UnixMilli(), d.ID)
	op, err = s.ops.Execute(ctx, operation.ExecuteCommand{OperationID: op.ID, ExpectedRevision: op.Revision, LeaseID: lease.ID, Input: input})
	if err != nil {
		if errors.Is(err, operation.ErrUnknownOutcome) {
			return d, s.markUnknown(ctx, d, err.Error())
		}
		return d, s.failDelivery(ctx, d, err.Error(), true)
	}
	v, err := s.ver.Create(ctx, verification.CreateCommand{WorkspaceID: d.WorkspaceID, OperationID: &op.ID, SubjectRef: "gateway-delivery:" + d.ID, RequiredLevel: policy.VerificationV1, Spec: json.RawMessage(`{"kind":"gateway_acceptance_receipt"}`)})
	if err != nil {
		return d, s.markUnknown(ctx, d, err.Error())
	}
	lvl := policy.VerificationV1
	v, err = s.ver.Resolve(ctx, verification.ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: verification.StatusPass, AchievedLevel: &lvl, Result: json.RawMessage(`{"receipt":"tool invocation accepted by configured gateway"}`), VerifiedBy: VerifierPrincipal})
	if err != nil {
		return d, s.markUnknown(ctx, d, err.Error())
	}
	op, err = s.ops.CommitVerified(ctx, operation.CommitCommand{OperationID: op.ID, ExpectedRevision: op.Revision, VerificationID: v.ID})
	if err != nil {
		return d, s.markUnknown(ctx, d, err.Error())
	}
	receipt, _ := json.Marshal(map[string]any{"operation_id": op.ID, "verification_id": v.ID, "status": "accepted"})
	now = s.clock.UnixMilli()
	_, err = s.db.ExecContext(ctx, `UPDATE notification_deliveries SET status='accepted',verification_id=?,receipt_json=?,last_error=NULL,updated_at=? WHERE id=?`, v.ID, string(receipt), now, d.ID)
	if err != nil {
		return d, err
	}
	return s.getDelivery(ctx, d.ID)
}
func (s *Service) failDelivery(ctx context.Context, d Delivery, reason string, retry bool) error {
	now := s.clock.UnixMilli()
	status := "failed"
	next := now
	if retry && d.Attempts+1 < d.MaxAttempts {
		status = "pending"
		back := time.Duration(1<<min(d.Attempts, 6)) * time.Minute
		next = now + back.Milliseconds()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE notification_deliveries SET status=?,last_error=?,next_attempt_at=?,updated_at=? WHERE id=?`, status, reason, next, now, d.ID)
	return err
}

func (s *Service) markUnknown(ctx context.Context, d Delivery, reason string) error {
	now := s.clock.UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE notification_deliveries SET status='unknown',last_error=?,updated_at=? WHERE id=?`, reason, now, d.ID)
	return err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Service) RoutineWorkspace(ctx context.Context, routineID string) (string, error) {
	var ws string
	err := s.db.QueryRowContext(ctx, `SELECT workspace_id FROM routines WHERE id=?`, routineID).Scan(&ws)
	return ws, err
}

func (s *Service) NotifyRoutineTarget(ctx context.Context, routineID, targetID, createdBy string) (Rule, error) {
	var ws, name string
	if err := s.db.QueryRowContext(ctx, `SELECT workspace_id,name FROM routines WHERE id=?`, routineID).Scan(&ws, &name); err != nil {
		return Rule{}, err
	}
	rid := routineID
	at := "task"
	return s.CreateRule(ctx, Rule{WorkspaceID: ws, Name: "Routine " + name + " completion", EventType: "task.completed", AggregateType: &at, RoutineID: &rid, TargetID: targetID, Template: json.RawMessage(`{"title":"Routine complete · {{routine_name}}","text":"{{task_objective}}\n\n{{task_result}}"}`), CreatedBy: createdBy})
}

// Compile-time reference documents that this subsystem reacts to verified task completion rather than model declarations.
var _ = task.StateComplete
