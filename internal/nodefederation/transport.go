package nodefederation

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/localai"
)

type LocalInferenceExecutor interface {
	DispatchFederatedLocal(context.Context, string, string, json.RawMessage) (inference.DispatchResult, error)
}

type LocalModelManager interface {
	FederatedRecommendations(context.Context, string, localai.RecommendRequest) (localai.HardwareProfile, []localai.Recommendation, error)
	QueueFederatedInstall(context.Context, string, localai.FederatedInstallRequest) (localai.InstallJob, error)
	InstallJob(context.Context, string) (localai.InstallJob, error)
	FederatedInferenceAllowed(context.Context, string) bool
	SpecSheet(context.Context, string) (localai.ModelSpecSheet, error)
	StartTestbed(context.Context, string, *string, string) (localai.TestbedSession, error)
	TestbedSession(context.Context, string) (localai.TestbedSession, error)
	RunTestbedTurn(context.Context, string, localai.TestbedTurnCommand) (localai.TestbedTurn, error)
	ListTestbedTurns(context.Context, string) ([]localai.TestbedTurn, error)
	CompleteTestbed(context.Context, string) error
	AdmitModel(context.Context, string, localai.AdmissionCommand) (localai.ModelSpecSheet, error)
}

type RemoteTransport struct{ Service *Service }

func (t RemoteTransport) Dispatch(ctx context.Context, req inference.DispatchRequest, _ inference.SecretResolver) (inference.DispatchResult, error) {
	if t.Service == nil {
		return inference.DispatchResult{}, &inference.TransportError{Code: "federation_unavailable", OutcomeKnown: true}
	}
	var cfg struct {
		Federation struct {
			RemoteDeploymentID string `json:"remote_deployment_id"`
			PeerNodeID         string `json:"peer_node_id"`
		} `json:"federation"`
	}
	if json.Unmarshal(req.Deployment.RuntimeConfigJSON, &cfg) != nil || cfg.Federation.RemoteDeploymentID == "" || cfg.Federation.PeerNodeID == "" {
		return inference.DispatchResult{}, &inference.TransportError{Code: "invalid_federated_deployment", OutcomeKnown: true}
	}
	endpoint, fp, err := t.Service.pairedEndpoint(ctx, cfg.Federation.PeerNodeID)
	if err != nil {
		return inference.DispatchResult{}, &inference.TransportError{Code: "peer_endpoint_invalid", OutcomeKnown: true, Err: err}
	}
	in := RemoteInferenceRequest{Protocol: ProtocolVersion, RequestID: req.RequestID, DeploymentID: cfg.Federation.RemoteDeploymentID, RequestJSON: req.RequestJSON}
	var out RemoteInferenceResponse
	err = postJSON(ctx, t.Service.pinnedClient(fp, true), endpoint+"/federation/v1/inference", in, &out)
	if err != nil {
		return inference.DispatchResult{}, &inference.TransportError{Code: "remote_transport_error", OutcomeKnown: false, Err: err}
	}
	if out.ErrorCode != "" {
		return inference.DispatchResult{}, &inference.TransportError{Code: out.ErrorCode, OutcomeKnown: out.OutcomeKnown}
	}
	return inference.DispatchResult{ResponseJSON: out.ResponseJSON, UsageJSON: out.UsageJSON}, nil
}

type Server struct {
	svc       *Service
	inference LocalInferenceExecutor
	models    LocalModelManager
	mux       *http.ServeMux
}

