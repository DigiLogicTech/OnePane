//go:build windows

package main

import (
 "os"
 "path/filepath"
 "testing"
)

func TestSelectedDataRootAcceptsNormalExistingAndNewLocalFolders(t *testing.T) {
 root:=t.TempDir()
 for _,path:=range []string{root,filepath.Join(root,"OnePane","PersistedData")}{
  if err:=verifySelectedRootComponents(path);err!=nil{
   t.Fatalf("normal selected root refused: %v",err)
  }
 }
}

func TestSelectedDataRootRejectsFileAncestorAndSymlink(t *testing.T) {
 root:=t.TempDir()
 file:=filepath.Join(root,"file")
 if err:=os.WriteFile(file,[]byte("operator data"),0600);err!=nil{t.Fatal(err)}
 if err:=verifySelectedRootComponents(filepath.Join(file,"OnePane"));err==nil{
  t.Fatal("existing file ancestor accepted")
 }
 target:=filepath.Join(root,"real")
 if err:=os.Mkdir(target,0700);err!=nil{t.Fatal(err)}
 link:=filepath.Join(root,"junction")
 if err:=os.Symlink(target,link);err!=nil{
  t.Logf("Windows runner cannot create symlink; rejection case not executed: %v",err)
  return
 }
 if err:=verifySelectedRootComponents(filepath.Join(link,"PersistentData"));err==nil{
  t.Fatal("NTFS symlink ancestor was accepted as selected storage root")
 }
 if contents,err:=os.ReadFile(file);err!=nil||string(contents)!="operator data"{
  t.Fatalf("non-directory storage check modified user data: %q %v",contents,err)
 }
}
