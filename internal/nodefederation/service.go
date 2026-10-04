package nodefederation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

var (
	ErrNodeNotPaired   = errors.New("node is not paired")
	ErrPairingExpired  = errors.New("pairing expired")
	ErrPairingCode     = errors.New("pairing code mismatch")
	ErrPeerCertificate = errors.New("peer certificate mismatch")
	ErrNotOperator     = errors.New("operator privileges required")
)

type Service struct {
	db            *sql.DB
	tx            storage.Transactor
	clock         clock.Clock
	events        event.Store
	ids           id.Generator
	local         NodeView
	identity      Identity
	federationURL string
	client        *http.Client
	staleAfter    time.Duration
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, local NodeView, ident Identity, federationURL string, staleAfter time.Duration) *Service {
	if staleAfter <= 0 {
		staleAfter = 45 * time.Second
	}
	return &Service{db: db, tx: tx, clock: clk, events: event.Store{}, ids: id.Generator{}, local: local, identity: ident, federationURL: strings.TrimRight(federationURL, "/"), client: &http.Client{Timeout: 15 * time.Second}, staleAfter: staleAfter}
}

func (s *Service) Local() NodeView       { return s.local }
func (s *Service) Identity() Identity    { return s.identity }
func (s *Service) FederationURL() string { return s.federationURL }

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func hashText(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func randomCode() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	n := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if n < 0 {
		n = -n
	}
	return fmt.Sprintf("%06d", n%1000000), nil
}

func scanNode(row interface{ Scan(...any) error }) (NodeView, error) {
	var n NodeView
	var local int
	var tz, endpoint sql.NullString
	var caps string
	var seen sql.NullInt64
	if err := row.Scan(&n.ID, &n.Name, &local, &n.IdentityFingerprint, &n.TrustState, &tz, &endpoint, &caps, &seen, &n.Revision); err != nil {
		return NodeView{}, err
	}
	n.Local = local == 1
	if tz.Valid {
		v := tz.String
		n.TrustZone = &v
	}
	if endpoint.Valid {
		n.EndpointJSON = json.RawMessage(endpoint.String)
	}
	n.CapabilitiesJSON = json.RawMessage(caps)
	if seen.Valid {
		v := seen.Int64
		n.LastSeenAt = &v
	}
	return n, nil
}

