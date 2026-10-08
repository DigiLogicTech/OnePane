package team

import (
    "encoding/json"
    "testing"
)

func TestValidateManualWebProvider(t *testing.T){
    tests:=[]struct{
        provider,model string
        valid bool
    }{
        {"ChatGPT","GPT-6 High",true},
        {"claude","Claude Sonnet",true},
        {"gemini","Gemini Pro",true},
        {"perplexity","Sonar",true},
        {"grok","Grok",true},
        {"a-custom_provider","Custom model",true},
        {"","No Provider",false},
        {"chatgpt","",false},
        {"https://evil.example","Model",false},
        {"chatgpt","gpt-6\ninjected",false},
    }
    for _,tt:=range tests {
        p,m,e:=ValidateManualWebProvider(tt.provider,tt.model)
        if (e==nil)!=tt.valid {t.Fatalf("provider=%q model=%q valid=%v err=%v",tt.provider,tt.model,tt.valid,e)}
        if tt.valid &&(p==""||m==""){t.Fatalf("empty normalized provider: %q %q",p,m)}
    }
}
func TestManualWebDigestDeterministicAndDifferent(t *testing.T){
    if a,b:=manualWebDigest("prompt one"),manualWebDigest("prompt one");a!=b||len(a)!=64 {
        t.Fatalf("sha256 mismatch: %q %q",a,b)
    }
    if manualWebDigest("prompt one")==manualWebDigest("prompt two"){t.Fatal("distinct prompts got same digest")}
}
func TestManualWebTurnMarshalIncludesGenerationAndProvider(t *testing.T){
    entry:=ManualWebTurn{ProviderID:"chatgpt",ModelLabel:"GPT-6 High",ConversationGeneration:2,PromptSHA256:"test",Status:"awaiting_input"}
    raw,err:=json.Marshal(entry);if err!=nil{t.Fatal(err)}
    var obj map[string]any
    if err:=json.Unmarshal(raw,&obj);err!=nil{t.Fatal(err)}
    if obj["conversation_generation"]!=float64(2)||obj["provider_id"]!="chatgpt" {t.Fatalf("missing provenance: %s",raw)}
}
