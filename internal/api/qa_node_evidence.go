package api

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
)

// RC11 admin-only Node evidence: the durable control-plane observations,
// not a fabricated live OS/service check. We never select node names,
// endpoints, certificates, pairing secrets, error text, arbitrary manifests,
// wake targets, task identifiers or remote inference content.
type qaNodeEvidence struct {
 SchemaVersion int `json:"schema_version"`
 BackendReadiness *qaBackendReadiness `json:"backend_readiness,omitempty"`
 Scope string `json:"scope"`
 NodeRef string `json:"node_ref"`
 TrustState string `json:"recorded_trust_state"`
 IsLocal bool `json:"registered_local_node"`
 LastSeenAt *int64 `json:"last_seen_at_ms,omitempty"`
 UpdatedAt int64 `json:"node_record_updated_at_ms"`
 ManifestState string `json:"manifest_state"`
 ManifestSequence *int64 `json:"manifest_sequence,omitempty"`
 ManifestReceivedAt *int64 `json:"manifest_received_at_ms,omitempty"`
 ManifestExpiresAt *int64 `json:"manifest_expires_at_ms,omitempty"`
 PairingStatus string `json:"pairing_status"`
 WakeAttempts qaNodeWakeSummary `json:"wake_attempts"`
 InferenceReceipts qaNodeInferenceSummary `json:"inference_receipts"`
 ServiceState string `json:"operating_system_service_state"`
 ServiceObservation *qaOSServiceObservation `json:"local_service_observation,omitempty"`
 CollectionLimits []string `json:"collection_limits"`
}

type qaNodeWakeSummary struct {
 TotalRecorded int64 `json:"total_recorded"`
 Ready int64 `json:"ready"`
 Failed int64 `json:"failed"`
 TimedOut int64 `json:"timed_out"`
 InProgress int64 `json:"in_progress"`
 LastKnownState string `json:"last_recorded_state"`
}

type qaNodeInferenceSummary struct {
 TotalRecorded int64 `json:"total_recorded"`
 Succeeded int64 `json:"succeeded"`
 Failed int64 `json:"failed"`
 Unknown int64 `json:"unknown"`
 Executing int64 `json:"executing"`
}

func qaNodeTrust(s string)string{
 switch s {
 case "local","discovered","pairing","paired","revoked","unavailable":return s
 default:return "unavailable"
 }
}
func qaNodePairing(s string)string{
 switch s {
 case "requested","pairing","paired","rejected","revoked","expired","failed":return s
 default:return "not_recorded"
 }
}
func qaNodeWakeState(s string)string{
 switch s{
 case "requested","wake_sent","reconnecting","preparing","ready","failed","timed_out":return s
 default:return "not_recorded"
 }
}

