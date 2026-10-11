package inference

import (
 "encoding/json"
 "testing"
)

func TestInferenceBudgetCannotBeBypassedByDeploymentCostOverrides(t *testing.T){
 for _,tc:=range []struct{
  name string
  provider string
  deployment string
  need bool
 }{
  {"paid provider cannot claim local",
   `{"scheduling":{"cost_class":"paid","hard_zero_incremental_cost":false}}`,
   `{"scheduling":{"cost_class":"local","hard_zero_incremental_cost":true}}`,true},
  {"paid provider cannot claim free",
   `{"scheduling":{"cost_class":"paid"}}`,
   `{"scheduling":{"cost_class":"free","hard_zero_incremental_cost":true}}`,true},
  {"unknown provider cannot claim free",
   `{"scheduling":{"cost_class":"unknown"}}`,
   `{"scheduling":{"cost_class":"free","hard_zero_incremental_cost":true}}`,true},
  {"provider claiming local lacks host verification",
   `{"scheduling":{"cost_class":"local","hard_zero_incremental_cost":true}}`,
   `{}`,true},
  {"free provider needs explicit zero-cost proof",
   `{"scheduling":{"cost_class":"free","hard_zero_incremental_cost":false}}`,
   `{"scheduling":{"cost_class":"local","hard_zero_incremental_cost":true}}`,true},
  {"registered provider free and hard zero",
   `{"scheduling":{"cost_class":"free","hard_zero_incremental_cost":true}}`,
   `{"scheduling":{"cost_class":"paid"}}`,false},
  {"malformed provider cannot borrow deployment grants",
   `{"scheduling":null`,
   `{"scheduling":{"cost_class":"free","hard_zero_incremental_cost":true}}`,true},
  {"empty provider cost is protected",
   `{}`,
   `{"scheduling":{"cost_class":"local","hard_zero_incremental_cost":true}}`,true},
 }{
  t.Run(tc.name,func(t *testing.T){
   deployment:=ModelDeployment{RuntimeConfigJSON:json.RawMessage(tc.deployment)}
   provider:=&ProviderConnection{ConnectionJSON:json.RawMessage(tc.provider)}
   if got:=inferenceRouteRequiresBudget(deployment,provider);got!=tc.need{
    t.Fatalf("provider classification was overridden by deployment metadata: need=%t got=%t",tc.need,got)
   }
  })
 }
 for _,metadata:=range []string{
  `{"scheduling":{"cost_class":"paid"}}`,
  `{"scheduling":{"cost_class":"unknown"}}`,
 }{
  if inferenceRouteRequiresBudget(ModelDeployment{RuntimeConfigJSON:json.RawMessage(metadata)},nil){
   t.Fatal("non-provider local deployment suddenly required external provider budget")
  }
 }
}
