package agentworker

import (
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/agentprotocol"
 "github.com/DigiLogicTech/OnePane/internal/scheduler"
)

// qualifiedJSONToolProposal is an alternative to native function calling for
// empirically JSON-qualified model deployments. Unverified L0/untested models
// cannot issue tool proposals, even when they can emit plausible free text.
// Actual commands remain mediated by persisted Task scope, ToolGateway leases,
// exact OCI identity and independent sandbox verification.
func qualifiedJSONToolProposal(c scheduler.Candidate) bool {
 if c.Kind!=scheduler.CandidateModel||c.ToolCallback{return false}
 switch c.Qualification {
 case scheduler.QualVerified,scheduler.QualSupported,scheduler.QualMediated,scheduler.QualLimited:
 default:return false
 }
 switch strings.ToUpper(strings.TrimSpace(c.ProtocolLevel)) {
 case "L1","L2","L3":return true
 default:return false
 }
}

func proposalsForCandidate(c scheduler.Candidate)([]agentprotocol.ProposalType,bool){
 structured:=qualifiedJSONToolProposal(c)
 allowTool:=c.ToolCallback||structured
 out:=make([]agentprotocol.ProposalType,0,len(permittedProposals))
 for _,p:=range permittedProposals{
  if p==agentprotocol.ProposalTool&&!allowTool{continue}
  out=append(out,p)
 }
 return out,structured
}
