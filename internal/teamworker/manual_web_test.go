package teamworker

import (
    "encoding/json"
    "strings"
    "testing"

    "github.com/DigiLogicTech/OnePane/internal/agentprotocol"
    "github.com/DigiLogicTech/OnePane/internal/team"
)
func TestManualWebSeatRequiresCompleteConfiguration(t *testing.T){
    good:=json.RawMessage(`{"manual_web":{"enabled":true,"provider_id":"chatgpt","model_label":"GPT-6 High"}}`)
    value,enabled,err:=manualWebSeatFromConfig(good)
    if err!=nil||!enabled||value.ProviderID!="chatgpt"||value.ModelLabel!="GPT-6 High"{t.Fatalf("unexpected configured seat: %+v %v %v",value,enabled,err)}
    _,enabled,err=manualWebSeatFromConfig(json.RawMessage(`{"manual_web":{"enabled":false}}`))
    if err!=nil||enabled{t.Fatalf("disabled seat should remain disabled: %v %v",enabled,err)}
    _,enabled,err=manualWebSeatFromConfig(json.RawMessage(`{"manual_web":{"enabled":true,"provider_id":"evil/site","model_label":"x"}}`))
    if err==nil||enabled {t.Fatal("invalid enabled seat must fail closed")}
}
func TestManualWebPromptContainsOnlyProvidedScopedEvidence(t *testing.T){
    sections:=[]agentprotocol.ContextSection{{ID:"task",Kind:"task",Trust:"USER_INSTRUCTION",Content:json.RawMessage(`{"objective":"Example"}`)}}
    prompt:=buildManualWebCouncilPrompt("session1","Reviewer","Check design",1,team.ResearchPhaseIndependent,team.ResearchSettings{},sections)
    for _,want:=range []string{"session1","Check design","independent","task","Example","Do not use tools"}{
        if !strings.Contains(prompt,want){t.Fatalf("expected %q in manual prompt",want)}
    }
    if strings.Contains(prompt,"another seat") {t.Fatal("unexpected unrelated Council context")}
}
