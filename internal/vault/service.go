package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

const algorithm = "AES-256-GCM-envelope-v1"

type SecretRecord struct {
	ID           string          `json:"id"`
	WorkspaceID  *string         `json:"workspace_id,omitempty"`
	LogicalName  string          `json:"logical_name"`
	ProviderType string          `json:"provider_type"`
	Version      int64           `json:"version"`
	Status       string          `json:"status"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    int64           `json:"created_at"`
	RetiredAt    *int64          `json:"retired_at,omitempty"`
}
type CreateCommand struct {
	WorkspaceID               *string
	LogicalName, ProviderType string
	Value                     []byte
	Metadata                  json.RawMessage
	CreatedBy                 string
}
type Service struct {
	db     *sql.DB
	tx     storage.Transactor
	events event.Store
	ids    id.Generator
	clock  clock.Clock
	kek    []byte
}

var (
	ErrInvalid   = errors.New("invalid secret command")
	ErrNotFound  = errors.New("secret not found")
	ErrRevoked   = errors.New("secret revoked")
	ErrIntegrity = errors.New("secret integrity failure")
)

func EnsureMasterKey(dataDir string) ([]byte, error) {
	dir := filepath.Join(dataDir, "vault")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "master.key")
	if st, err := os.Stat(path); err == nil {
		if runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("vault master key permissions must be 0600 or stricter")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(b) != 32 {
			return nil, fmt.Errorf("vault master key has invalid length")
		}
		return b, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return b, nil
}
func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, kek []byte) (*Service, error) {
	if len(kek) != 32 {
		return nil, errors.New("vault KEK must be 32 bytes")
	}
	return &Service{db: db, tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk, kek: append([]byte(nil), kek...)}, nil
}
func canonical(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func aad(r SecretRecord) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s", r.ID, deref(r.WorkspaceID), r.LogicalName, r.Version, r.ProviderType))
}
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func newGCM(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}
func seal(key, plain, aad []byte) (nonce, ciphertext []byte, err error) {
	g, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, g.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, g.Seal(nil, nonce, plain, aad), nil
}
func open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	g, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != g.NonceSize() {
		return nil, ErrIntegrity
	}
	p, err := g.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrIntegrity
	}
	return p, nil
}
func (s *Service) Create(ctx context.Context, cmd CreateCommand) (SecretRecord, error) {
	if strings.TrimSpace(cmd.LogicalName) == "" || strings.TrimSpace(cmd.ProviderType) == "" || strings.TrimSpace(cmd.CreatedBy) == "" || len(cmd.Value) == 0 {
		return SecretRecord{}, ErrInvalid
	}
	if len(cmd.Value) > 1<<20 {
		return SecretRecord{}, fmt.Errorf("%w: secret exceeds 1MiB", ErrInvalid)
	}
	meta, err := canonical(cmd.Metadata)
	if err != nil {
		return SecretRecord{}, ErrInvalid
	}
	idv, _ := s.ids.New("secret")
	now := s.clock.UnixMilli()
	var out SecretRecord
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if cmd.WorkspaceID != nil {
			var st string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM workspaces WHERE id=?`, *cmd.WorkspaceID).Scan(&st); err != nil {
				return err
			}
			if st != "active" {
				return ErrInvalid
			}
		}
		var version int64 = 1
		var prevID string
		var prevVer int64
		err := tx.QueryRowContext(ctx, `SELECT id,version FROM secret_records WHERE workspace_id IS ? AND logical_name=? AND status='active' ORDER BY version DESC LIMIT 1`, cmd.WorkspaceID, cmd.LogicalName).Scan(&prevID, &prevVer)
		if err == nil {
			version = prevVer + 1
		} else if err != sql.ErrNoRows {
			return err
		}
		out = SecretRecord{ID: idv, WorkspaceID: cmd.WorkspaceID, LogicalName: strings.TrimSpace(cmd.LogicalName), ProviderType: strings.TrimSpace(cmd.ProviderType), Version: version, Status: "active", Metadata: meta, CreatedBy: cmd.CreatedBy, CreatedAt: now}
		dek := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, dek); err != nil {
			return err
		}
		nonce, ct, err := seal(dek, cmd.Value, aad(out))
		if err != nil {
			return err
		}
		wrapNonce, wrapped, err := seal(s.kek, dek, []byte("wrap:"+out.ID))
		for i := range dek {
			dek[i] = 0
		}
		if err != nil {
			return err
		}
		wrappedBlob := append(wrapNonce, wrapped...)
		if prevID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE secret_records SET status='retired',retired_at=? WHERE id=? AND status='active'`, now, prevID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO secret_records(id,workspace_id,logical_name,provider_type,version,status,metadata_json,created_by,created_at) VALUES(?,?,?,?,?,'active',?,?,?)`, out.ID, out.WorkspaceID, out.LogicalName, out.ProviderType, out.Version, string(out.Metadata), out.CreatedBy, out.CreatedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO builtin_vault_items(secret_record_id,ciphertext,wrapped_dek,nonce,algorithm,key_version,created_at) VALUES(?,?,?,?,?,1,?)`, out.ID, ct, wrappedBlob, nonce, algorithm, now); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"secret_record_id": out.ID, "logical_name": out.LogicalName, "version": out.Version, "provider_type": out.ProviderType})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: out.WorkspaceID, Type: "secret.created", AggregateType: "secret_record", AggregateID: out.ID, ActorPrincipalID: &cmd.CreatedBy, Payload: payload, OccurredAt: now})
	})
	return out, err
}
func (s *Service) record(ctx context.Context, idv string) (SecretRecord, error) {
	var r SecretRecord
	var ws sql.NullString
	var retired sql.NullInt64
	var meta string
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,logical_name,provider_type,version,status,metadata_json,created_by,created_at,retired_at FROM secret_records WHERE id=?`, idv).Scan(&r.ID, &ws, &r.LogicalName, &r.ProviderType, &r.Version, &r.Status, &meta, &r.CreatedBy, &r.CreatedAt, &retired)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if ws.Valid {
		r.WorkspaceID = &ws.String
	}
	if retired.Valid {
		r.RetiredAt = &retired.Int64
	}
	r.Metadata = json.RawMessage(meta)
	return r, nil
}
func (s *Service) Resolve(ctx context.Context, ref string) (string, error) {
	if !strings.HasPrefix(ref, "vault:") {
		return "", fmt.Errorf("unsupported secret_ref %q", ref)
	}
	idv := strings.TrimSpace(strings.TrimPrefix(ref, "vault:"))
	r, err := s.record(ctx, idv)
	if err != nil {
		return "", err
	}
	if r.Status == "revoked" {
		return "", ErrRevoked
	}
	if r.Status != "active" && r.Status != "retired" {
		return "", ErrNotFound
	}
	var ct, wrapped, nonce []byte
	var alg string
	var kv int
	if err := s.db.QueryRowContext(ctx, `SELECT ciphertext,wrapped_dek,nonce,algorithm,key_version FROM builtin_vault_items WHERE secret_record_id=?`, idv).Scan(&ct, &wrapped, &nonce, &alg, &kv); err != nil {
		return "", err
	}
	if alg != algorithm || kv != 1 {
		return "", ErrIntegrity
	}
	g, err := newGCM(s.kek)
	if err != nil {
		return "", err
	}
	if len(wrapped) <= g.NonceSize() {
		return "", ErrIntegrity
	}
	dek, err := open(s.kek, wrapped[:g.NonceSize()], wrapped[g.NonceSize():], []byte("wrap:"+r.ID))
	if err != nil {
		return "", err
	}
	plain, err := open(dek, nonce, ct, aad(r))
	for i := range dek {
		dek[i] = 0
	}
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ResolveWorkspaceLogical resolves the active version of a logical secret name
// within exactly one workspace. It is intended for trusted adapters such as the
// sandbox runner; callers receive the plaintext only at the final consumption
// boundary together with a non-secret record/version identity for audit/spec hashing.
func (s *Service) ResolveWorkspaceLogical(ctx context.Context, workspaceID, logicalName string) (value, recordID string, version int64, err error) {
	workspaceID = strings.TrimSpace(workspaceID)
	logicalName = strings.TrimSpace(logicalName)
	if workspaceID == "" || logicalName == "" {
		return "", "", 0, ErrInvalid
	}
	var idv string
	if err := s.db.QueryRowContext(ctx, `SELECT id,version FROM secret_records WHERE workspace_id=? AND logical_name=? AND status='active' ORDER BY version DESC LIMIT 1`, workspaceID, logicalName).Scan(&idv, &version); err != nil {
		if err == sql.ErrNoRows {
			return "", "", 0, ErrNotFound
		}
		return "", "", 0, err
	}
	value, err = s.Resolve(ctx, "vault:"+idv)
	if err != nil {
		return "", "", 0, err
	}
	return value, idv, version, nil
}

func (s *Service) Revoke(ctx context.Context, idv, actor string) error {
	if idv == "" || actor == "" {
		return ErrInvalid
	}
	r, err := s.record(ctx, idv)
	if err != nil {
		return err
	}
	if r.Status == "revoked" {
		return nil
	}
	now := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE secret_records SET status='revoked',retired_at=COALESCE(retired_at,?) WHERE id=? AND status<>'revoked'`, now, idv)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrNotFound
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"secret_record_id": idv})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: r.WorkspaceID, Type: "secret.revoked", AggregateType: "secret_record", AggregateID: idv, ActorPrincipalID: &actor, Payload: payload, OccurredAt: now})
	})
}

// ListWorkspace returns secret metadata only. Ciphertext and plaintext values are
// intentionally never part of the general control-plane read model.
func (s *Service) ListWorkspace(ctx context.Context, workspaceID string) ([]SecretRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,logical_name,provider_type,version,status,metadata_json,created_by,created_at,retired_at FROM secret_records WHERE workspace_id=? ORDER BY logical_name,version DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SecretRecord
	for rows.Next() {
		var r SecretRecord
		var ws sql.NullString
		var retired sql.NullInt64
		var meta string
		if err := rows.Scan(&r.ID, &ws, &r.LogicalName, &r.ProviderType, &r.Version, &r.Status, &meta, &r.CreatedBy, &r.CreatedAt, &retired); err != nil {
			return nil, err
		}
		if ws.Valid {
			r.WorkspaceID = &ws.String
		}
		if retired.Valid {
			r.RetiredAt = &retired.Int64
		}
		r.Metadata = json.RawMessage(meta)
		out = append(out, r)
	}
	return out, rows.Err()
}
