package tool

import (
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/authority"
)

// A registry entry with a built-in Project Tool ID does not itself make
// its claimed operation trustworthy. This table guards the exact effect
// mode for every built-in OCI/Workspace adapter operation.
func TestBuiltInProjectToolNamesCannotChangeTheirAuthorityMode(t *testing.T){
 cases:=[]struct{id,cap string;allowed authority.ActionMode}{
  {"project.runtime.inspect","project.runtime.observe",authority.ActionObserve},
  {"project.app.inspect","project.runtime.observe",authority.ActionObserve},
  {"project.image.inspect","project.runtime.observe",authority.ActionObserve},
  {"project.app.files.inspect","filesystem.read",authority.ActionExecuteSandboxed},
  {"project.app.files.edit","filesystem.write",authority.ActionExecuteSandboxed},
  {"project.app.files.publish","filesystem.write",authority.ActionExecuteSandboxed},
  {"project.app.git.inspect","project.app.execute",authority.ActionExecuteSandboxed},
  {"project.app.git.mutate","project.app.execute",authority.ActionExecuteSandboxed},
  {"project.app.exec","project.app.execute",authority.ActionExecuteSandboxed},
  {"project.app.godot.build","project.app.execute",authority.ActionExecuteSandboxed},
  {"project.app.tools.discover","project.app.execute",authority.ActionExecuteSandboxed},
  {"project.app.toolchain.preflight","project.app.execute",authority.ActionExecuteSandboxed},
  {"project.app.unreal.mcp.probe","project.app.execute",authority.ActionExecuteSandboxed},
  {"project.runtime.ensure","project.runtime.manage",authority.ActionMutate},
  {"project.runtime.stop","project.runtime.manage",authority.ActionMutate},
  {"project.app.ensure","project.runtime.manage",authority.ActionMutate},
  {"project.app.stop","project.runtime.manage",authority.ActionMutate},
  {"project.app.pull","project.runtime.manage",authority.ActionMutate},
 }
 modes:=[]authority.ActionMode{
  authority.ActionObserve,authority.ActionRead,authority.ActionMutate,
  authority.ActionExternalSend,authority.ActionExecuteSandboxed,
 }
 for _,tc:=range cases{
  for _,mode:=range modes{
   effects,classified:=scopedToolActions(tc.cap,tc.id,mode)
   if mode==tc.allowed{
    if !classified{
     t.Errorf("%s: registered mode %s was unexpectedly denied",tc.id,mode)
    }
   }else if classified||len(effects)>0{
    t.Errorf("%s: unregistered mode %s enabled %v (expected only %s)",
     tc.id,mode,effects,tc.allowed)
   }
  }
 }
}
