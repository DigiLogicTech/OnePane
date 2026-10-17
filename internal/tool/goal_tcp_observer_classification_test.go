package tool

import (
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/authority"
)

func TestGoalTCPObserverToolCannotDowncastMutationPolicy(t *testing.T){
 for _,id:=range []string{
  "project.goal.tcp_observer.pause",
  "project.goal.tcp_observer.resume",
 }{
  actions,ok:=scopedToolActions("goal.tcp_observer.control",id,authority.ActionMutate)
  if !ok||len(actions)!=1||actions[0]!="network.connect"{
   t.Fatalf("local observer operation omitted required network policy: %s %v %v",id,actions,ok)
  }
  for _,mode:=range []authority.ActionMode{
   authority.ActionObserve,
   authority.ActionExecuteSandboxed,
   authority.ActionExternalSend,
  }{
   if _,ok:=scopedToolActions("goal.tcp_observer.control",id,mode);ok{
    t.Fatalf("observer tool spoofed less governed mode: %s %s",id,mode)
   }
  }
 }
}