// Selection is strictly keyed by the admin-authorized canonical Node ID.
// All subordinate SQL queries use the same literal bound ID. Aggregated
// counts are output; no arbitrary error, task, identity, endpoint or payload
// strings leave this projector.
func loadQANodeEvidence(ctx context.Context,db *sql.DB,nodeID string)(qaNodeEvidence,error){
 if db==nil||nodeID==""{return qaNodeEvidence{},errors.New("Node QA source unavailable")}
 out:=qaNodeEvidence{
  SchemaVersion:3,Scope:"admin_authorised_local_control_plane",
  NodeRef:qaOpaqueRef("node",nodeID),
  ManifestState:"not_recorded",PairingStatus:"not_recorded",
  ServiceState:"not_collected",
  WakeAttempts:qaNodeWakeSummary{LastKnownState:"not_recorded"},
  CollectionLimits:[]string{
   "control-plane database records only; not a live remote Node probe",
   "manifest presence and expiry are not proof of current capabilities",
   "last-seen is a recorded timestamp, not proof a Node is online now",
   "wake and inference receipt totals are historical, not runtime health",
   "remote OS services, drivers, models, disks, thermal and GPU/CPU health are not collected",
   "local OS service-manager state is queried on demand only for the canonical local Node",
   "local backend readiness is on-demand and does not attest to complete application operation",
   "network endpoints, identities, pairing material, raw errors, logs and model outputs are omitted",
  },
 }
 var isLocal int64
 var trust string
 var seen sql.NullInt64
 if err:=db.QueryRowContext(ctx,`SELECT local,trust_state,last_seen_at,updated_at
 FROM harness_nodes WHERE id=?`,nodeID).Scan(&isLocal,&trust,&seen,&out.UpdatedAt);err!=nil{return qaNodeEvidence{},err}
 out.IsLocal=isLocal==1
 out.TrustState=qaNodeTrust(trust)
 if seen.Valid&&seen.Int64>0{v:=seen.Int64;out.LastSeenAt=&v}

 // Only the immutable control-plane metadata for a manifest; never select
 // the full manifest payload, which may contain private host or device data.
 var seq,received,expires sql.NullInt64
 err:=db.QueryRowContext(ctx,`SELECT sequence,received_at,expires_at
 FROM node_capability_manifests WHERE peer_node_id=?`,nodeID).Scan(&seq,&received,&expires)
 if err!=nil&&!errors.Is(err,sql.ErrNoRows){return qaNodeEvidence{},fmt.Errorf("read Node manifest metadata: %w",err)}
 if err==nil{
  out.ManifestState="recorded_expiry_unverified"
  if seq.Valid&&seq.Int64>0{v:=seq.Int64;out.ManifestSequence=&v}
  if received.Valid&&received.Int64>0{v:=received.Int64;out.ManifestReceivedAt=&v}
  if expires.Valid&&expires.Int64>0{v:=expires.Int64;out.ManifestExpiresAt=&v}
 }
 err=db.QueryRowContext(ctx,`SELECT status FROM node_pairings WHERE peer_node_id=?`,nodeID).Scan(&trust)
 if err!=nil&&!errors.Is(err,sql.ErrNoRows){return qaNodeEvidence{},fmt.Errorf("read Node pairing status: %w",err)}
 if err==nil{out.PairingStatus=qaNodePairing(trust)}

 // Failure field is deliberately not selected. These counts describe the
 // stored historical outcomes, not whether a magic packet succeeded today.
 err=db.QueryRowContext(ctx,`SELECT COUNT(*),
 COALESCE(SUM(CASE WHEN state='ready' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN state='failed' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN state='timed_out' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN state IN ('requested','wake_sent','reconnecting','preparing') THEN 1 ELSE 0 END),0)
 FROM node_wake_attempts WHERE node_id=?`,nodeID).Scan(
 &out.WakeAttempts.TotalRecorded,&out.WakeAttempts.Ready,&out.WakeAttempts.Failed,
 &out.WakeAttempts.TimedOut,&out.WakeAttempts.InProgress)
 if err!=nil{return qaNodeEvidence{},fmt.Errorf("read Node wake outcomes: %w",err)}
 err=db.QueryRowContext(ctx,`SELECT state FROM node_wake_attempts
 WHERE node_id=? ORDER BY requested_at DESC,id DESC LIMIT 1`,nodeID).Scan(&trust)
 if err!=nil&&!errors.Is(err,sql.ErrNoRows){return qaNodeEvidence{},fmt.Errorf("read latest Node wake state: %w",err)}
 if err==nil{out.WakeAttempts.LastKnownState=qaNodeWakeState(trust)}

 // Model outputs, raw request IDs and free-text remote failures are intentionally
 // absent from the SQL projection.
 err=db.QueryRowContext(ctx,`SELECT COUNT(*),
 COALESCE(SUM(CASE WHEN status='succeeded' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN status='unknown' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN status='executing' THEN 1 ELSE 0 END),0)
 FROM node_federated_inference_receipts WHERE peer_node_id=?`,nodeID).Scan(
 &out.InferenceReceipts.TotalRecorded,&out.InferenceReceipts.Succeeded,
 &out.InferenceReceipts.Failed,&out.InferenceReceipts.Unknown,
 &out.InferenceReceipts.Executing)
 if err!=nil{return qaNodeEvidence{},fmt.Errorf("read Node inference outcomes: %w",err)}
 return out,nil
}
