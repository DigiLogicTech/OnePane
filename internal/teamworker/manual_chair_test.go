package teamworker

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/team"
)

func TestCouncilChairUsesDistinctFrozenModelAndNoApiInference(t *testing.T){
 chair:=team.SnapshotMember{
  ID:"chair-1",Status:"active",MemberKind:"agent",
  Config:json.RawMessage(`{"council_chair_only":true,"manual_web":{"enabled":true,"provider_id":"chatgpt","model_label":"Web model chosen by operator"}}`),
 }
 participant:=team.SnapshotMember{ID:"analyst-1",Status:"active",MemberKind:"agent"}
 snap:=team.SessionSnapshot{SessionID:"s-1",TaskObjective:"Compare candidate architectures",
  Members:[]team.SnapshotMember{participant,chair},Research:team.ResearchSettings{
    ChairMode:"manual",ChairMemberID:"chair-1",ChairRequireApproval:true,SynthesisMemberID:"analyst-1",
  }}
 identity,err:=chairIdentity(snap)
 if err!=nil||!identity.Enabled||identity.ProviderID!="chatgpt"||identity.ModelLabel!="Web model chosen by operator"{
  t.Fatalf("unexpected Chair identity: %+v %v",identity,err)
 }
 service:=&Service{}
 prompt,err:=service.chairPrompt(context.Background(),snap,"agenda",0)
 if err!=nil{t.Fatal(err)}
 for _,word:=range []string{"Compare candidate architectures","agenda","hypotheses","independent","Do not invent evidence","approval"}{
  if !strings.Contains(prompt,word){t.Fatalf("Chair agenda prompt missing %q",word)}
 }
 if strings.Contains(prompt,"agent-candidate-id") {t.Fatal("Chair must never carry an inferred scheduler seat")}
 snap.Research.ChairMemberID="analyst-1"
 if _,err:=chairIdentity(snap);err==nil {t.Fatal("non-Chair participant cannot act as Chair")}
 snap.Research.ChairMemberID="chair-1"
 snap.Members[1].Config=json.RawMessage(`{"manual_web":{"enabled":true,"provider_id":"chatgpt","model_label":"label"}}`)
 if _,err:=chairIdentity(snap);err==nil {t.Fatal("Chair must be configured explicitly as Chair-only")}
}