func (s *Service) Nodes(ctx context.Context) ([]NodeView, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,local,identity_fingerprint,trust_state,trust_zone,endpoint_json,capabilities_json,last_seen_at,revision FROM harness_nodes ORDER BY local DESC,name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeView
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
func (s *Service) Node(ctx context.Context, idv string) (NodeView, error) {
	return scanNode(s.db.QueryRowContext(ctx, `SELECT id,name,local,identity_fingerprint,trust_state,trust_zone,endpoint_json,capabilities_json,last_seen_at,revision FROM harness_nodes WHERE id=?`, idv))
}

func (s *Service) CanOperate(ctx context.Context, principalID string) error {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM principal_roles pr JOIN roles r ON r.id=pr.role_id JOIN workspace_memberships wm ON wm.workspace_id=pr.workspace_id AND wm.principal_id=pr.principal_id WHERE pr.principal_id=? AND wm.status='active' AND (r.name='Admin' OR EXISTS (SELECT 1 FROM json_each(r.definition_json,'$.capabilities') WHERE value='*'))`, principalID).Scan(&n)
	if err != nil {
		return err
	}
	if n < 1 {
		return ErrNotOperator
	}
	return nil
}

func endpointJSON(endpoint, tlsfp string) string {
	b, _ := json.Marshal(map[string]any{"federation_url": endpoint, "tls_fingerprint": tlsfp, "protocol": ProtocolVersion})
	return string(b)
}

func (s *Service) ObserveDiscovery(ctx context.Context, a DiscoveryAnnouncement, sourceIP string) error {
	if a.Protocol != ProtocolVersion || a.NodeID == "" || a.NodeID == s.local.ID || a.IdentityFingerprint == "" || a.TLSFingerprint == "" || a.FederationPort < 1 || a.FederationPort > 65535 {
		return nil
	}
	host := strings.Trim(sourceIP, "[]")
	endpoint := "https://" + host + ":" + strconv.Itoa(a.FederationPort)
	now := s.clock.UnixMilli()
	ep := endpointJSON(endpoint, a.TLSFingerprint)
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var state string
		err := tx.QueryRowContext(ctx, `SELECT trust_state FROM harness_nodes WHERE id=? OR identity_fingerprint=?`, a.NodeID, a.IdentityFingerprint).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,trust_zone,endpoint_json,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at) VALUES(?,?,0,?,'discovered',NULL,?,'{"control_plane":"0.1","inference_mesh":"v1"}','{"inference_provider":true,"inference_consumer":true}',?,1,?,?)`, a.NodeID, a.Name, a.IdentityFingerprint, ep, now, now, now)
			return err
		}
		if err != nil {
			return err
		}
		// Discovery is unauthenticated. It may refresh only an unpaired discovered
		// record; once pairing begins, the endpoint/certificate pin is immutable
		// until an operator explicitly revokes and re-pairs the node.
		if state != "discovered" {
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE harness_nodes SET name=?,endpoint_json=?,last_seen_at=?,revision=revision+1,updated_at=? WHERE id=? AND trust_state='discovered'`, a.Name, ep, now, now, a.NodeID)
		return err
	})
}

func (s *Service) Pairings(ctx context.Context) ([]Pairing, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,peer_node_id,direction,status,local_confirmed,peer_confirmed,peer_tls_fingerprint,expires_at,paired_at,revision,created_at,updated_at FROM node_pairings ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pairing
	for rows.Next() {
		var p Pairing
		var l, r int
		var paired sql.NullInt64
		if err := rows.Scan(&p.ID, &p.PeerNodeID, &p.Direction, &p.Status, &l, &r, &p.PeerTLSFingerprint, &p.ExpiresAt, &paired, &p.Revision, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.LocalConfirmed = l == 1
		p.PeerConfirmed = r == 1
		if paired.Valid {
			v := paired.Int64
			p.PairedAt = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func certFromPEM(raw string) (*x509.Certificate, error) {
	b, _ := pem.Decode([]byte(raw))
	if b == nil || b.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("invalid certificate pem")
	}
	return x509.ParseCertificate(b.Bytes)
}
func endpointFromNode(n NodeView) (string, string, error) {
	var v struct {
		FederationURL  string `json:"federation_url"`
		TLSFingerprint string `json:"tls_fingerprint"`
	}
	if len(n.EndpointJSON) == 0 || json.Unmarshal(n.EndpointJSON, &v) != nil || v.FederationURL == "" || v.TLSFingerprint == "" {
		return "", "", fmt.Errorf("node has no federation endpoint")
	}
	u, err := url.Parse(v.FederationURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", "", fmt.Errorf("invalid federation endpoint")
	}
	return strings.TrimRight(v.FederationURL, "/"), v.TLSFingerprint, nil
}

func (s *Service) pairedEndpoint(ctx context.Context, peerID string) (string, string, error) {
	var raw, fp, state, pairStatus string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(h.endpoint_json,''),p.peer_tls_fingerprint,h.trust_state,p.status FROM harness_nodes h JOIN node_pairings p ON p.peer_node_id=h.id WHERE h.id=?`, peerID).Scan(&raw, &fp, &state, &pairStatus); err != nil {
		return "", "", err
	}
	if state != "paired" || pairStatus != "paired" {
		return "", "", ErrNodeNotPaired
	}
	var ep struct {
		FederationURL string `json:"federation_url"`
	}
	if json.Unmarshal([]byte(raw), &ep) != nil || strings.TrimSpace(ep.FederationURL) == "" {
		return "", "", fmt.Errorf("paired node has invalid endpoint")
	}
	u, err := url.Parse(ep.FederationURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", "", fmt.Errorf("invalid federation endpoint")
	}
	return strings.TrimRight(ep.FederationURL, "/"), fp, nil
}

func (s *Service) pinnedClient(fp string, withClientCert bool) *http.Client {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true}
	if withClientCert {
		tlsCfg.Certificates = []tls.Certificate{s.identity.Certificate}
	}
	tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) != 1 {
			return ErrPeerCertificate
		}
		if CertificateFingerprint(cs.PeerCertificates[0].Raw) != fp {
			return ErrPeerCertificate
		}
		return nil
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg, ForceAttemptHTTP2: true}}
}

