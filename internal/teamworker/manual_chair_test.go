package teamworker

import (
 "context"
 "database/sql"
 "encoding/json"
 "strings"
 "testing"
 _ "modernc.org/sqlite"

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
 for _,word:=range []string{"Compare candidate architectures","agenda","hypothesis","independent","Do not invent evidence","approval"}{
  if !strings.Contains(prompt,word){t.Fatalf("Chair agenda prompt missing %q",word)}
 }
 if strings.Contains(prompt,"agent-candidate-id") {t.Fatal("Chair must never carry an inferred scheduler seat")}
 snap.Research.ChairMemberID="analyst-1"
 if _,err:=chairIdentity(snap);err==nil {t.Fatal("non-Chair participant cannot act as Chair")}
 snap.Research.ChairMemberID="chair-1"
 snap.Members[1].Config=json.RawMessage(`{"manual_web":{"enabled":true,"provider_id":"chatgpt","model_label":"label"}}`)
 if _,err:=chairIdentity(snap);err==nil {t.Fatal("Chair must be configured explicitly as Chair-only")}
}

func TestChairGuidanceIsOnlyExposedAfterApprovalAndNextRound(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:");if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 if _,err=db.Exec(`CREATE TABLE manual_web_council_chair_turns(
  session_id TEXT,stage TEXT,after_round INTEGER,status TEXT,
  approved_text TEXT,approved_sha256 TEXT,approved_by TEXT);`);err!=nil{t.Fatal(err)}
 _,err=db.Exec(`INSERT INTO manual_web_council_chair_turns VALUES
   ('session','agenda',0,'approved','Independent checks: compare evidence','sum1','human'),
   ('session','review',1,'awaiting_approval',NULL,NULL,NULL),
   ('session','review',2,'approved','Only use after round 2','sum2','human'),
   ('another','agenda',0,'approved','SECRET_OTHER_SESSION','sum3','human')`)
 if err!=nil{t.Fatal(err)}
 service:=&Service{db:db}
 first,err:=service.chairApprovedContext(context.Background(),"session",1)
 if err!=nil{t.Fatal(err)}
 if len(first)!=1||first[0].Kind!="approved_council_chair_guidance"{
  t.Fatalf("only approved agenda may reach first independent round, got %+v",first)
 }
 if !strings.Contains(string(first[0].Content),"Independent checks"){t.Fatal("approved agenda missing")}
 second,err:=service.chairApprovedContext(context.Background(),"session",2)
 if err!=nil{t.Fatal(err)}
 if len(second)!=1{t.Fatalf("pending approval leaked into round two: %+v",second)}
 later,err:=service.chairApprovedContext(context.Background(),"session",3)
 if err!=nil{t.Fatal(err)}
 if len(later)!=2{t.Fatalf("subsequently approved prior guidance missing: %+v",later)}
 for _,section:=range later{
  if strings.Contains(string(section.Content),"SECRET_OTHER_SESSION"){t.Fatal("another session's guidance leaked")}
 }
}
