package teamworker

import (
 "encoding/json"
 "errors"

 "github.com/DigiLogicTech/OnePane/internal/agentprotocol"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
)

// researchTurnEvidenceProof is a small, model-input-bound audit record. It
// never retains filenames, source text, artifact IDs or storage locations.
type researchTurnEvidenceProof struct{
 ContextAttached bool
 ConsentSHA256 string
 ManifestSHA256 string
 DocumentCount int
 VerifiedBytes int64
}

// proveResearchTurnEvidence verifies the exact evidence sections and their
// declared content hashes before dispatch and before durable provenance
// capture. False or missing flags with plaintext sections fail closed.
func proveResearchTurnEvidence(req agentprotocol.Request,candidate scheduler.Candidate)(
 researchTurnEvidenceProof,error){
 zero:=researchTurnEvidenceProof{}
 var manifest struct {
  Attached bool `json:"research_evidence_content_delivered"`
  ConsentSHA string `json:"research_evidence_consent_sha256"`
  ManifestSHA string `json:"research_evidence_manifest_sha256"`
  Count int `json:"research_evidence_document_count"`
  Bytes int64 `json:"research_evidence_verified_bytes"`
 }
 if len(req.ContextManifest)==0||json.Unmarshal(req.ContextManifest,&manifest)!=nil{
  return zero,errors.New("Research evidence request manifest is invalid")
 }
 count:=0
 total:=int64(0)
 for _,section:=range req.Context{
  if section.Kind!="research_evidence"{continue}
  count++
  if !manifest.Attached||section.Authoritative||section.Trust!="UNVERIFIED_DERIVED"{
   return zero,errors.New("research evidence context is missing the untrusted-data fence")
  }
  var body struct{
   AssetID string `json:"asset_id"`
   Version int64 `json:"version"`
   SHA256 string `json:"sha256"`
   Text string `json:"text"`
   Trust string `json:"trust"`
  }
  if json.Unmarshal(section.Content,&body)!=nil||body.AssetID==""||
   body.Version<1||body.Trust!="untrusted_library_content"||
   body.SHA256==""||body.SHA256!="sha256:"+councilDigest([]byte(body.Text)){
   return zero,errors.New("research evidence content integrity or trust mismatch")
  }
  size:=int64(len(body.Text))
  if size>researchSeatDeliveryMaxFileBytes{return zero,errors.New("research evidence file exceeds size cap")}
  total+=size
  if total>researchSeatDeliveryMaxTotalBytes{return zero,errors.New("research evidence context exceeds cap")}
 }
 if !manifest.Attached{
  if count!=0||manifest.ConsentSHA!=""||manifest.ManifestSHA!=""||
   manifest.Count!=0||manifest.Bytes!=0{
   return zero,errors.New("Research evidence manifest claims consent without attached, verified bytes")
  }
  return zero,nil
 }
 if !researchEvidenceCandidateAllowed(candidate)||
  manifest.ConsentSHA==""||manifest.ManifestSHA==""||
  count==0||count>researchSeatDeliveryMaxEntries||
  count!=manifest.Count||total!=manifest.Bytes||
  total>researchSeatDeliveryMaxTotalBytes{
  return zero,errors.New("Research evidence model destination or manifest contradicts verified input")
 }
 return researchTurnEvidenceProof{
  ContextAttached:true,ConsentSHA256:manifest.ConsentSHA,
  ManifestSHA256:manifest.ManifestSHA,DocumentCount:count,VerifiedBytes:total,
 },nil
}