func (s *Service) BeginPair(ctx context.Context, peerID string) (Pairing, string, error) {
	peer, err := s.Node(ctx, peerID)
	if err != nil {
		return Pairing{}, "", err
	}
	if peer.Local {
		return Pairing{}, "", fmt.Errorf("cannot pair local node")
	}
	endpoint, fp, err := endpointFromNode(peer)
	if err != nil {
		return Pairing{}, "", err
	}
	pairID, err := s.ids.New("pair")
	if err != nil {
		return Pairing{}, "", err
	}
	token, err := randomHex(24)
	if err != nil {
		return Pairing{}, "", err
	}
	code, err := randomCode()
	if err != nil {
		return Pairing{}, "", err
	}
	now := s.clock.UnixMilli()
	expires := now + 5*60*1000
	cert := string(s.identity.CertPEM)
	req := PairRequest{Protocol: ProtocolVersion, PairingID: pairID, NodeID: s.local.ID, Name: s.local.Name, IdentityFingerprint: s.local.IdentityFingerprint, TLSFingerprint: s.identity.Fingerprint, CertificatePEM: cert, Endpoint: s.federationURL, PairingToken: token, PairingCode: code, ExpiresAt: expires}
	if err := s.storePairing(ctx, peer.ID, "outbound", "requested", pairID, token, code, fp, "", expires, false, false); err != nil {
		return Pairing{}, "", err
	}
	var reply map[string]any
	if err := postJSON(ctx, s.pinnedClient(fp, false), endpoint+"/federation/v1/pair/request", req, &reply); err != nil {
		_ = s.failPairing(ctx, peer.ID, "failed")
		return Pairing{}, "", err
	}
	_ = s.setPairingState(ctx, peer.ID, "pairing")
	p, err := s.pairingByPeer(ctx, peer.ID)
	return p, code, err
}

func (s *Service) ReceivePairRequest(ctx context.Context, req PairRequest) error {
	if req.Protocol != ProtocolVersion || req.NodeID == "" || req.NodeID == s.local.ID || req.PairingID == "" || req.PairingToken == "" || len(req.PairingCode) != 6 {
		return fmt.Errorf("invalid pairing request")
	}
	now := s.clock.UnixMilli()
	if req.ExpiresAt <= now || req.ExpiresAt > now+10*60*1000 {
		return ErrPairingExpired
	}
	cert, err := certFromPEM(req.CertificatePEM)
	if err != nil {
		return err
	}
	if CertificateFingerprint(cert.Raw) != req.TLSFingerprint {
		return ErrPeerCertificate
	}
	u, err := url.Parse(req.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("invalid peer endpoint")
	}
	if err := s.upsertPairingNode(ctx, req.NodeID, req.Name, req.IdentityFingerprint, req.Endpoint, req.TLSFingerprint); err != nil {
		return err
	}
	return s.storePairing(ctx, req.NodeID, "inbound", "pairing", req.PairingID, req.PairingToken, req.PairingCode, req.TLSFingerprint, req.CertificatePEM, req.ExpiresAt, false, false)
}

func (s *Service) upsertPairingNode(ctx context.Context, nodeID, name, identityFP, endpoint, tlsFP string) error {
	now := s.clock.UnixMilli()
	ep := endpointJSON(endpoint, tlsFP)
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var idv string
		err := tx.QueryRowContext(ctx, `SELECT id FROM harness_nodes WHERE id=? OR identity_fingerprint=?`, nodeID, identityFP).Scan(&idv)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,endpoint_json,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at) VALUES(?,?,0,?,'pairing',?,'{"control_plane":"0.1","inference_mesh":"v1"}','{"inference_provider":true,"inference_consumer":true}',?,1,?,?)`, nodeID, name, identityFP, ep, now, now, now)
			return err
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE harness_nodes SET name=?,trust_state=CASE WHEN trust_state='paired' THEN 'paired' ELSE 'pairing' END,endpoint_json=?,last_seen_at=?,revision=revision+1,updated_at=? WHERE id=?`, name, ep, now, now, idv)
		return err
	})
}

