package nodefederation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/localai"
	"github.com/DigiLogicTech/OnePane/internal/observation"
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
	// Scheduling is not a durable authority: a peer can expire, revoke or
	// withdraw the advertised model after routing but before transport.
	// Fail before sending any prompt or starting a network request.
	if err := t.Service.authorizeRemoteModelDispatch(ctx,cfg.Federation.PeerNodeID,
		req.Deployment.ID,cfg.Federation.RemoteDeploymentID);err!=nil{
		return inference.DispatchResult{},&inference.TransportError{
			Code:"remote_node_not_admitted",OutcomeKnown:true,Err:err}
	}
	endpoint, fp, err := t.Service.pairedEndpoint(ctx, cfg.Federation.PeerNodeID)
	if err != nil {
		return inference.DispatchResult{}, &inference.TransportError{Code: "peer_endpoint_invalid", OutcomeKnown: true, Err: err}
	}
	in := RemoteInferenceRequest{Protocol: ProtocolVersion, RequestID: req.RequestID, DeploymentID: cfg.Federation.RemoteDeploymentID, RequestJSON: req.RequestJSON}
	var out RemoteInferenceResponse
	err = postJSON(ctx, t.Service.pinnedClient(fp, true), endpoint+"/federation/v1/inference", in, &out)
	if err != nil {
  return inference.DispatchResult{},classifyFederationTransportFailure(err)
 }
 return validateRemoteInferenceReply(out)
}

// Only serialization and admission-size errors are provably pre-network.
 // A TLS error, timeout, invalid acknowledgement or failed read MAY have
 // followed execution on the other Node and remains outcome-unknown.
func classifyFederationTransportFailure(err error)*inference.TransportError{
 if errors.Is(err,ErrFederationPayloadTooLarge){
  return &inference.TransportError{Code:"remote_payload_too_large",
   OutcomeKnown:true,Err:err}
 }
 if errors.Is(err,ErrFederationPayloadInvalid){
  return &inference.TransportError{Code:"remote_payload_invalid",
   OutcomeKnown:true,Err:err}
 }
 return &inference.TransportError{Code:"remote_transport_error",
  OutcomeKnown:false,Err:err}
}