func NewServer(svc *Service, exec LocalInferenceExecutor, managers ...LocalModelManager) *Server {
	var mgr LocalModelManager
	if len(managers) > 0 {
		mgr = managers[0]
	}
	s := &Server{svc: svc, inference: exec, models: mgr, mux: http.NewServeMux()}
	s.routes()
	return s
}
func (s *Server) Handler() http.Handler { return s.mux }
func (s *Server) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{s.svc.identity.Certificate}, ClientAuth: tls.RequestClientCert}
}
func (s *Server) routes() {
	s.mux.HandleFunc("GET /federation/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "protocol": ProtocolVersion, "node_id": s.svc.local.ID, "tls_fingerprint": s.svc.identity.Fingerprint})
	})
	s.mux.HandleFunc("POST /federation/v1/pair/request", s.pairRequest)
	s.mux.HandleFunc("POST /federation/v1/pair/confirm", s.pairConfirm)
	s.mux.HandleFunc("POST /federation/v1/heartbeat", s.heartbeat)
	s.mux.HandleFunc("POST /federation/v1/inference", s.remoteInference)
	s.mux.HandleFunc("POST /federation/v1/models/management-status", s.remoteModelManagementStatus)
	s.mux.HandleFunc("POST /federation/v1/models/recommend", s.remoteModelRecommend)
	s.mux.HandleFunc("POST /federation/v1/models/install", s.remoteModelInstall)
	s.mux.HandleFunc("POST /federation/v1/models/install-status", s.remoteModelInstallStatus)
	s.mux.HandleFunc("POST /federation/v1/models/spec-sheet", s.remoteModelSpecSheet)
	s.mux.HandleFunc("POST /federation/v1/models/testbed/start", s.remoteModelTestbedStart)
	s.mux.HandleFunc("POST /federation/v1/models/testbed/session", s.remoteModelTestbedSession)
	s.mux.HandleFunc("POST /federation/v1/models/testbed/turns", s.remoteModelTestbedTurns)
	s.mux.HandleFunc("POST /federation/v1/models/testbed/turn", s.remoteModelTestbedTurn)
	s.mux.HandleFunc("POST /federation/v1/models/testbed/complete", s.remoteModelTestbedComplete)
	s.mux.HandleFunc("POST /federation/v1/models/admit", s.remoteModelAdmit)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return false
	}
	return true
}
func (s *Server) pairRequest(w http.ResponseWriter, r *http.Request) {
	var in PairRequest
	if !decode(w, r, &in) {
		return
	}
	if err := s.svc.ReceivePairRequest(r.Context(), in); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 202, map[string]any{"accepted": true, "pairing_id": in.PairingID, "expires_at": in.ExpiresAt})
}
func (s *Server) pairConfirm(w http.ResponseWriter, r *http.Request) {
	var in PairConfirm
	if !decode(w, r, &in) {
		return
	}
	local, paired, err := s.svc.ReceivePeerConfirm(r.Context(), in)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"peer_confirmed": local, "paired": paired, "certificate_pem": string(s.svc.identity.CertPEM)})
}

func (s *Service) peerFromTLS(r *http.Request) (string, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
		return "", ErrPeerCertificate
	}
	fp := CertificateFingerprint(r.TLS.PeerCertificates[0].Raw)
	var peer, state, stored string
	err := s.db.QueryRowContext(r.Context(), `SELECT np.peer_node_id,hn.trust_state,np.peer_tls_fingerprint FROM node_pairings np JOIN harness_nodes hn ON hn.id=np.peer_node_id WHERE np.status='paired' AND np.peer_tls_fingerprint=?`, fp).Scan(&peer, &state, &stored)
	if err != nil {
		return "", ErrPeerCertificate
	}
	if (state != "paired" && state != "unavailable") || stored != fp {
		return "", ErrNodeNotPaired
	}
	return peer, nil
}
func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	peer, err := s.svc.peerFromTLS(r)
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": "unpaired client certificate"})
		return
	}
	var in Heartbeat
	if !decode(w, r, &in) {
		return
	}
	if err := s.svc.ReceiveHeartbeat(r.Context(), peer, in); err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "received_at": s.svc.clock.UnixMilli()})
}
func (s *Server) remoteModelManagementStatus(w http.ResponseWriter, r *http.Request) {
	peer, err := s.svc.peerFromTLS(r)
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": "unpaired client certificate"})
		return
	}
	g, err := s.svc.ModelManagementGrant(r.Context(), peer)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 200, map[string]any{"peer_node_id": peer, "enabled": false, "allow_catalog_install": true})
			return
		}
		writeJSON(w, 500, map[string]string{"error": "read model-management grant failed"})
		return
	}
	writeJSON(w, 200, g)
}

func (s *Server) authorizeModelManagement(w http.ResponseWriter, r *http.Request) (string, bool) {
	peer, err := s.svc.peerFromTLS(r)
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": "unpaired client certificate"})
		return "", false
	}
	if s.models == nil || !s.svc.modelManagementAllowed(r.Context(), peer) {
		writeJSON(w, 403, map[string]string{"error": "remote model management is not enabled for this peer"})
		return "", false
	}
	return peer, true
}