func (s *Service) storePairing(ctx context.Context, peer, direction, status, pairID, token, code, fp, cert string, expires int64, local, remote bool) error {
	now := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO node_pairings(id,peer_node_id,direction,status,pairing_token,pairing_token_hash,pairing_code,pairing_code_hash,local_confirmed,peer_confirmed,peer_tls_fingerprint,peer_certificate_pem,expires_at,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?) ON CONFLICT(peer_node_id) DO UPDATE SET id=excluded.id,direction=excluded.direction,status=excluded.status,pairing_token=excluded.pairing_token,pairing_token_hash=excluded.pairing_token_hash,pairing_code=excluded.pairing_code,pairing_code_hash=excluded.pairing_code_hash,local_confirmed=excluded.local_confirmed,peer_confirmed=excluded.peer_confirmed,peer_tls_fingerprint=excluded.peer_tls_fingerprint,peer_certificate_pem=CASE WHEN excluded.peer_certificate_pem<>'' THEN excluded.peer_certificate_pem ELSE node_pairings.peer_certificate_pem END,expires_at=excluded.expires_at,paired_at=NULL,revision=node_pairings.revision+1,updated_at=excluded.updated_at`, pairID, peer, direction, status, token, hashText(token), code, hashText(code), boolInt(local), boolInt(remote), fp, cert, expires, now, now)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE harness_nodes SET trust_state='pairing',revision=revision+1,updated_at=? WHERE id=? AND trust_state NOT IN ('local','paired')`, now, peer)
		return err
	})
}

func (s *Service) pairingByPeer(ctx context.Context, peer string) (Pairing, error) {
	var p Pairing
	var l, r int
	var paired sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,peer_node_id,direction,status,local_confirmed,peer_confirmed,peer_tls_fingerprint,expires_at,paired_at,revision,created_at,updated_at FROM node_pairings WHERE peer_node_id=?`, peer).Scan(&p.ID, &p.PeerNodeID, &p.Direction, &p.Status, &l, &r, &p.PeerTLSFingerprint, &p.ExpiresAt, &paired, &p.Revision, &p.CreatedAt, &p.UpdatedAt)
	p.LocalConfirmed = l == 1
	p.PeerConfirmed = r == 1
	if paired.Valid {
		v := paired.Int64
		p.PairedAt = &v
	}
	return p, err
}
func (s *Service) pairingSecrets(ctx context.Context, peer string) (Pairing, string, string, string, error) {
	p, err := s.pairingByPeer(ctx, peer)
	if err != nil {
		return Pairing{}, "", "", "", err
	}
	var token, code, cert string
	err = s.db.QueryRowContext(ctx, `SELECT pairing_token,pairing_code,peer_certificate_pem FROM node_pairings WHERE peer_node_id=?`, peer).Scan(&token, &code, &cert)
	return p, token, code, cert, err
}

func (s *Service) ConfirmPair(ctx context.Context, peerID, code string) (Pairing, error) {
	p, token, storedCode, _, err := s.pairingSecrets(ctx, peerID)
	if err != nil {
		return Pairing{}, err
	}
	now := s.clock.UnixMilli()
	if now >= p.ExpiresAt {
		return Pairing{}, ErrPairingExpired
	}
	if code != storedCode {
		return Pairing{}, ErrPairingCode
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE node_pairings SET local_confirmed=1,status='pairing',revision=revision+1,updated_at=? WHERE peer_node_id=?`, now, peerID); err != nil {
		return Pairing{}, err
	}
	peer, err := s.Node(ctx, peerID)
	if err != nil {
		return Pairing{}, err
	}
	endpoint, fp, err := endpointFromNode(peer)
	if err != nil {
		return Pairing{}, err
	}
	req := PairConfirm{Protocol: ProtocolVersion, PairingID: p.ID, NodeID: s.local.ID, PairingToken: token}
	var reply struct {
		PeerConfirmed  bool   `json:"peer_confirmed"`
		Paired         bool   `json:"paired"`
		CertificatePEM string `json:"certificate_pem"`
	}
	if err := postJSON(ctx, s.pinnedClient(fp, false), endpoint+"/federation/v1/pair/confirm", req, &reply); err != nil {
		return s.pairingByPeer(ctx, peerID)
	}
	if reply.PeerConfirmed {
		_, _ = s.db.ExecContext(ctx, `UPDATE node_pairings SET peer_confirmed=1,peer_certificate_pem=CASE WHEN ?<>'' THEN ? ELSE peer_certificate_pem END,revision=revision+1,updated_at=? WHERE peer_node_id=?`, reply.CertificatePEM, reply.CertificatePEM, now, peerID)
	}
	if err := s.finalizeIfReady(ctx, peerID); err != nil {
		return Pairing{}, err
	}
	return s.pairingByPeer(ctx, peerID)
}

