//go:build windows

package main

import (
 "path/filepath"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/config"
)

func TestNewWindowsServiceStartsLocalFirstWithoutNodeDiscovery(t *testing.T){
 root:=filepath.Join(t.TempDir(),"OnePane")
 projects:=filepath.Join(root,"Projects")
 models:=filepath.Join(root,"Models")
 cfg:=firstRunWindowsServiceConfig(root,projects,models)
 if cfg.Federation.Enabled||cfg.Federation.DiscoveryEnabled{
  t.Fatal("new Windows install would expose federation/listener or multicast before opt-in")
 }
 if cfg.Server.Listen!="127.0.0.1:18181"||
  cfg.Server.PreviewListen!="127.0.0.1:18182"{
  t.Fatalf("new Windows service unexpectedly exposes HTTP endpoints: %+v",cfg.Server)
 }
 if cfg.Storage.DataDir!=filepath.Join(root,"data")||
  cfg.Storage.ProjectRoot!=projects||cfg.LocalAI.ModelPoolPath!=models{
  t.Fatalf("new Windows service ignored selected storage: %+v",cfg)
 }
 if err:=config.Validate(cfg);err!=nil{
  t.Fatalf("local-only initial service config invalid: %v",err)
 }
 if !config.Default().Federation.Enabled{
  t.Fatal("test changed unrelated defaults globally instead of only Windows first run")
 }
}
