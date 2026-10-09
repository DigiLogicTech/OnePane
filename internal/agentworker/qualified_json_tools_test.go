package agentworker

import (
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/agentprotocol"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
)

func TestJSONToolProposalsRequireEmpiricallyQualifiedStructuredModel(t *testing.T){
 cases:=[]struct{
  name string
  candidate scheduler.Candidate
  allowTool,structured bool
 }{
  {"local_l1_verified",scheduler.Candidate{Kind:scheduler.CandidateModel,Local:true,Qualification:scheduler.QualVerified,ProtocolLevel:"L1"},true,true},
  {"local_l2_limited",scheduler.Candidate{Kind:scheduler.CandidateModel,Local:true,Qualification:scheduler.QualLimited,ProtocolLevel:"L2"},true,true},
  {"cloud_l2_explicitly_routed",scheduler.Candidate{Kind:scheduler.CandidateModel,Qualification:scheduler.QualSupported,ProtocolLevel:"L2"},true,true},
  {"local_l0",scheduler.Candidate{Kind:scheduler.CandidateModel,Local:true,Qualification:scheduler.QualVerified,ProtocolLevel:"L0"},false,false},
  {"untested_forged_l2",scheduler.Candidate{Kind:scheduler.CandidateModel,Qualification:scheduler.QualUntested,ProtocolLevel:"L2"},false,false},
  {"incompatible_l2",scheduler.Candidate{Kind:scheduler.CandidateModel,Qualification:scheduler.QualIncompatible,ProtocolLevel:"L2"},false,false},
  {"external_runtime_without_native_tools",scheduler.Candidate{Kind:scheduler.CandidateAgentRuntime,Qualification:scheduler.QualVerified,ProtocolLevel:"L3"},false,false},
  {"native_tools",scheduler.Candidate{Kind:scheduler.CandidateModel,Qualification:scheduler.QualVerified,ProtocolLevel:"L3",ToolCallback:true},true,false},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   allowed,structured:=proposalsForCandidate(tc.candidate)
   containsTool:=false
   for _,v:=range allowed{if v==agentprotocol.ProposalTool{containsTool=true}}
   if containsTool!=tc.allowTool||structured!=tc.structured{
    t.Fatalf("JSON/native gateway capability leaked: tool=%v structured=%v",containsTool,structured)
   }
  })
 }
}

func TestJSONQualifiedModelGetsStrictlyMediatedAgentResponse(t *testing.T){
 c:=scheduler.Candidate{Kind:scheduler.CandidateModel,Local:true,
  Qualification:scheduler.QualVerified,ProtocolLevel:"L1"}
 allowed,structured:=proposalsForCandidate(c)
 req:=agentprotocol.Request{
  ProtocolVersion:agentprotocol.Version,RequestID:"request-1",WorkspaceID:"tenant",
  TaskID:"task",AttemptID:"attempt",PrincipalID:"onepane-worker",Objective:"Build in sandbox",
  Constraints:json.RawMessage(`{}`),ContextManifest:json.RawMessage(`{}`),
  PermittedProposalTypes:allowed,JSONToolProposals:structured,ToolCallback:false,
 }
 if err:=req.Validate();err!=nil{t.Fatal(err)}
 body,err:=buildModelRequest(req,c.ProtocolLevel)
 if err!=nil{t.Fatal(err)}
 var envelope struct{
  Messages []struct{Role string `json:"role"`;Content string `json:"content"`} `json:"messages"`
  ResponseFormat struct{Type string `json:"type"`} `json:"response_format"`
 }
 if err:=json.Unmarshal(body,&envelope);err!=nil{t.Fatal(err)}
 if envelope.ResponseFormat.Type!="json_object"||
  !strings.Contains(envelope.Messages[0].Content,"JSON-qualified") {
  t.Fatalf("JSON proposal was not constrained by structured response: %s",body)
 }
 proposal:=agentprotocol.Response{
  ProtocolVersion:agentprotocol.Version,RequestID:req.RequestID,
  ProposalType:agentprotocol.ProposalTool,
  Proposal:json.RawMessage(`{"tool_id":"project.app.exec","tool_version":"1","resource_ref":"project_runtime:r","input":{"runtime_id":"r","application_id":"a","command":["sh","-c","true"]}}`),
 }
 if err:=proposal.ValidateFor(req);err!=nil{
  t.Fatalf("qualified JSON tool request denied before governed Gateway: %v",err)
 }
 req.JSONToolProposals=false
 if err:=proposal.ValidateFor(req);err==nil{
  t.Fatal("nonqualified model was allowed to send a tool proposal")
 }
 req.JSONToolProposals=true
 req.PermittedProposalTypes=[]agentprotocol.ProposalType{agentprotocol.ProposalComplete}
 if err:=proposal.ValidateFor(req);err==nil{
  t.Fatal("JSON eligibility overrode an explicit per-request proposal whitelist")
 }
}
