package teamworker

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/agentprotocol"
 "github.com/DigiLogicTech/OnePane/internal/team"
)

type chairWebIdentity struct {ProviderID string `json:"provider_id"`;ModelLabel string `json:"model_label"`;Enabled bool `json:"enabled"`}
func chairIdentity(snapshot team.SessionSnapshot)(chairWebIdentity,error){
 for _,member:=range snapshot.Members {
  if member.ID!=snapshot.Research.ChairMemberID{continue}
  var cfg struct{ManualWeb chairWebIdentity `json:"manual_web"`;ChairOnly bool `json:"council_chair_only"`}
  if json.Unmarshal(member.Config,&cfg)!=nil||!cfg.ChairOnly||!cfg.ManualWeb.Enabled{return chairWebIdentity{},team.ErrInvalid}
  return cfg.ManualWeb,nil
 }
 return chairWebIdentity{},team.ErrInvalid
}

// Freeze agenda and round-review output before exposing it to a research seat.
// Only approved text is forwarded, never unreviewed Chair proposals.
func(s *Service) chairApprovedContext(ctx context.Context,session string,round int64)([]agentprotocol.ContextSection,error){
 rows,err:=s.db.QueryContext(ctx,`SELECT stage,after_round,approved_text,approved_sha256,approved_by
 FROM manual_web_council_chair_turns
 WHERE session_id=? AND status='approved' AND after_round<? ORDER BY after_round,stage`,session,round)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]agentprotocol.ContextSection{}
 used:=0
 for rows.Next(){
  var stage,text,sum,approvedBy string
  var after int64
  if err:=rows.Scan(&stage,&after,&text,&sum,&approvedBy);err!=nil{return nil,err}
  raw,_:=json.Marshal(map[string]any{"stage":stage,"after_round":after,"approved_text":text,"approved_sha256":sum,"approved_by":approvedBy,"authority":"operator_approved_agenda"})
  if used+len(raw)>96<<10 {return nil,fmt.Errorf("approved Chair agenda exceeds scoped evidence limit")}
  used+=len(raw)
  out=append(out,agentprotocol.ContextSection{ID:fmt.Sprintf("chair-%s-%d",stage,after),Kind:"approved_council_chair_guidance",
   Trust:"USER_INSTRUCTION",Authoritative:true,Content:raw})
 }
 return out,rows.Err()
}
// Build a Chair prompt scoped to only the objective, approved agenda, and
// completed previous-round outputs. It never includes active same-round
// research seats and never grants a tool capability.
func(s *Service) chairPrompt(ctx context.Context,snapshot team.SessionSnapshot,stage string,after int64)(string,error){
 var b strings.Builder
 b.WriteString("OnePane Research Council Chair — manual web consultation\n")
 b.WriteString(fmt.Sprintf("Session: %s\nStage: %s\nAfter round: %d\nObjective: %s\n\n",snapshot.SessionID,stage,after,snapshot.TaskObjective))
 b.WriteString("You are the facilitator, not a voting participant. Do not invent evidence, override human controls, reveal hidden independent outputs, invoke tools or take external actions. Return a substantive plain-text proposal, with explicit uncertainties and precise questions. Your response is a proposal: OnePane only advances upon approval when required.\n\n")
 if stage=="agenda"{
  b.WriteString("Create an evidence-sensitive research agenda: frame the core question, define success criteria, distinguish subproblems, propose a fair set of independent first-pass questions and clarify what would falsify each hypothesis. Do not answer for the Council members.\n")
 }else{
  b.WriteString("Facilitate cross-critique. Assess the completed research answers below, isolate substantive agreements, disagreements, missing evidence and circular arguments, then propose specific questions for the next round. Do not dictate consensus or answer in place of the participants.\n")
  rows,err:=s.db.QueryContext(ctx,`SELECT m.content_json,m.author_member_id,m.round_number FROM team_messages m
   WHERE m.session_id=? AND m.message_kind='agent' AND m.round_number=?
   ORDER BY m.created_at,m.id LIMIT 20`,snapshot.SessionID,after)
  if err!=nil{return "",err}
  defer rows.Close()
  var used int
  for rows.Next(){
   var text,author string
   var r int64
   if err:=rows.Scan(&text,&author,&r);err!=nil{return "",err}
   sum:=sha256.Sum256([]byte(text))
   label:="Anonymous source"
   for i,m:=range snapshot.Members{if m.ID==author {label=fmt.Sprintf("Source %d",i+1);break}}
   section:=fmt.Sprintf("\n%s response sha256=%s:\n%s\n",label,hex.EncodeToString(sum[:]),text)
   if used+len(section)>96<<10 {return "",fmt.Errorf("Chair review context exceeds 96KiB; no silent truncation")}
   used+=len(section);b.WriteString(section)
  }
  if err:=rows.Err();err!=nil{return "",err}
  b.WriteString("\nPreserve minority findings and distinguish evidential disagreement from preferences.\n")
 }
 if b.Len()>160<<10{return "",team.ErrInvalid}
 return b.String(),nil
}
func(s *Service) ensureManualChair(ctx context.Context,snapshot team.SessionSnapshot,workspace,stage string,after int64)(bool,error){
 var status string
 err:=s.db.QueryRowContext(ctx,`SELECT status FROM manual_web_council_chair_turns
  WHERE session_id=? AND stage=? AND after_round=?`,snapshot.SessionID,stage,after).Scan(&status)
 if err==nil{return status=="approved",nil}
 if err!=sql.ErrNoRows{return false,err}
 identity,err:=chairIdentity(snapshot);if err!=nil{return false,err}
 prompt,err:=s.chairPrompt(ctx,snapshot,stage,after);if err!=nil{return false,err}
 _,err=s.teams.QueueManualWebChair(ctx,team.QueueManualWebChairCommand{
  SessionID:snapshot.SessionID,WorkspaceID:workspace,MemberID:snapshot.Research.ChairMemberID,
  ProviderID:identity.ProviderID,ModelLabel:identity.ModelLabel,Stage:stage,AfterRound:after,Prompt:prompt,
 })
 return false,err
}