func (s *Service) ReceivePeerConfirm(ctx context.Context, req PairConfirm) (bool, bool, error) {
	if req.Protocol != ProtocolVersion {
		return false, false, fmt.Errorf("protocol mismatch")
	}
	var peer, token string
	var local int
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT peer_node_id,pairing_token,local_confirmed,expires_at FROM node_pairings WHERE id=?`, req.PairingID).Scan(&peer, &token, &local, &expires)
	if err != nil {
		return false, false, err
	}
	if peer != req.NodeID || token != req.PairingToken {
		return false, false, ErrPairingCode
	}
	if s.clock.UnixMilli() >= expires {
		return false, false, ErrPairingExpired
	}
	_, err = s.db.ExecContext(ctx, `UPDATE node_pairings SET peer_confirmed=1,revision=revision+1,updated_at=? WHERE id=?`, s.clock.UnixMilli(), req.PairingID)
	if err != nil {
		return false, false, err
	}
	if err := s.finalizeIfReady(ctx, peer); err != nil {
		return false, false, err
	}
	p, err := s.pairingByPeer(ctx, peer)
	return p.LocalConfirmed, p.Status == "paired", err
}

func (s *Service) finalizeIfReady(ctx context.Context, peer string) error {
	now := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var l, r int
		var status string
		var exp int64
		if err := tx.QueryRowContext(ctx, `SELECT local_confirmed,peer_confirmed,status,expires_at FROM node_pairings WHERE peer_node_id=?`, peer).Scan(&l, &r, &status, &exp); err != nil {
			return err
		}
		if status == "paired" {
			return nil
		}
		if now >= exp {
			return ErrPairingExpired
		}
		if l != 1 || r != 1 {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE node_pairings SET status='paired',paired_at=?,revision=revision+1,updated_at=? WHERE peer_node_id=?`, now, now, peer); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE harness_nodes SET trust_state='paired',trust_zone='PAIRED_MTLS',last_seen_at=?,revision=revision+1,updated_at=? WHERE id=?`, now, now, peer)
		return err
	})
}

func (s *Service) setPairingState(ctx context.Context, peer, state string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE node_pairings SET status=?,revision=revision+1,updated_at=? WHERE peer_node_id=?`, state, s.clock.UnixMilli(), peer)
	return err
}
func (s *Service) failPairing(ctx context.Context, peer, state string) error {
	_ = s.setPairingState(ctx, peer, state)
	_, err := s.db.ExecContext(ctx, `UPDATE harness_nodes SET trust_state='discovered',revision=revision+1,updated_at=? WHERE id=? AND trust_state='pairing'`, s.clock.UnixMilli(), peer)
	return err
}

func (s *Service) BindRemoteModelJob(ctx context.Context, peer, jobID string) error {
	if !s.modelManagementAllowed(ctx, peer) {
		return ErrNodeNotPaired
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO node_remote_model_jobs(peer_node_id,job_id,created_at) VALUES(?,?,?) ON CONFLICT(peer_node_id,job_id) DO NOTHING`, peer, jobID, s.clock.UnixMilli())
	return err
}

func (s *Service) RemoteModelJobAllowed(ctx context.Context, peer, jobID string) bool {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_remote_model_jobs WHERE peer_node_id=? AND job_id=?`, peer, jobID).Scan(&n)
	return err == nil && n == 1
}

