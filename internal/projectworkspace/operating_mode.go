package projectworkspace

import (
    "encoding/json"
    "fmt"
)

// An operating mode controls a Project's objective lifecycle, not its
// permissions. Existing Projects retain Level 4 semantics without a
// schema migration or implicit activation of monitoring.
type OperatingMode string
const (
    OperatingAutonomousProject OperatingMode = "autonomous_project"
    OperatingContinuousOperations OperatingMode = "continuous_operations"
)

type operatingModeConfig struct {
    Version int `json:"version"`
    Mode OperatingMode `json:"mode"`
}

// Project operating mode lives within project_policy_json under
// onepane_operations, allowing older data to remain untouched. An absent
// value has the safe visible default of Level 4. Continuous Operations
// never grants host/network/secrets access by itself.
func operatingModeFromPolicy(raw json.RawMessage) (OperatingMode,error){
    var doc map[string]json.RawMessage
    if err:=json.Unmarshal(raw,&doc);err!=nil||doc==nil{
        return "",fmt.Errorf("invalid Project policy document")
    }
    section,ok:=doc["onepane_operations"]
    if !ok{return OperatingAutonomousProject,nil}
    var cfg operatingModeConfig
    if err:=json.Unmarshal(section,&cfg);err!=nil{
        return "",fmt.Errorf("invalid Project operating mode: %w",err)
    }
    if cfg.Version!=1 {return "",fmt.Errorf("unsupported Project operating mode schema version")}
    switch cfg.Mode{
    case OperatingAutonomousProject,OperatingContinuousOperations:return cfg.Mode,nil
    default:return "",fmt.Errorf("unsupported Project operating mode")
    }
}
