package approval

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type Status string

const (
	Pending     Status = "pending"
	Approved    Status = "approved"
	Denied      Status = "denied"
	Expired     Status = "expired"
	Invalidated Status = "invalidated"
	Revoked     Status = "revoked"
	Consumed    Status = "consumed"
)

type Approval struct {
	ID, WorkspaceID string
	OperationID     *string
	RequestedBy     string
	ApprovedBy      *string
	Status          Status
	OperationHash   string
	RequirementJSON json.RawMessage
	PolicyRevision  int64
	CreatedAt       int64
	ExpiresAt       *int64
	ResolvedAt      *int64
	Revision        int64
}
type RequestCommand struct {
	OperationID, RequestedBy string
	TTLMillis                int64
	ActorPrincipalID         *string
}
type ResolveCommand struct {
	ApprovalID       string
	ExpectedRevision int64
	PrincipalID      string
	Approve          bool
	Reason           string
	ActorPrincipalID *string
}

var (
	ErrInvalid      = errors.New("invalid approval command")
	ErrIneligible   = errors.New("principal is not eligible to approve")
	ErrSelfApproval = errors.New("requester cannot approve own operation")
	ErrConflict     = errors.New("approval revision/state conflict")
	ErrBinding      = errors.New("approval no longer matches operation")
)

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

func operationHash(fields ...string) string {
	h := sha256.New()
	for _, v := range fields {
		_, _ = h.Write([]byte(v))
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
func readOperationBinding(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, idv string) (workspace, principal, state string, policyRev int64, required policy.ApprovalLevel, hash string, err error) {
	var task, attempt, comp sql.NullString
	var desired, pre, recon, inputHash, cap, resource, tool, toolv, adapter, adapterv string
	var req sql.NullString
	err = q.QueryRowContext(ctx, `SELECT workspace_id,task_id,attempt_id,principal_id,compensates_operation_id,state,capability_id,resource_ref,tool_id,tool_version,adapter_id,adapter_version,desired_state_json,precondition_json,reconciliation_json,policy_revision,input_hash,required_approval FROM operations WHERE id=?`, idv).Scan(&workspace, &task, &attempt, &principal, &comp, &state, &cap, &resource, &tool, &toolv, &adapter, &adapterv, &desired, &pre, &recon, &policyRev, &inputHash, &req)
	if err != nil {
		return
	}
	if req.Valid {
		required = policy.ApprovalLevel(req.String)
	} else {
		required = policy.ApprovalNone
	}
	hash = operationHash(workspace, task.String, attempt.String, principal, comp.String, cap, resource, tool, toolv, adapter, adapterv, desired, pre, recon, fmt.Sprint(policyRev), inputHash, string(required))
	return
}
func (s *Service) Request(ctx context.Context, cmd RequestCommand) (Approval, error) {
	if cmd.OperationID == "" || cmd.RequestedBy == "" {
		return Approval{}, ErrInvalid
	}
	ws, principal, state, prev, required, hash, err := readOperationBinding(ctx, s.db, cmd.OperationID)
	if err != nil {
		return Approval{}, err
	}
	if principal != cmd.RequestedBy {
		return Approval{}, ErrInvalid
	}
	if state != "authorized" || required == policy.ApprovalNone {
		return Approval{}, ErrInvalid
	}
	ttl := cmd.TTLMillis
	if ttl == 0 {
		ttl = 15 * 60 * 1000
	}
	if ttl < 60_000 || ttl > 24*60*60*1000 {
		return Approval{}, ErrInvalid
	}
	idv, _ := s.ids.New("approval")
	now := s.clock.UnixMilli()
	exp := now + ttl
	reqJSON, _ := json.Marshal(map[string]any{"level": required})
	a := Approval{ID: idv, WorkspaceID: ws, OperationID: &cmd.OperationID, RequestedBy: cmd.RequestedBy, Status: Pending, OperationHash: hash, RequirementJSON: reqJSON, PolicyRevision: prev, CreatedAt: now, ExpiresAt: &exp, Revision: 1}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO approvals(id,workspace_id,operation_id,requested_by,status,operation_hash,requirement_json,policy_revision,created_at,expires_at,revision) VALUES(?,?,?,?,?,?,?,?,?,?,1)`, a.ID, a.WorkspaceID, a.OperationID, a.RequestedBy, a.Status, a.OperationHash, string(a.RequirementJSON), a.PolicyRevision, a.CreatedAt, a.ExpiresAt)
		if err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"approval_id": a.ID, "operation_id": cmd.OperationID, "required": required, "expires_at": exp})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &ws, Type: "approval.requested", AggregateType: "approval", AggregateID: a.ID, ActorPrincipalID: cmd.ActorPrincipalID, Payload: payload, OccurredAt: now})
	})
	return a, err
}
func (s *Service) Get(ctx context.Context, idv string) (Approval, error) {
	return scan(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,operation_id,requested_by,approved_by,status,operation_hash,requirement_json,policy_revision,created_at,expires_at,resolved_at,revision FROM approvals WHERE id=?`, idv))
}

type scanner interface{ Scan(...any) error }

