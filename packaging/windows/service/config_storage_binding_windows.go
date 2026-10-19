//go:build windows

package main

import (
 "fmt"
 "os"
 "path/filepath"
 "strings"

 "gopkg.in/yaml.v3"
)

// verifyExistingConfigUsesSelectedRoot is a read-only guard for MSI upgrades.
// The installer-selected DataRoot is authoritative for SQLite identity;
// an existing YAML copied from C: must never make a D: installation quietly
// create or reopen the old C: database. Do not rewrite/move operator data.
func verifyExistingConfigUsesSelectedRoot(configPath,selectedRoot string)error{
 if err:=inspectExistingServiceConfig(configPath);err!=nil{return err}
 if strings.TrimSpace(selectedRoot)==""||!filepath.IsAbs(selectedRoot){
  return fmt.Errorf("invalid selected OnePane data root")
 }
 contents,err:=os.ReadFile(configPath)
 if err!=nil{return fmt.Errorf("inspect existing persistent config: %w",err)}
 var parsed struct {
  Storage struct {
   DataDir string `yaml:"data_dir"`
  } `yaml:"storage"`
 }
 if err:=yaml.Unmarshal(contents,&parsed);err!=nil{
  return fmt.Errorf("existing OnePane storage configuration is invalid: %w",err)
 }
 actual:=strings.TrimSpace(parsed.Storage.DataDir)
 if actual==""||!filepath.IsAbs(actual) {
  return fmt.Errorf("existing configuration has no absolute storage.data_dir; reconcile storage locations without overwriting data")
 }
 expected:=filepath.Join(selectedRoot,"data")
 if !strings.EqualFold(filepath.Clean(actual),filepath.Clean(expected)){
  return fmt.Errorf("existing configuration storage.data_dir differs from MSI-selected DataRoot; reconcile or migrate persistent data before service start")
 }
 return nil
}