func (s *Service) ModelManagementGrant(ctx context.Context, peer string) (ModelManagementGrant, error) {
	var g ModelManagementGrant
	var enabled, allow int
	var updatedBy sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT peer_node_id,enabled,allow_catalog_install,updated_by,revision,created_at,updated_at FROM node_model_management_grants WHERE peer_node_id=?`, peer).Scan(&g.PeerNodeID, &enabled, &allow, &updatedBy, &g.Revision, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return g, err
	}
	g.Enabled = enabled == 1
	g.AllowCatalogInstall = allow == 1
	if updatedBy.Valid {
		v := updatedBy.String
		g.UpdatedBy = &v
	}
	return g, nil
}

func (s *Service) SetModelManagementGrant(ctx context.Context, actor, peer string, enabled bool) (ModelManagementGrant, error) {
	if err := s.CanOperate(ctx, actor); err != nil {
		return ModelManagementGrant{}, err
	}
	var state string
	if err := s.db.QueryRowContext(ctx, `SELECT trust_state FROM harness_nodes WHERE id=? AND local=0`, peer).Scan(&state); err != nil {
		return ModelManagementGrant{}, err
	}
	if state != "paired" && state != "unavailable" {
		return ModelManagementGrant{}, ErrNodeNotPaired
	}
	now := s.clock.UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO node_model_management_grants(peer_node_id,enabled,allow_catalog_install,updated_by,revision,created_at,updated_at) VALUES(?,?,1,?,1,?,?) ON CONFLICT(peer_node_id) DO UPDATE SET enabled=excluded.enabled,allow_catalog_install=1,updated_by=excluded.updated_by,revision=node_model_management_grants.revision+1,updated_at=excluded.updated_at`, peer, boolInt(enabled), actor, now, now)
	if err != nil {
		return ModelManagementGrant{}, err
	}
	return s.ModelManagementGrant(ctx, peer)
}

func (s *Service) modelManagementAllowed(ctx context.Context, peer string) bool {
	var enabled, allow int
	err := s.db.QueryRowContext(ctx, `SELECT enabled,allow_catalog_install FROM node_model_management_grants WHERE peer_node_id=?`, peer).Scan(&enabled, &allow)
	return err == nil && enabled == 1 && allow == 1
}

func (s *Service) Revoke(ctx context.Context, peer string) error {
	now := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE node_pairings SET status='revoked',revision=revision+1,updated_at=? WHERE peer_node_id=?`, now, peer); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE harness_nodes SET trust_state='revoked',revision=revision+1,updated_at=? WHERE id=?`, now, peer); err != nil {
			return err
		}
		_, _ = tx.ExecContext(ctx, `UPDATE node_model_management_grants SET enabled=0,revision=revision+1,updated_at=? WHERE peer_node_id=?`, now, peer)
		_, err := tx.ExecContext(ctx, `UPDATE model_deployments SET status='unavailable',revision=revision+1,updated_at=? WHERE node_id=? AND json_extract(runtime_config_json,'$.federation.remote_deployment_id') IS NOT NULL`, now, peer)
		return err
	})
}

func postJSON(ctx context.Context, c *http.Client, urlv string, in, out any) error {
	b, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlv, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("peer http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out != nil && len(body) > 0 {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (s *Service) Manifest(ctx context.Context, peerID string) (CapabilityManifest, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT manifest_json FROM node_capability_manifests WHERE peer_node_id=?`, peerID).Scan(&raw); err != nil {
		return CapabilityManifest{}, err
	}
	var m CapabilityManifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return CapabilityManifest{}, err
	}
	return m, nil
}

func (s *Service) ActivateLocalCapabilities(ctx context.Context) error {
	now := s.clock.UnixMilli()
	protocol, _ := json.Marshal(map[string]any{"control_plane": "0.1", "inference_mesh": "v1", "federation": ProtocolVersion})
	caps, _ := json.Marshal(map[string]any{"inference_provider": true, "inference_consumer": true, "can_originate_tasks": true, "remote_model_management": true, "max_hops": 1})
	_, err := s.db.ExecContext(ctx, `UPDATE harness_nodes SET protocol_json=?,capabilities_json=?,revision=revision+1,updated_at=? WHERE id=? AND local=1`, string(protocol), string(caps), now, s.local.ID)
	if err == nil {
		s.local.CapabilitiesJSON = caps
	}
	return err
}

