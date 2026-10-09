package localai

import (
 "encoding/json"
 "testing"
)

func TestManagedModelInventoryDoesNotConfuseAdmissionWithResidency(t *testing.T){
 row:=ManagedDeploymentSummary{
  DeploymentID:"world-model",Status:"ready",ResidencyState:"stopped",
  AgentCheckStatus:"passed",AdmissionStatus:"accepted",ComputeMode:"gpu",
 }
 raw,err:=json.Marshal(row)
 if err!=nil{t.Fatal(err)}
 var actual map[string]any
 if err:=json.Unmarshal(raw,&actual);err!=nil{t.Fatal(err)}
 if actual["status"]!="ready"||actual["residency_state"]!="stopped"||
  actual["agent_check_status"]!="passed"||actual["admission_status"]!="accepted"||
  actual["compute_mode"]!="gpu"{
  t.Fatalf("installed/qualified is not equivalent to resident: %s",raw)
 }
}