// An authenticated Node response is still untrusted *data*. A 200 response
// containing {}, null, a contradictory status or invalid usage must never
// become an empty successful model completion. Its effect may already have
// occurred on the peer, so ambiguous acknowledgements remain unknown, not
// safe-to-retry failures.
func validateRemoteInferenceReply(out RemoteInferenceResponse)(
 inference.DispatchResult,error){
 invalid:=func()(inference.DispatchResult,error){
  return inference.DispatchResult{},&inference.TransportError{
   Code:"invalid_federation_reply",OutcomeKnown:false}
 }
 if out.ErrorCode!=""{
  if len(out.ErrorCode)>128||len(out.ResponseJSON)!=0||
   len(out.UsageJSON)!=0{return invalid()}
  for _,r:=range out.ErrorCode{
   if !((r>='a'&&r<='z')||(r>='A'&&r<='Z')||
    (r>='0'&&r<='9')||r=='_'||r=='-'||r=='.'){return invalid()}
  }
  return inference.DispatchResult{},&inference.TransportError{
   Code:out.ErrorCode,OutcomeKnown:out.OutcomeKnown}
 }
 if !out.OutcomeKnown||classifyFederatedOutput(inference.DispatchResult{
  ResponseJSON:out.ResponseJSON,UsageJSON:out.UsageJSON})!=""{
  return invalid()
 }
 return inference.DispatchResult{
  ResponseJSON:out.ResponseJSON,UsageJSON:out.UsageJSON},nil
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
	s.mux.HandleFunc("POST /federation/v1/measurements", s.receiveNodeMeasurement)
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
	s.mux.HandleFunc("POST /federation/v1/models/testbed/abort", s.remoteModelTestbedAbort)
	s.mux.HandleFunc("POST /federation/v1/models/admit", s.remoteModelAdmit)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
 // Every model, pairing and management request is a single, size-bounded
 // unambiguous JSON document. Decode-only admission silently accepted
 // repeated keys (last wins), including escaped field-name aliases.
 raw,err:=io.ReadAll(http.MaxBytesReader(w,r.Body,4<<20))
 if err!=nil||observation.ValidateUnambiguousJSON(raw)!=nil{
  writeJSON(w,400,map[string]string{"error":"invalid request"})
  return false
 }
 d:=json.NewDecoder(bytes.NewReader(raw))
 d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return false
	}
	// A federation call is exactly one JSON document. Decoder.Decode
	// otherwise accepts and silently ignores subsequent JSON values.
	// Require EOF after optional whitespace, under the same 4 MiB cap.
	var extra json.RawMessage
	if err := d.Decode(&extra); err != io.EOF {
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

// tls.RequestClientCert collects a certificate without Go validating its
// dates or permitted role. Check these properties before a paired fingerprint
// can authorise inbound inference or remote model management.
func validatePairedNodeClientCertificate(cert *x509.Certificate,now time.Time)error{
 if cert==nil||len(cert.Raw)==0||
  cert.IsCA||cert.KeyUsage&x509.KeyUsageCertSign!=0||
  cert.KeyUsage&x509.KeyUsageDigitalSignature==0||
  now.Before(cert.NotBefore)||!now.Before(cert.NotAfter){
  return ErrPeerCertificate
 }
 clientAuth:=false
 for _,usage:=range cert.ExtKeyUsage{
  if usage==x509.ExtKeyUsageClientAuth{clientAuth=true;break}
 }
 if !clientAuth{return ErrPeerCertificate}
 return nil
}

func (s *Service) peerFromTLS(r *http.Request) (string, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
		return "", ErrPeerCertificate
	}
	if err:=validatePairedNodeClientCertificate(r.TLS.PeerCertificates[0],time.Now());err!=nil{
		return "",err
	}
	fp := CertificateFingerprint(r.TLS.PeerCertificates[0].Raw)
	var peer, state, stored string
	var localConfirmed,peerConfirmed int
	err := s.db.QueryRowContext(r.Context(), `SELECT np.peer_node_id,hn.trust_state,np.peer_tls_fingerprint,
	 np.local_confirmed,np.peer_confirmed
	 FROM node_pairings np JOIN harness_nodes hn ON hn.id=np.peer_node_id
	 WHERE np.status='paired' AND hn.local=0 AND np.peer_tls_fingerprint=?`, fp).
	 Scan(&peer, &state, &stored, &localConfirmed, &peerConfirmed)
	if err != nil {
		return "", ErrPeerCertificate
	}
	if (state != "paired" && state != "unavailable") || stored != fp ||
	 localConfirmed!=1 || peerConfirmed!=1 {
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
// Aborting a failed remote Agent Check does not affect other workloads.
func (s *Server) remoteModelTestbedAbort(w http.ResponseWriter, r *http.Request) {
 if _,ok:=s.authorizeModelManagement(w,r);!ok{return}
 var in struct{SessionID string `json:"session_id"`;Reason string `json:"reason"`}
 if !decode(w,r,&in){return}
 manager,ok:=s.models.(interface{AbortTestbed(context.Context,string,string) error})
 if !ok{writeJSON(w,503,map[string]string{"error":"testbed abort unavailable"});return}
 if err:=manager.AbortTestbed(r.Context(),strings.TrimSpace(in.SessionID),in.Reason);err!=nil{
  writeJSON(w,409,map[string]string{"error":err.Error()});return
 }
 writeJSON(w,200,map[string]any{"aborted":true})
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

func (s *Service) peerAdmittedForInboundInference(ctx context.Context,peer string)bool{
	if s==nil||s.db==nil||s.clock==nil||peer==""{return false}
	var eligible int
	err:=s.db.QueryRowContext(ctx,`SELECT EXISTS(
	 SELECT 1 FROM harness_nodes n
	 JOIN node_pairings p ON p.peer_node_id=n.id
	 JOIN node_capability_manifests m ON m.peer_node_id=n.id
	 WHERE n.id=? AND n.local=0 AND n.trust_state='paired'
	  AND p.status='paired' AND p.local_confirmed=1 AND p.peer_confirmed=1
	  AND m.expires_at>? AND m.sequence>0 AND m.received_at>0
	  AND json_valid(m.manifest_json)=1
	  AND json_extract(m.manifest_json,'$.node_id')=n.id
	  AND json_extract(m.manifest_json,'$.protocol')=?)`,peer,s.clock.UnixMilli(),ProtocolVersion).Scan(&eligible)
	return err==nil&&eligible==1
}

func (s *Server) remoteInference(w http.ResponseWriter, r *http.Request) {
	peer, err := s.svc.peerFromTLS(r)
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": "unpaired client certificate"})
		return
	}
	if !s.svc.peerAdmittedForInboundInference(r.Context(),peer){
		writeJSON(w, 403, map[string]string{"error": "peer Node capability heartbeat is unavailable"})
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
	// An unavailable or uninitialized local model manager must never
	// implicitly authorise a paired peer to run an arbitrary deployment.
	if s.models == nil || !s.models.FederatedInferenceAllowed(r.Context(), in.DeploymentID) {
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

// inboundRequestHash includes the resolved model and all request context
// fields. Only the digest is persisted; no raw prompt is added to receipts.
// A historical receipt lacking this digest is deliberately non-replayable.
func inboundRequestHash(in RemoteInferenceRequest)(string,error){
 raw,err:=json.Marshal(struct{
  DeploymentID string          `json:"deployment_id"`
  Request json.RawMessage      `json:"request_json"`
  Context json.RawMessage      `json:"context_manifest,omitempty"`
  Classification json.RawMessage `json:"classification_json,omitempty"`
 }{in.DeploymentID,in.RequestJSON,in.ContextManifest,in.ClassificationJSON})
 if err!=nil||in.DeploymentID==""||!json.Valid(in.RequestJSON){
  return "",fmt.Errorf("invalid federated request identity")
 }
 sum:=sha256.Sum256(raw)
 return hex.EncodeToString(sum[:]),nil
}

// Keep one-shot inference payloads below the current four-MiB federation
// response transport bound, with a separate small metadata budget. This is
// not a substitute for a future chunked/streamed federation protocol.
func classifyFederatedOutput(r inference.DispatchResult)string{
 if len(r.ResponseJSON)==0||len(r.ResponseJSON)>3<<20||
  !json.Valid(r.ResponseJSON){return "invalid_remote_response"}
 if len(r.UsageJSON)>64<<10||
  (len(r.UsageJSON)>0&&!json.Valid(r.UsageJSON)){
  return "invalid_remote_usage"
 }
 return ""
}

// ErrFederatedReceiptConflict means an already executing operation did not
// successfully commit its exact terminal receipt. Do not retry that effect.
var ErrFederatedReceiptConflict=errors.New("federated inference receipt is not in the original executing state")

// Inbound inference may complete AFTER its HTTP request has been cancelled.
// A bounded, cancellation-independent receipt write prevents completed work
// from becoming a permanent 'executing' tombstone due solely to cancellation.
// It never retries or launches the model: only the result of the original
// invocation may be written, and DB failure is propagated for reconciliation.
func (s *Service)finishInboundInferenceReceipt(ctx context.Context,
 query string,args ...any)error{
 cleanup,cancel:=context.WithTimeout(context.WithoutCancel(ctx),3*time.Second)
 defer cancel()
 result,err:=s.db.ExecContext(cleanup,query,args...)
 if err!=nil{return err}
 changed,err:=result.RowsAffected()
 if err!=nil{return err}
 if changed!=1{return ErrFederatedReceiptConflict}
 return nil
}

func (s *Service) executeInbound(ctx context.Context,peer string,in RemoteInferenceRequest,exec LocalInferenceExecutor)(RemoteInferenceResponse,error){
 digest,err:=inboundRequestHash(in)
 if err!=nil{return RemoteInferenceResponse{},err}
 // The unique (peer, request ID) constraint arbitrates even *concurrent*
 // requests; no second caller may execute a different model/prompt under
 // the same idempotency key.
 idv,err:=s.ids.New("fedrecv")
 if err!=nil{return RemoteInferenceResponse{},err}
 now:=s.clock.UnixMilli()
 inserted,err:=s.db.ExecContext(ctx,`INSERT OR IGNORE INTO node_federated_inference_receipts(
  id,peer_node_id,remote_request_id,deployment_id,request_hash,status,created_at)
  VALUES(?,?,?,?,?,'executing',?)`,idv,peer,in.RequestID,in.DeploymentID,digest,now)
 if err!=nil{return RemoteInferenceResponse{},err}
 n,err:=inserted.RowsAffected()
 if err!=nil{return RemoteInferenceResponse{},err}
 if n==0{
  var status,recordedModel,recordedDigest string
  var response,usage,errorCode sql.NullString
  if err:=s.db.QueryRowContext(ctx,`SELECT status,deployment_id,request_hash,
   response_json,usage_json,error_code FROM node_federated_inference_receipts
   WHERE peer_node_id=? AND remote_request_id=?`,peer,in.RequestID).
   Scan(&status,&recordedModel,&recordedDigest,&response,&usage,&errorCode);err!=nil{
   return RemoteInferenceResponse{},err
  }
  if recordedDigest==""||recordedDigest!=digest||recordedModel!=in.DeploymentID{
   return RemoteInferenceResponse{ErrorCode:"remote_request_identity_conflict",OutcomeKnown:true},nil
  }
  if status=="succeeded"{
   // A redacted, truncated or corrupt success receipt is a known past
   // execution but NOT a replayable success. Never run the model again.
   if response.Valid&&json.Valid([]byte(response.String))&&
    (!usage.Valid||json.Valid([]byte(usage.String))){
    out:=RemoteInferenceResponse{ResponseJSON:json.RawMessage(response.String),OutcomeKnown:true}
    if usage.Valid {out.UsageJSON=json.RawMessage(usage.String)}
    return out,nil
   }
   code:="remote_cached_response_unavailable"
   if errorCode.String=="remote_cached_response_expired"{
    code=errorCode.String
   }
   return RemoteInferenceResponse{ErrorCode:code,OutcomeKnown:true},nil
  }
  if status=="executing"||status=="unknown"{
   return RemoteInferenceResponse{ErrorCode:"duplicate_or_unknown_remote_request",OutcomeKnown:false},nil
  }
  if status=="failed"&&errorCode.Valid&&errorCode.String!=""{
   return RemoteInferenceResponse{ErrorCode:errorCode.String,OutcomeKnown:true},nil
  }
  // A failed or otherwise unexpected receipt without an error code must
  // not be returned as an empty successful inference either.
  return RemoteInferenceResponse{ErrorCode:"remote_receipt_unavailable",OutcomeKnown:true},nil
 }
 result,dispatchErr:=exec.DispatchFederatedLocal(ctx,in.DeploymentID,in.RequestID,in.RequestJSON)
 completed:=s.clock.UnixMilli()
 if dispatchErr!=nil{
  known:=true
  code:="remote_inference_failed"
  var te *inference.TransportError
  if errors.As(dispatchErr,&te){
   known=te.OutcomeKnown
   if te.Code!=""{code=te.Code}
  }else if errors.Is(dispatchErr,context.Canceled)||
   errors.Is(dispatchErr,context.DeadlineExceeded){
   // Request cancellation cannot prove that inference did not happen.
   known=false
   code="remote_inference_outcome_unknown"
  }
  st:="failed"
  if !known{st="unknown"}
  if err:=s.finishInboundInferenceReceipt(ctx,`UPDATE node_federated_inference_receipts
   SET status=?,error_code=?,completed_at=?
   WHERE id=? AND request_hash=? AND status='executing'
    AND peer_node_id=? AND remote_request_id=? AND deployment_id=?`,
   st,code,completed,idv,digest,peer,in.RequestID,in.DeploymentID);err!=nil{
   return RemoteInferenceResponse{},err
  }
  return RemoteInferenceResponse{ErrorCode:code,OutcomeKnown:known},nil
 }
 code:=classifyFederatedOutput(result)
 if code!=""{
  if err:=s.finishInboundInferenceReceipt(ctx,`UPDATE node_federated_inference_receipts
   SET status='failed',error_code=?,completed_at=?
   WHERE id=? AND request_hash=? AND status='executing'
    AND peer_node_id=? AND remote_request_id=? AND deployment_id=?`,
   code,completed,idv,digest,peer,in.RequestID,in.DeploymentID);err!=nil{
   return RemoteInferenceResponse{},err
  }
  return RemoteInferenceResponse{ErrorCode:code,OutcomeKnown:true},nil
 }
 usageJSON:=result.UsageJSON
 if len(usageJSON)==0{usageJSON=json.RawMessage(`{}`)}
 err=s.finishInboundInferenceReceipt(ctx,`UPDATE node_federated_inference_receipts
  SET status='succeeded',response_json=?,usage_json=?,completed_at=?
  WHERE id=? AND request_hash=? AND status='executing'
   AND peer_node_id=? AND remote_request_id=? AND deployment_id=?`,
  string(result.ResponseJSON),string(usageJSON),completed,idv,digest,
  peer,in.RequestID,in.DeploymentID)
 if err!=nil{return RemoteInferenceResponse{},err}
 return RemoteInferenceResponse{ResponseJSON:result.ResponseJSON,UsageJSON:usageJSON,OutcomeKnown:true},nil
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
func (s *Service) RemoteAbortModelTestbed(ctx context.Context, peer, sessionID, reason string) error {
 endpoint,fp,err:=s.pairedEndpoint(ctx,peer);if err!=nil{return err}
 var out any
 return postJSON(ctx,s.pinnedClient(fp,true),endpoint+"/federation/v1/models/testbed/abort",map[string]string{"session_id":sessionID,"reason":reason},&out)
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