func scan(r scanner) (Approval, error) {
	var a Approval
	var op, by sql.NullString
	var exp, res sql.NullInt64
	var raw string
	if err := r.Scan(&a.ID, &a.WorkspaceID, &op, &a.RequestedBy, &by, &a.Status, &a.OperationHash, &raw, &a.PolicyRevision, &a.CreatedAt, &exp, &res, &a.Revision); err != nil {
		return a, err
	}
	if op.Valid {
		a.OperationID = &op.String
	}
	if by.Valid {
		a.ApprovedBy = &by.String
	}
	if exp.Valid {
		a.ExpiresAt = &exp.Int64
	}
	if res.Valid {
		a.ResolvedAt = &res.Int64
	}
	a.RequirementJSON = json.RawMessage(raw)
	return a, nil
}
func (s *Service) eligible(ctx context.Context, principal, workspace string, level policy.ApprovalLevel) bool {
	var typ, status string
	if s.db.QueryRowContext(ctx, `SELECT principal_type,status FROM principals WHERE id=?`, principal).Scan(&typ, &status) != nil || typ != "human" || status != "active" {
		return false
	}
	var member string
	if s.db.QueryRowContext(ctx, `SELECT status FROM workspace_memberships WHERE workspace_id=? AND principal_id=?`, workspace, principal).Scan(&member) != nil || member != "active" {
		return false
	}
	rows, err := s.db.QueryContext(ctx, `SELECT lower(r.name) FROM principal_roles pr JOIN roles r ON r.id=pr.role_id WHERE pr.principal_id=? AND (pr.workspace_id=? OR pr.workspace_id IS NULL) AND r.role_class='human'`, principal, workspace)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		if level == policy.ApprovalApprover && (n == "approver" || n == "admin") {
			return true
		}
		if level == policy.ApprovalAdmin && n == "admin" {
			return true
		}
	}
	return false
}
func (s *Service) Resolve(ctx context.Context, cmd ResolveCommand) (Approval, error) {
	a, err := s.Get(ctx, cmd.ApprovalID)
	if err != nil {
		return a, err
	}
	if a.Revision != cmd.ExpectedRevision || a.Status != Pending {
		return a, ErrConflict
	}
	if a.RequestedBy == cmd.PrincipalID {
		return a, ErrSelfApproval
	}
	if a.ExpiresAt != nil && s.clock.UnixMilli() >= *a.ExpiresAt {
		return a, ErrConflict
	}
	if a.OperationID == nil {
		return a, ErrInvalid
	}
	_, _, _, prev, required, hash, err := readOperationBinding(ctx, s.db, *a.OperationID)
	if err != nil {
		return a, err
	}
	if prev != a.PolicyRevision || hash != a.OperationHash {
		return a, ErrBinding
	}
	if !s.eligible(ctx, cmd.PrincipalID, a.WorkspaceID, required) {
		return a, ErrIneligible
	}
	next := Denied
	if cmd.Approve {
		next = Approved
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE approvals SET status=?,approved_by=?,resolved_at=?,revision=revision+1 WHERE id=? AND revision=? AND status='pending'`, next, cmd.PrincipalID, now, a.ID, a.Revision)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrConflict
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"approval_id": a.ID, "operation_id": a.OperationID, "status": next, "reason": strings.TrimSpace(cmd.Reason)})
		actor := cmd.ActorPrincipalID
		if actor == nil {
			actor = &cmd.PrincipalID
		}
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &a.WorkspaceID, Type: "approval." + string(next), AggregateType: "approval", AggregateID: a.ID, ActorPrincipalID: actor, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return a, err
	}
	return s.Get(ctx, a.ID)
}

// Consume validates the exact operation binding and atomically makes an approved decision single-use.
func (s *Service) Consume(ctx context.Context, approvalID, operationID string) error {
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error { return s.ConsumeTx(ctx, tx, approvalID, operationID) })
}

// ConsumeTx lets OperationCoordinator consume the approval in the same transaction
// that acquires the exclusive ResourceLease.
func (s *Service) ConsumeTx(ctx context.Context, tx storage.Tx, approvalID, operationID string) error {
	var a Approval
	var op, by sql.NullString
	var exp, resAt sql.NullInt64
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT id,workspace_id,operation_id,requested_by,approved_by,status,operation_hash,requirement_json,policy_revision,created_at,expires_at,resolved_at,revision FROM approvals WHERE id=?`, approvalID).Scan(&a.ID, &a.WorkspaceID, &op, &a.RequestedBy, &by, &a.Status, &a.OperationHash, &raw, &a.PolicyRevision, &a.CreatedAt, &exp, &resAt, &a.Revision); err != nil {
		return err
	}
	if op.Valid {
		a.OperationID = &op.String
	}
	if by.Valid {
		a.ApprovedBy = &by.String
	}
	if exp.Valid {
		a.ExpiresAt = &exp.Int64
	}
	if a.Status != Approved || a.OperationID == nil || *a.OperationID != operationID {
		return ErrConflict
	}
	if a.ExpiresAt != nil && s.clock.UnixMilli() >= *a.ExpiresAt {
		return ErrConflict
	}
	_, _, _, prev, _, hash, err := readOperationBinding(ctx, tx, operationID)
	if err != nil {
		return err
	}
	if prev != a.PolicyRevision || hash != a.OperationHash {
		return ErrBinding
	}
	now := s.clock.UnixMilli()
	upd, err := tx.ExecContext(ctx, `UPDATE approvals SET status='consumed',revision=revision+1,resolved_at=? WHERE id=? AND revision=? AND status='approved'`, now, a.ID, a.Revision)
	if err != nil {
		return err
	}
	n, _ := upd.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	eid, _ := s.ids.New("evt")
	payload, _ := json.Marshal(map[string]any{"approval_id": a.ID, "operation_id": operationID})
	return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &a.WorkspaceID, Type: "approval.consumed", AggregateType: "approval", AggregateID: a.ID, Payload: payload, OccurredAt: now})
}
