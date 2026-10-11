package tool

import "testing"

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
