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

func TestSelectedMSIProjectAndModelRootsCannotSilentlyShadowExistingLibraries(t *testing.T){
 root:=t.TempDir()
 configPath:=filepath.Join(root,"config.yaml")
 projects:=filepath.Join(root,"Projects")
 models:=filepath.Join(root,"models")
 makeConfig:=func(project,model string) []byte {
  return []byte("storage:\n  data_dir: "+filepath.ToSlash(filepath.Join(root,"data"))+
   "\n  project_root: "+filepath.ToSlash(project)+
   "\nlocal_ai:\n  model_pool_path: "+filepath.ToSlash(model)+
   "\nlogging:\n  level: info\n")
 }
 original:=makeConfig(projects,models)
 if err:=os.WriteFile(configPath,original,0600);err!=nil{t.Fatal(err)}
 if err:=verifyExistingConfigStorageSelections(configPath,root,true,
  projects,true,models,true);err!=nil{t.Fatalf("matching MSI storage refused: %v",err)}
 for _,tt:=range []struct{name,dataRoot,projectRoot,modelRoot string}{
  {"project moved only",root,filepath.Join(root,"elsewhere","Projects"),models},
  {"models moved only",root,projects,filepath.Join(root,"elsewhere","models")},
  {"database moved only",filepath.Join(root,"newroot"),projects,models},
 }{
  t.Run(tt.name,func(t *testing.T){
   if err:=verifyExistingConfigStorageSelections(configPath,tt.dataRoot,true,
    tt.projectRoot,true,tt.modelRoot,true);err==nil{
    t.Fatal("MSI storage mismatch silently accepted")
   }
  })
 }
 saved,err:=os.ReadFile(configPath)
 if err!=nil||string(saved)!=string(original){t.Fatalf("config bytes changed: %q %v",saved,err)}
}

func TestExplicitModelAndProjectRequireExistingAbsoluteConfiguredPaths(t *testing.T){
 root:=t.TempDir()
 path:=filepath.Join(root,"config.yaml")
 project:=filepath.Join(root,"Projects")
 model:=filepath.Join(root,"models")
 for _,yaml:=range []string{
  "storage:\n  project_root: ''\nlocal_ai:\n  model_pool_path: "+filepath.ToSlash(model)+"\n",
  "storage:\n  project_root: "+filepath.ToSlash(project)+"\nlocal_ai:\n  model_pool_path: relative\n",
  "storage: [not-a-map]\n",
 } {
  if err:=os.WriteFile(path,[]byte(yaml),0600);err!=nil{t.Fatal(err)}
  if err:=verifyExistingConfigStorageSelections(path,root,false,project,true,model,true);err==nil{
   t.Fatalf("invalid existing model/project choice accepted: %q",yaml)
  }
 }
}

func TestAbsentMSIProjectAndModelChoicesDoNotRewriteOperatorConfig(t *testing.T){
 root:=t.TempDir()
 path:=filepath.Join(root,"config.yaml")
 source:=[]byte("storage:\n  project_root: C:\\Legacy\\SomeProjects\n")
 if err:=os.WriteFile(path,source,0600);err!=nil{t.Fatal(err)}
 if err:=verifyExistingConfigStorageSelections(path,root,false,"",false,"",false);err!=nil{
  t.Fatalf("absent registry selections broke legacy config: %v",err)
 }
 saved,err:=os.ReadFile(path)
 if err!=nil||string(saved)!=string(source){t.Fatalf("legacy YAML changed: %q %v",saved,err)}
}
