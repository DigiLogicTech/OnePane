package tool

import (
 "testing"
 "reflect"
 "github.com/DigiLogicTech/OnePane/internal/authority"
)

func TestScopedFileActionClassifier(t *testing.T){
 cases:=[]struct{capability,tool,want string}{
  {"filesystem.read","other","files.read"},
  {"filesystem.write","other","files.write"},
  {"file.read","file.read","files.read"},
  {"","project.app.files.inspect","files.read"},
  {"","project.app.files.edit","files.write"},
  {"","project.app.files.publish","files.write"},
  {"network.send","http.post",""},
  {"vault.read","vault.get",""},
  {"project.app.execute","project.app.exec",""},
  {"","unrecognised",""},
 }
 for _,tc:=range cases{
  if got:=scopedFileAction(tc.capability,tc.tool);got!=tc.want{
   t.Errorf("cap=%q tool=%q got=%q want=%q",tc.capability,tc.tool,got,tc.want)
  }
 }
}

func TestToolEffectClassificationIsConservativeAcrossAdapters(t *testing.T){
 cases:=[]struct{id,cap string;mode authority.ActionMode;want []string;classified bool}{
  {"project.app.files.inspect","project.app.execute",authority.ActionExecuteSandboxed,[]string{"files.read"},true},
  {"project.app.files.edit","project.app.execute",authority.ActionExecuteSandboxed,[]string{"files.read","files.write"},true},
  {"project.app.git.inspect","project.app.execute",authority.ActionExecuteSandboxed,[]string{"files.read","process.execute"},true},
  {"project.app.git.mutate","project.app.execute",authority.ActionExecuteSandboxed,[]string{"files.read","files.write","process.execute"},true},
  {"project.app.exec","project.app.execute",authority.ActionExecuteSandboxed,[]string{"files.read","files.write","process.execute"},true},
  {"project.app.godot.build","project.app.execute",authority.ActionExecuteSandboxed,[]string{"files.read","files.write","process.execute"},true},
  {"project.app.pull","project.app.manage",authority.ActionMutate,[]string{"process.execute","network.connect"},true},
  {"project.app.unreal.mcp.probe","project.app.execute",authority.ActionExecuteSandboxed,[]string{"process.execute","network.connect"},true},
  {"http.post","network.send",authority.ActionExternalSend,[]string{"network.connect"},true},
  {"vault.get","vault.read",authority.ActionRead,[]string{"secrets.use"},true},
  {"unknown.tool","unclassified",authority.ActionExecuteSandboxed,[]string{"files.read","files.write","process.execute"},true},
  {"unknown.mutate","unclassified",authority.ActionMutate,nil,false},
  {"unknown.read","unclassified",authority.ActionRead,nil,false},
 }
 for _,tc:=range cases{
  got,ok:=scopedToolActions(tc.cap,tc.id,tc.mode)
  if ok!=tc.classified||!reflect.DeepEqual(got,tc.want){
   t.Errorf("tool=%s cap=%s mode=%s got=%v classified=%v expected=%v %v",
    tc.id,tc.cap,tc.mode,got,ok,tc.want,tc.classified)
  }
 }
}
