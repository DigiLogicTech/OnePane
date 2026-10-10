package teamworker

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/agentprotocol"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
)

// councilTurnProvenance records deterministic fingerprints in the existing
// durable Council response message. It never includes raw prompts, model API
// credentials, private evidence packets or prior-seat content in the metadata.
// The evidence packet itself remains subject to the frozen session manifest
// and Council visibility controls.
type councilTurnProvenance struct {
 Version string `json:"version"`
 TimestampMS int64 `json:"timestamp_ms"`
 SessionManifestSHA256 string `json:"session_manifest_sha256"`
 ContextSHA256 string `json:"context_sha256"`
 AgentRequestSHA256 string `json:"agent_request_sha256"`
 DecodedResponseSHA256 string `json:"decoded_response_sha256"`
 CandidateMetadataSHA256 string `json:"candidate_metadata_sha256"`
 CandidateKind scheduler.CandidateKind `json:"candidate_kind"`
 CandidateID string `json:"candidate_id"`
 Provider string `json:"provider,omitempty"`
 RuntimeBackend string `json:"runtime_backend,omitempty"`
 ProfileID string `json:"profile_id"`
 ProfileRevision int64 `json:"profile_revision"`
 Round int64 `json:"round"`
 Phase string `json:"phase"`
 PinnedSeatCandidateID string `json:"pinned_seat_candidate_id,omitempty"`
}

func councilDigest(raw []byte)string{
 hash:=sha256.Sum256(raw)
 return hex.EncodeToString(hash[:])
}

func buildCouncilTurnProvenance(
 now int64, sessionSHA,contextSHA string,
 request agentprotocol.Request,response agentprotocol.Response,
 candidate scheduler.Candidate,profileID string,profileRevision int64,
 round int64,phase,pinnedCandidateID string,
)(councilTurnProvenance,error){
 if sessionSHA==""||contextSHA==""||candidate.ID==""||round<1||phase==""{
  return councilTurnProvenance{},fmt.Errorf("Research Council provenance requires a frozen manifest, context and resolved seat")
 }
 requestJSON,err:=json.Marshal(request)
 if err!=nil{return councilTurnProvenance{},err}
 responseJSON,err:=json.Marshal(response)
 if err!=nil{return councilTurnProvenance{},err}
 metadataJSON,err:=json.Marshal(candidate.Metadata)
 if err!=nil{return councilTurnProvenance{},err}
 if pinnedCandidateID!=""&&pinnedCandidateID!=candidate.ID{
  return councilTurnProvenance{},fmt.Errorf("resolved Council candidate disagrees with pinned seat")
 }
 return councilTurnProvenance{
  Version:"onepane.research-turn-provenance/v1",
  TimestampMS:now,
  SessionManifestSHA256:sessionSHA,
  ContextSHA256:contextSHA,
  AgentRequestSHA256:councilDigest(requestJSON),
  DecodedResponseSHA256:councilDigest(responseJSON),
  CandidateMetadataSHA256:councilDigest(metadataJSON),
  CandidateKind:candidate.Kind,
  CandidateID:candidate.ID,
  Provider:candidate.Provider,
  RuntimeBackend:candidate.RuntimeBackend,
  ProfileID:profileID,
  ProfileRevision:profileRevision,
  Round:round,Phase:phase,
  PinnedSeatCandidateID:pinnedCandidateID,
 },nil
}