func (s *Service) ComputePolicy(ctx context.Context, peer string) (ComputePolicy, error) {
	var out ComputePolicy
	var availability, limits, projects string
	var enabled, idle, downloads int
	var updatedBy sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT node_id,enabled,idle_only,availability_json,limits_json,allow_model_downloads,runtime_installation,project_scope,allowed_projects_json,updated_by,revision,created_at,updated_at FROM remote_node_compute_policies WHERE node_id=?`, strings.TrimSpace(peer)).Scan(&out.NodeID, &enabled, &idle, &availability, &limits, &downloads, &out.RuntimeInstallation, &out.ProjectScope, &projects, &updatedBy, &out.Revision, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return out, err
	}
	out.Enabled, out.IdleOnly, out.AllowModelDownloads = enabled != 0, idle != 0, downloads != 0
	out.AvailabilityJSON, out.LimitsJSON, out.AllowedProjectsJSON = json.RawMessage(availability), json.RawMessage(limits), json.RawMessage(projects)
	if updatedBy.Valid {
		v := updatedBy.String
		out.UpdatedBy = &v
	}
	return out, nil
}

func canonicalPolicyJSON(raw json.RawMessage, fallback string) (string, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(fallback)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Service) SetComputePolicy(ctx context.Context, cmd SetComputePolicyCommand) (ComputePolicy, error) {
	cmd.NodeID, cmd.Actor = strings.TrimSpace(cmd.NodeID), strings.TrimSpace(cmd.Actor)
	if cmd.NodeID == "" || cmd.Actor == "" {
		return ComputePolicy{}, errors.New("node and actor are required")
	}
	if err := s.CanOperate(ctx, cmd.Actor); err != nil {
		return ComputePolicy{}, err
	}
	if cmd.RuntimeInstallation == "" {
		cmd.RuntimeInstallation = "confirm"
	}
	if cmd.RuntimeInstallation != "allow" && cmd.RuntimeInstallation != "confirm" && cmd.RuntimeInstallation != "deny" {
		return ComputePolicy{}, errors.New("runtime_installation must be allow, confirm or deny")
	}
	if cmd.ProjectScope == "" {
		cmd.ProjectScope = "all"
	}
	if cmd.ProjectScope != "all" && cmd.ProjectScope != "selected" {
		return ComputePolicy{}, errors.New("project_scope must be all or selected")
	}
	availability, err := canonicalPolicyJSON(cmd.AvailabilityJSON, `{}`)
	if err != nil {
		return ComputePolicy{}, err
	}
	limits, err := canonicalPolicyJSON(cmd.LimitsJSON, `{}`)
	if err != nil {
		return ComputePolicy{}, err
	}
	projects, err := canonicalPolicyJSON(cmd.AllowedProjectsJSON, `[]`)
	if err != nil {
		return ComputePolicy{}, err
	}
	now := s.clock.UnixMilli()
	_, err = s.db.ExecContext(ctx, `INSERT INTO remote_node_compute_policies(node_id,enabled,idle_only,availability_json,limits_json,allow_model_downloads,runtime_installation,project_scope,allowed_projects_json,updated_by,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,1,?,?) ON CONFLICT(node_id) DO UPDATE SET enabled=excluded.enabled,idle_only=excluded.idle_only,availability_json=excluded.availability_json,limits_json=excluded.limits_json,allow_model_downloads=excluded.allow_model_downloads,runtime_installation=excluded.runtime_installation,project_scope=excluded.project_scope,allowed_projects_json=excluded.allowed_projects_json,updated_by=excluded.updated_by,revision=remote_node_compute_policies.revision+1,updated_at=excluded.updated_at`, cmd.NodeID, boolInt(cmd.Enabled), boolInt(cmd.IdleOnly), availability, limits, boolInt(cmd.AllowModelDownloads), cmd.RuntimeInstallation, cmd.ProjectScope, projects, cmd.Actor, now, now)
	if err != nil {
		return ComputePolicy{}, err
	}
	return s.ComputePolicy(ctx, cmd.NodeID)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
