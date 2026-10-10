package teamworker

import (
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/agentprotocol"
 "github.com/DigiLogicTech/OnePane/internal/policy"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
)

func researchAuditFixture()(agentprotocol.Request,scheduler.Candidate){
 secret:="Private evidence line 1\nUntrusted line 2."
 body,_:=json.Marshal(map[string]any{
  "asset_id":"assetA","version":int64(3),
  "sha256":"sha256:"+councilDigest([]byte(secret)),
  "text":secret,"trust":"untrusted_library_content",
 })
 context,_:=json.Marshal(map[string]any{
  "research_evidence_content_delivered":true,
  "research_evidence_consent_sha256":"sha256:approved-consent",
  "research_evidence_manifest_sha256":"sha256:frozen-manifest",
  "research_evidence_document_count":1,
  "research_evidence_verified_bytes":len(secret),
 })
 req:=agentprotocol.Request{
  ProtocolVersion:agentprotocol.Version,WorkspaceID:"tenant",TaskID:"task",
  PrincipalID:WorkerPrincipal,Role:"Researcher",Objective:"Evaluate evidence",
  Context:[]agentprotocol.ContextSection{{
   ID:"research-evidence-assetA-v3",Kind:"research_evidence",
   Trust:"UNVERIFIED_DERIVED",Authoritative:false,Content:body,
  }},
  ContextManifest:context,Constraints:json.RawMessage(`{"no_side_effects":true}`),
  PermittedProposalTypes:[]agentprotocol.ProposalType{agentprotocol.ProposalComplete},
 }
 cand:=scheduler.Candidate{
  ID:"local-seat",Kind:scheduler.CandidateModel,
  Local:true,NodeID:"origin",ComputeMode:"cpu",
  CostClass:scheduler.CostLocal,HardZeroIncrementalCost:true,
  Destination:policy.FlowDestination{Kind:policy.DestinationOriginNode},
 }
 return req,cand
}

func TestResearchEvidenceProvenanceIsBoundAndRedacted(t *testing.T){
 req,cand:=researchAuditFixture()
 got,err:=proveResearchTurnEvidence(req,cand)
 if err!=nil{t.Fatal(err)}
 if !got.ContextAttached||got.DocumentCount!=1||
  got.VerifiedBytes<=0||got.ManifestSHA256!="sha256:frozen-manifest"{
  t.Fatalf("Research evidence proof omitted metadata: %+v",got)
 }
 result,err:=buildCouncilTurnProvenance(42,"session-sha","context-sha",
  req,agentprotocol.Response{Message:"Result"},cand,"researcher",1,1,
  "independent",cand.ID)
 if err!=nil{t.Fatal(err)}
 if !result.ResearchEvidenceAttached||
  result.ResearchEvidenceConsentSHA256!=got.ConsentSHA256||
  result.ResearchEvidenceManifestSHA256!=got.ManifestSHA256||
  result.ResearchEvidenceDocumentCount!=got.DocumentCount||
  result.ResearchEvidenceVerifiedBytes!=got.VerifiedBytes||
  result.Version!="onepane.research-turn-provenance/v2"{
  t.Fatalf("provenance omitted verified Research context: %+v",result)
 }
 raw,_:=json.Marshal(result)
 if strings.Contains(string(raw),"Private evidence")||
  strings.Contains(string(raw),"assetA")||
  strings.Contains(string(raw),"Untrusted line 2"){
  t.Fatalf("Research audit leaked private material: %s",raw)
 }
}

func TestResearchEvidenceProvenanceFailsClosedForContextForgery(t *testing.T){
 cases:=[]struct{
  name string
  mutate func(*agentprotocol.Request,*scheduler.Candidate)
 }{
  {"cloud",func(_ *agentprotocol.Request,c *scheduler.Candidate){c.Local=false}},
  {"external_destination",func(_ *agentprotocol.Request,c *scheduler.Candidate){c.Destination.Kind=policy.DestinationCloud}},
  {"remote",func(_ *agentprotocol.Request,c *scheduler.Candidate){c.ComputeMode="remote"}},
  {"runtime",func(_ *agentprotocol.Request,c *scheduler.Candidate){c.Kind=scheduler.CandidateAgentRuntime}},
  {"system_instructions",func(r *agentprotocol.Request,_ *scheduler.Candidate){r.Context[0].Trust="USER_INSTRUCTION"}},
  {"authoritative",func(r *agentprotocol.Request,_ *scheduler.Candidate){r.Context[0].Authoritative=true}},
  {"tamper_text",func(r *agentprotocol.Request,_ *scheduler.Candidate){
   var body map[string]any
   _=json.Unmarshal(r.Context[0].Content,&body)
   body["text"]="changed"
   r.Context[0].Content,_=json.Marshal(body)
  }},
  {"missing_evidence",func(r *agentprotocol.Request,_ *scheduler.Candidate){r.Context=nil}},
  {"missing_manifest",func(r *agentprotocol.Request,_ *scheduler.Candidate){r.ContextManifest=json.RawMessage(`{}`)}},
  {"incorrect_document_count",func(r *agentprotocol.Request,_ *scheduler.Candidate){
   var m map[string]any;_=json.Unmarshal(r.ContextManifest,&m)
   m["research_evidence_document_count"]=2
   r.ContextManifest,_=json.Marshal(m)
  }},
  {"wrong_verified_size",func(r *agentprotocol.Request,_ *scheduler.Candidate){
   var m map[string]any;_=json.Unmarshal(r.ContextManifest,&m)
   m["research_evidence_verified_bytes"]=100
   r.ContextManifest,_=json.Marshal(m)
  }},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){
  req,cand:=researchAuditFixture()
  tc.mutate(&req,&cand)
  v,e:=proveResearchTurnEvidence(req,cand)
  if e==nil||v.ContextAttached{t.Fatalf("tampered Research context accepted: %+v %v",v,e)}
 })}
}