func (s *Server) remoteModelRecommend(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeModelManagement(w, r); !ok {
		return
	}
	var in RemoteModelRecommendRequest
	if !decode(w, r, &in) {
		return
	}
	fit := localai.FitLevel(in.MinimumFit)
	if fit == "" {
		fit = localai.FitMarginal
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	profile, recs, err := s.models.FederatedRecommendations(r.Context(), s.svc.local.ID, localai.RecommendRequest{UseCase: localai.UseCase(in.UseCase), ContextTokens: in.ContextTokens, Limit: limit, MinimumFit: fit, StorageHeadroomPct: 20, PreferGPU: in.PreferGPU, PlacementPreference: localai.PlacementMode(in.PlacementPreference)})
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"hardware_profile": profile, "recommendations": recs})
}

func (s *Server) remoteModelInstall(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeModelManagement(w, r); !ok {
		return
	}
	var in RemoteModelInstallRequest
	if !decode(w, r, &in) {
		return
	}
	job, err := s.models.QueueFederatedInstall(r.Context(), s.svc.local.ID, localai.FederatedInstallRequest{ModelRef: in.ModelRef, Quantization: in.Quantization, UseCase: localai.UseCase(in.UseCase), ContextTokens: in.ContextTokens, RoleName: in.RoleName, PreferGPU: in.PreferGPU, PlacementPreference: localai.PlacementMode(in.PlacementPreference)})
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 202, job)
}

func (s *Server) remoteModelInstallStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeModelManagement(w, r); !ok {
		return
	}
	var in struct {
		JobID string `json:"job_id"`
	}
	if !decode(w, r, &in) || in.JobID == "" {
		return
	}
	job, err := s.models.InstallJob(r.Context(), in.JobID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "install job not found"})
		return
	}
	writeJSON(w, 200, job)
}

func (s *Server) remoteModelSpecSheet(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeModelManagement(w, r); !ok {
		return
	}
	var in struct {
		DeploymentID string `json:"deployment_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	x, err := s.models.SpecSheet(r.Context(), strings.TrimSpace(in.DeploymentID))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "model spec sheet not found"})
		return
	}
	writeJSON(w, 200, x)
}
func (s *Server) remoteModelTestbedStart(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeModelManagement(w, r); !ok {
		return
	}
	var in struct {
		DeploymentID string `json:"deployment_id"`
		Notes        string `json:"notes"`
	}
	if !decode(w, r, &in) {
		return
	}
	actor := localai.FederatedModelManagerPrincipal
	x, err := s.models.StartTestbed(r.Context(), strings.TrimSpace(in.DeploymentID), &actor, in.Notes)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, x)
}

func (s *Server) remoteModelTestbedSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeModelManagement(w, r); !ok {
		return
	}
	var in struct {
		SessionID string `json:"session_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	x, err := s.models.TestbedSession(r.Context(), strings.TrimSpace(in.SessionID))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "model testbed session not found"})
		return
	}
	writeJSON(w, 200, x)
}

