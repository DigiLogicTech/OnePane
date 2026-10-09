package agentworker

import (
 "encoding/json"
 "strings"
 "testing"
 "unicode/utf8"
)

func TestOversizedBuildFailureKeepsExitCodeAndUsefulDiagnosticTail(t *testing.T){
 stderr:=strings.Repeat("old compiler warning\n",2500)+"error: unresolved symbol maçã\n"
 stdout:=strings.Repeat("build output\n",2500)+"FAIL package/game/world\n"
 raw,err:=json.Marshal(map[string]any{
  "runtime_id":"world","application_id":"toolchain",
  "command":[]string{"sh","-c","go test ./..."},
  "succeeded":false,
  "result":map[string]any{"exit_code":2,"stdout":stdout,"stderr":stderr},
 })
 if err!=nil{t.Fatal(err)}
 const budget=16<<10
 if len(raw)<=budget{t.Fatalf("fixture must exceed context limit, got %d",len(raw))}
 compact:=boundedToolResult(raw,budget)
 metadata,ok:=compact.(map[string]any)
 if !ok{t.Fatalf("expected diagnostic object: %#v",compact)}
 if metadata["succeeded"]!=false||metadata["exit_code"]!=2||metadata["truncated"]!=true{
  t.Fatalf("lost tool completion truth: %+v",metadata)
 }
 errorsTail,ok:=metadata["stderr_tail"].(string)
 if !ok||!strings.HasSuffix(errorsTail,"error: unresolved symbol maçã\n")||
  strings.HasPrefix(errorsTail,"old compiler warning\nold compiler warning\n"){
  t.Fatalf("useful final compiler error not preserved: %q",errorsTail)
 }
 if !utf8.ValidString(errorsTail){t.Fatal("UTF-8 truncated across codepoint")}
 if !strings.Contains(metadata["stdout_tail"].(string),"FAIL package/game/world"){
  t.Fatal("test summary lost from stdout tail")
 }
 encoded,err:=json.Marshal(compact)
 if err!=nil||len(encoded)>budget{
  t.Fatalf("oversized continuation: %d bytes err=%v",len(encoded),err)
 }
}

func TestSmallToolResultAndNonSandboxOversizeRetainLegacySemantics(t *testing.T){
 small:=json.RawMessage(`{"succeeded":false,"result":{"exit_code":1,"stderr":"error"}}`)
 compact:=boundedToolResult(small,16<<10)
 m,ok:=compact.(map[string]any)
 if !ok||m["succeeded"]!=false||m["truncated"]!=nil{
  t.Fatalf("small result incorrectly changed: %+v",compact)
 }
 huge,_:=json.Marshal(map[string]any{"blob":strings.Repeat("x",100000)})
 fallback:=boundedToolResult(huge,16<<10)
 summary,ok:=fallback.(map[string]any)
 if !ok||summary["truncated"]!=true||summary["bytes"]!=len(huge)||
  summary["exit_code"]!=nil{
  t.Fatalf("non-sandbox tool summary incorrectly received fabricated exit code: %+v",fallback)
 }
}
