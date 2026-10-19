//go:build windows

package main

import (
 "os"
 "path/filepath"
 "testing"
)

func TestExistingConfigSelectedRootMatchesAndPreservesUserData(t *testing.T){
 root:=t.TempDir()
 configPath:=filepath.Join(root,"config.yaml")
 source:=[]byte("storage:\n  data_dir: "+filepath.ToSlash(filepath.Join(root,"data"))+"\nsecrets:\n  user_custom: untouched\n")
 if err:=os.WriteFile(configPath,source,0600);err!=nil{t.Fatal(err)}
 if err:=verifyExistingConfigUsesSelectedRoot(configPath,root);err!=nil{
  t.Fatalf("matching selected data drive refused: %v",err)
 }
 after,err:=os.ReadFile(configPath)
 if err!=nil||string(after)!=string(source){t.Fatalf("legacy settings rewritten: %q %v",after,err)}
}

func TestExistingConfigCannotSilentlyRestartDatabaseOnOldDrive(t *testing.T){
 root:=t.TempDir()
 configPath:=filepath.Join(root,"config.yaml")
 variants:=[]string{
  "storage:\n  data_dir: C:\\ProgramData\\OnePane\\data\n",
  "storage:\n  data_dir: .\\data\n",
  "storage:\n  data_dir: ''\n",
  "storage: [not-a-mapping]\n",
 }
 for _,contents:=range variants{
  if err:=os.WriteFile(configPath,[]byte(contents),0600);err!=nil{t.Fatal(err)}
  if err:=verifyExistingConfigUsesSelectedRoot(configPath,root);err==nil{
   t.Fatalf("refused to detect displaced or invalid database: %q",contents)
  }
  saved,err:=os.ReadFile(configPath)
  if err!=nil||string(saved)!=contents{
   t.Fatalf("mismatch validation changed existing file: %q %v",saved,err)
  }
 }
}

func TestExistingConfigDoesNotFollowLinkedOrDirectoryPath(t *testing.T){
 root:=t.TempDir()
 path:=filepath.Join(root,"config.yaml")
 if err:=os.Mkdir(path,0700);err!=nil{t.Fatal(err)}
 if err:=verifyExistingConfigUsesSelectedRoot(path,root);err==nil{
  t.Fatal("directory accepted as database configuration")
 }
}
