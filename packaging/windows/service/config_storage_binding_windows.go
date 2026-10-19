//go:build windows

package main

import (
 "fmt"
 "os"
 "path/filepath"
 "strings"

 "gopkg.in/yaml.v3"
)

// Verify selected MSI paths against existing operator YAML without changing
// any model weights, Projects or configuration. An installer registry value
// does NOT itself move data: mismatches must be reconciled by a human before
// a Windows service starts using a different state location.
func verifyExistingConfigStorageSelections(path string,
 dataRoot string, dataSelected bool,
 projectRoot string, projectSelected bool,
 modelRoot string, modelSelected bool) error {
 if err:=inspectExistingServiceConfig(path);err!=nil{return err}
 if !dataSelected&&!projectSelected&&!modelSelected{return nil}
 contents,err:=os.ReadFile(path)
 if err!=nil{return fmt.Errorf("inspect existing OnePane configuration: %w",err)}
 var parsed struct {
  Storage struct {
   DataDir string `yaml:"data_dir"`
   ProjectRoot string `yaml:"project_root"`
  } `yaml:"storage"`
  LocalAI struct{
   ModelPoolPath string `yaml:"model_pool_path"`
  } `yaml:"local_ai"`
 }
 if err:=yaml.Unmarshal(contents,&parsed);err!=nil{
  return fmt.Errorf("existing OnePane storage configuration is invalid: %w",err)
 }
 if dataSelected {
  if err:=compareInstallerStorageChoice("storage.data_dir",parsed.Storage.DataDir,
   filepath.Join(dataRoot,"data"),"DataRoot");err!=nil{return err}
 }
 if projectSelected{
  if err:=compareInstallerStorageChoice("storage.project_root",parsed.Storage.ProjectRoot,
   projectRoot,"ProjectRoot");err!=nil{return err}
 }
 if modelSelected{
  if err:=compareInstallerStorageChoice("local_ai.model_pool_path",parsed.LocalAI.ModelPoolPath,
   modelRoot,"ModelRoot");err!=nil{return err}
 }
 return nil
}

func compareInstallerStorageChoice(configField,configValue,expected,registryName string)error{
 actual:=strings.TrimSpace(configValue)
 if actual==""||!filepath.IsAbs(actual){
  return fmt.Errorf("existing %s is missing or not absolute; reconcile MSI %s without overwriting operator data",configField,registryName)
 }
 if !strings.EqualFold(filepath.Clean(actual),filepath.Clean(expected)){
  return fmt.Errorf("existing %s differs from MSI-selected %s; reconcile actual storage or migrate with a verified backup before restarting OnePane",configField,registryName)
 }
 return nil
}

// Preserve the earlier test/helper contract for explicitly-selected DataRoot.
func verifyExistingConfigUsesSelectedRoot(configPath,selectedRoot string)error{
 if strings.TrimSpace(selectedRoot)==""||!filepath.IsAbs(selectedRoot){
  return fmt.Errorf("invalid selected OnePane data root")
 }
 return verifyExistingConfigStorageSelections(configPath,selectedRoot,true,"",false,"",false)
}