func (s *Server) remoteModelTestbedTurns(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeModelManagement(w, r); !ok {
		return
	}
	var in struct {
		SessionID string `json:"session_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	x, err := s.models.ListTestbedTurns(r.Context(), strings.TrimSpace(in.SessionID))
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"turns": x})
}

func (s *Server) remoteModelTestbedTurn(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeModelManagement(w, r); !ok {
		return
	}
	var in struct {
		SessionID string                     `json:"session_id"`
		Command   localai.TestbedTurnCommand `json:"command"`
	}
	if !decode(w, r, &in) {
		return
	}
	x, err := s.models.RunTestbedTurn(r.Context(), strings.TrimSpace(in.SessionID), in.Command)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, x)
}
func (s *Server) remoteModelTestbedComplete(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeModelManagement(w, r); !ok {
		return
	}
	var in struct {
		SessionID string `json:"session_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.models.CompleteTestbed(r.Context(), strings.TrimSpace(in.SessionID)); err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"completed": true})
}
func (s *Server) remoteModelAdmit(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeModelManagement(w, r); !ok {
		return
	}
	var in struct {
		DeploymentID string                   `json:"deployment_id"`
		Command      localai.AdmissionCommand `json:"command"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Command.ActorPrincipalID = localai.FederatedModelManagerPrincipal
	x, err := s.models.AdmitModel(r.Context(), strings.TrimSpace(in.DeploymentID), in.Command)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, x)
}

func (s *Server) remoteInference(w http.ResponseWriter, r *http.Request) {
	peer, err := s.svc.peerFromTLS(r)
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": "unpaired client certificate"})
		return
	}
	var in RemoteInferenceRequest
	if !decode(w, r, &in) {
		return
	}
	if in.Protocol != ProtocolVersion || in.RequestID == "" || in.DeploymentID == "" || !json.Valid(in.RequestJSON) {
		writeJSON(w, 400, map[string]string{"error": "invalid inference request"})
		return
	}
	if s.models != nil && !s.models.FederatedInferenceAllowed(r.Context(), in.DeploymentID) {
		writeJSON(w, 403, map[string]string{"error": "model deployment is not admitted for federated inference"})
		return
	}
	out, err := s.svc.executeInbound(r.Context(), peer, in, s.inference)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, out)
}

func (s *Service) executeInbound(ctx context.Context, peer string, in RemoteInferenceRequest, exec LocalInferenceExecutor) (RemoteInferenceResponse, error) {
	var status string
	var resp, usage, errorCode sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT status,response_json,usage_json,error_code FROM node_federated_inference_receipts WHERE peer_node_id=? AND remote_request_id=?`, peer, in.RequestID).Scan(&status, &resp, &usage, &errorCode)
	if err == nil {
		if status == "succeeded" && resp.Valid {
			return RemoteInferenceResponse{ResponseJSON: json.RawMessage(resp.String), UsageJSON: json.RawMessage(usage.String), OutcomeKnown: true}, nil
		}
		if status == "executing" || status == "unknown" {
			return RemoteInferenceResponse{ErrorCode: "duplicate_or_unknown_remote_request", OutcomeKnown: false}, nil
		}
		return RemoteInferenceResponse{ErrorCode: errorCode.String, OutcomeKnown: true}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RemoteInferenceResponse{}, err
	}
	idv, err := s.ids.New("fedrecv")
	if err != nil {
		return RemoteInferenceResponse{}, err
	}
	now := s.clock.UnixMilli()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO node_federated_inference_receipts(id,peer_node_id,remote_request_id,deployment_id,status,created_at) VALUES(?,?,?,?, 'executing',?)`, idv, peer, in.RequestID, in.DeploymentID, now); err != nil {
		return RemoteInferenceResponse{}, err
	}
	result, dispatchErr := exec.DispatchFederatedLocal(ctx, in.DeploymentID, in.RequestID, in.RequestJSON)
	completed := s.clock.UnixMilli()
	if dispatchErr != nil {
		known := true
		code := "remote_inference_failed"
		var te *inference.TransportError
		if errors.As(dispatchErr, &te) {
			known = te.OutcomeKnown
			if te.Code != "" {
				code = te.Code
			}
		}
		st := "failed"
		if !known {
			st = "unknown"
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE node_federated_inference_receipts SET status=?,error_code=?,completed_at=? WHERE id=?`, st, code, completed, idv)
		return RemoteInferenceResponse{ErrorCode: code, OutcomeKnown: known}, nil
	}
	if !json.Valid(result.ResponseJSON) {
		_, _ = s.db.ExecContext(ctx, `UPDATE node_federated_inference_receipts SET status='failed',error_code='invalid_remote_response',completed_at=? WHERE id=?`, completed, idv)
		return RemoteInferenceResponse{ErrorCode: "invalid_remote_response", OutcomeKnown: true}, nil
	}
	usageJSON := result.UsageJSON
	if len(usageJSON) == 0 {
		usageJSON = json.RawMessage(`{}`)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE node_federated_inference_receipts SET status='succeeded',response_json=?,usage_json=?,completed_at=? WHERE id=?`, string(result.ResponseJSON), string(usageJSON), completed, idv)
	if err != nil {
		return RemoteInferenceResponse{}, err
	}
	return RemoteInferenceResponse{ResponseJSON: result.ResponseJSON, UsageJSON: usageJSON, OutcomeKnown: true}, nil
}

func (s *Service) HeartbeatPeers(ctx context.Context) error {
	manifest, err := s.BuildManifest(ctx)
	if err != nil {
		return err
	}
	nodes, err := s.Nodes(ctx)
	if err != nil {
		return err
	}
	var first error
	for _, n := range nodes {
		if n.Local || n.TrustState != "paired" {
			continue
		}
		endpoint, fp, err := s.pairedEndpoint(ctx, n.ID)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		hb := Heartbeat{Protocol: ProtocolVersion, NodeID: s.local.ID, Manifest: manifest}
		var out any
		if err := postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/heartbeat", hb, &out); err != nil {
			if first == nil {
				first = fmt.Errorf("heartbeat %s: %w", n.ID, err)
			}
		}
	}
	return first
}

func (s *Service) RemoteModelManagementStatus(ctx context.Context, peer string) (ModelManagementGrant, error) {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return ModelManagementGrant{}, err
	}
	var out ModelManagementGrant
	if err := postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/management-status", map[string]any{}, &out); err != nil {
		return ModelManagementGrant{}, err
	}
	return out, nil
}

func (s *Service) RemoteModelRecommendations(ctx context.Context, peer string, req RemoteModelRecommendRequest) (localai.HardwareProfile, []localai.Recommendation, error) {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return localai.HardwareProfile{}, nil, err
	}
	var out struct {
		HardwareProfile localai.HardwareProfile  `json:"hardware_profile"`
		Recommendations []localai.Recommendation `json:"recommendations"`
	}
	if err := postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/recommend", req, &out); err != nil {
		return localai.HardwareProfile{}, nil, err
	}
	return out.HardwareProfile, out.Recommendations, nil
}

func (s *Service) RemoteInstallModel(ctx context.Context, peer string, req RemoteModelInstallRequest) (localai.InstallJob, error) {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return localai.InstallJob{}, err
	}
	var out localai.InstallJob
	if err := postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/install", req, &out); err != nil {
		return localai.InstallJob{}, err
	}
	return out, nil
}

func (s *Service) RemoteInstallJob(ctx context.Context, peer, jobID string) (localai.InstallJob, error) {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return localai.InstallJob{}, err
	}
	var out localai.InstallJob
	if err := postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/install-status", map[string]string{"job_id": jobID}, &out); err != nil {
		return localai.InstallJob{}, err
	}
	return out, nil
}

func (s *Service) RemoteModelSpecSheet(ctx context.Context, peer, deploymentID string) (localai.ModelSpecSheet, error) {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return localai.ModelSpecSheet{}, err
	}
	var out localai.ModelSpecSheet
	err = postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/spec-sheet", map[string]string{"deployment_id": deploymentID}, &out)
	return out, err
}
func (s *Service) RemoteStartModelTestbed(ctx context.Context, peer, deploymentID, notes string) (localai.TestbedSession, error) {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return localai.TestbedSession{}, err
	}
	var out localai.TestbedSession
	err = postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/testbed/start", map[string]string{"deployment_id": deploymentID, "notes": notes}, &out)
	return out, err
}

func (s *Service) RemoteModelTestbedSession(ctx context.Context, peer, sessionID string) (localai.TestbedSession, error) {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return localai.TestbedSession{}, err
	}
	var out localai.TestbedSession
	err = postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/testbed/session", map[string]string{"session_id": sessionID}, &out)
	return out, err
}

func (s *Service) RemoteModelTestbedTurns(ctx context.Context, peer, sessionID string) ([]localai.TestbedTurn, error) {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return nil, err
	}
	var out struct {
		Turns []localai.TestbedTurn `json:"turns"`
	}
	err = postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/testbed/turns", map[string]string{"session_id": sessionID}, &out)
	return out.Turns, err
}

func (s *Service) RemoteRunModelTestbedTurn(ctx context.Context, peer, sessionID string, cmd localai.TestbedTurnCommand) (localai.TestbedTurn, error) {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return localai.TestbedTurn{}, err
	}
	var out localai.TestbedTurn
	err = postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/testbed/turn", map[string]any{"session_id": sessionID, "command": cmd}, &out)
	return out, err
}
func (s *Service) RemoteCompleteModelTestbed(ctx context.Context, peer, sessionID string) error {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return err
	}
	var out any
	return postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/testbed/complete", map[string]string{"session_id": sessionID}, &out)
}
func (s *Service) RemoteAdmitModel(ctx context.Context, peer, deploymentID string, cmd localai.AdmissionCommand) (localai.ModelSpecSheet, error) {
	endpoint, fp, err := s.pairedEndpoint(ctx, peer)
	if err != nil {
		return localai.ModelSpecSheet{}, err
	}
	cmd.ActorPrincipalID = ""
	var out localai.ModelSpecSheet
	err = postJSON(ctx, s.pinnedClient(fp, true), endpoint+"/federation/v1/models/admit", map[string]any{"deployment_id": deploymentID, "command": cmd}, &out)
	return out, err
}
