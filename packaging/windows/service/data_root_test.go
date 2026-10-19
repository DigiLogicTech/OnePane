//go:build windows

package main

import (
 "os"
 "path/filepath"
 "testing"
)

func TestInstallerDataRootDoesNotSilentlyFallBack(t *testing.T) {
 fallback := `C:\ProgramData\OnePane`
 // A missing installer setting must preserve legacy installs.
 if got,err:=resolveOnePaneDataRoot(fallback,"",false);err!=nil||got!=filepath.Clean(fallback){
  t.Fatalf("legacy absent-root migration: %q %v",got,err)
 }
 selected := `D:\OnePane\PersistedData`
 if got,err:=resolveOnePaneDataRoot(fallback,"  "+selected+"  ",true);err!=nil||got!=filepath.Clean(selected){
  t.Fatalf("installer-selected non-C data root changed: %q %v",got,err)
 }
 for _,value:=range []string{"", " ", "relative\\OnePane", `D:relative\OnePane`, `.\OnePane`, `\OnePane`} {
  if got,err:=resolveOnePaneDataRoot(fallback,value,true);err==nil{
   t.Fatalf("explicit invalid DataRoot %q silently fell back to %q",value,got)
  }
 }
}

func TestInstallerExplicitStorageRootsRejectUNCAndDeviceNames(t *testing.T) {
 for _,configured:=range []string{
  `\\server\share\OnePane`,
  `\\?\C:\OnePane`,
  `\\.\C:\OnePane`,
  `\\?\UNC\server\share\OnePane`,
 } {
  if got,err:=resolveOnePaneDataRoot(`C:\ProgramData\OnePane`,configured,true);err==nil {
   t.Fatalf("explicit remote/device storage %q incorrectly accepted: %q",configured,got)
  }
 }
 for _,configured:=range []string{`C:\OnePane\data`,`D:\OnePane\models`,`E:\OnePane\Projects`} {
  if got,err:=resolveOnePaneDataRoot(`C:\ProgramData\OnePane`,configured,true);err!=nil||got!=filepath.Clean(configured){
   t.Fatalf("ordinary local drive %q rejected: %q %v",configured,got,err)
  }
 }
}

func TestModelAndProjectSelectionRejectUnsafeRootsBeforeConfigReuse(t *testing.T){
 root:=t.TempDir()
 for _,relative:=range []string{"Models","Projects"}{
  good:=filepath.Join(root,relative)
  got,err:=resolveInstallerStoragePath(filepath.Join(root,"legacy"),good,true)
  if err!=nil||got!=filepath.Clean(good){
   t.Fatalf("normal selected %s path refused: %q %v",relative,got,err)
  }
 }
 if got,err:=resolveInstallerStoragePath(filepath.Join(root,"legacy"),"",false);err!=nil||
  got!=filepath.Join(root,"legacy"){
  t.Fatalf("legacy absent path changed: %q %v",got,err)
 }
 bad:=filepath.Join(root,"existing-file")
 if err:=os.WriteFile(bad,[]byte("do not touch"),0600);err!=nil{t.Fatal(err)}
 if _,err:=resolveInstallerStoragePath(filepath.Join(root,"legacy"),filepath.Join(bad,"Projects"),true);err==nil{
  t.Fatal("model/project path beneath non-directory accepted")
 }
 if _,err:=resolveInstallerStoragePath(filepath.Join(root,"legacy"),`\\fileserver\models\OnePane`,true);err==nil{
  t.Fatal("UNC model pool accepted despite local-drive policy")
 }
}
