package teamworker

import (
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/agentprotocol"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
)

func TestResearchCouncilTurnProvenanceHashesFrozenContextAndResponse(t *testing.T){
 req:=agentprotocol.Request{
  ProtocolVersion:agentprotocol.Version,WorkspaceID:"tenant",TaskID:"research-task",
  PrincipalID:WorkerPrincipal,Role:"Researcher",Objective:"Study protein folding",
  Constraints:json.RawMessage(`{"no_side_effects":true}`),
  Context:[]agentprotocol.ContextSection{{ID:"evidence",Kind:"evidence",Trust:"USER_INSTRUCTION",
   Content:json.RawMessage(`{"private_source":"scoped user evidence"}`)}},
  ContextManifest:json.RawMessage(`{"context_sha256":"cafe"}`),
 }
 response:=agentprotocol.Response{ProtocolVersion:agentprotocol.Version,
  ProposalType:agentprotocol.ProposalComplete,Message:"Evidence supports hypothesis A.",
  Proposal:json.RawMessage(`{"result":"A"}`),
  Usage:json.RawMessage(`{"total_tokens":42}`),
 }
 cand:=scheduler.Candidate{
  ID:"qualified-seat",Kind:scheduler.CandidateModel,Provider:"local",
  RuntimeBackend:"llama.cpp",
  Metadata:map[string]any{"model_ref":"example/model:v2"},
 }
 v,err:=buildCouncilTurnProvenance(1700000000000,"frozen-session-sha","scoped-evidence-sha",
  req,response,cand,"research.profile",4,1,"independent","qualified-seat")
 if err!=nil{t.Fatal(err)}
 if v.AgentRequestSHA256==""||v.DecodedResponseSHA256==""||
  v.CandidateMetadataSHA256==""||v.ContextSHA256!="scoped-evidence-sha"||
  v.SessionManifestSHA256!="frozen-session-sha"||
  v.CandidateID!="qualified-seat"||v.PinnedSeatCandidateID!="qualified-seat"||
  v.ProfileRevision!=4{
  t.Fatalf("Council provenance omitted required fingerprints: %+v",v)
 }
 raw,err:=json.Marshal(v)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(raw),"scoped user evidence")||
  strings.Contains(string(raw),"Study protein folding")||
  strings.Contains(string(raw),"Evidence supports hypothesis A."){
  t.Fatalf("provenance metadata leaked raw private Research content: %s",raw)
 }
 second,err:=buildCouncilTurnProvenance(1700000000000,"frozen-session-sha","scoped-evidence-sha",
  req,response,cand,"research.profile",4,1,"independent","qualified-seat")
 if err!=nil{t.Fatal(err)}
 if second.AgentRequestSHA256!=v.AgentRequestSHA256||second.DecodedResponseSHA256!=v.DecodedResponseSHA256{
  t.Fatal("frozen inputs produced nondeterministic evidence hashes")
 }
 response.Message="Evidence supports hypothesis B."
 modified,err:=buildCouncilTurnProvenance(1700000000000,"frozen-session-sha","scoped-evidence-sha",
  req,response,cand,"research.profile",4,1,"independent","qualified-seat")
 if err!=nil{t.Fatal(err)}
 if modified.DecodedResponseSHA256==v.DecodedResponseSHA256{t.Fatal("tampered response retained original digest")}
 req.Objective="Different research task"
 changed,err:=buildCouncilTurnProvenance(1700000000000,"frozen-session-sha","scoped-evidence-sha",
  req,response,cand,"research.profile",4,1,"independent","qualified-seat")
 if err!=nil{t.Fatal(err)}
 if changed.AgentRequestSHA256==v.AgentRequestSHA256{t.Fatal("changed Council request retained original digest")}
}

func TestResearchCouncilRefusesContradictoryPinnedSeatProvenance(t *testing.T){
 req:=agentprotocol.Request{ProtocolVersion:agentprotocol.Version,WorkspaceID:"tenant"}
 resp:=agentprotocol.Response{Message:"ok"}
 cand:=scheduler.Candidate{ID:"seat-A",Kind:scheduler.CandidateModel}
 if _,err:=buildCouncilTurnProvenance(1,"session-sha","context-sha",req,resp,cand,
  "profile",1,2,"critique","seat-B");err==nil{
  t.Fatal("resolved model substitution was accepted into provenance")
 }
 if _,err:=buildCouncilTurnProvenance(1,"","context-sha",req,resp,cand,
  "profile",1,2,"critique","seat-A");err==nil{
  t.Fatal("Research provenance accepted an unbound session manifest")
 }
}
