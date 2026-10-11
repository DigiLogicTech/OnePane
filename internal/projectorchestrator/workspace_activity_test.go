package projectorchestrator

import (
 "strings"
 "testing"
 "unicode/utf8"
)

func TestBoundedOrchestratorObjectivePreservesUTF8AndFlagsTruncation(t *testing.T) {
 tests:=[]struct{name, objective, expected string; truncated bool}{
  {"short","ship code","ship code",false},
  {"exact maximum",strings.Repeat("x",256),strings.Repeat("x",256),false},
  {"ASCII past maximum",strings.Repeat("x",260)+"PRIVATE_TAIL",strings.Repeat("x",256),true},
  {"Unicode boundary",strings.Repeat("🧪",257)+"PRIVATE_TAIL",strings.Repeat("🧪",256),true},
 }
 for _,tt:=range tests{
  t.Run(tt.name,func(t *testing.T){
   got,truncated:=boundedOrchestratorObjective(tt.objective)
   if got!=tt.expected||truncated!=tt.truncated||!utf8.ValidString(got){
    t.Fatalf("preview length %d truncated %t, expected %d truncated %t",utf8.RuneCountInString(got),truncated,utf8.RuneCountInString(tt.expected),tt.truncated)
   }
  })
 }
}
