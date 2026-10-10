package localai

import (
 "context"
 "errors"
 "fmt"
 "strings"
)

// ColibriPin is an explicit, durable per-deployment preference. It does not
// convert GGUF to a Colibri model or guarantee VRAM residency. An activated
// pinned Colibri deployment is excluded from the normal idle reaper and cannot
// be silently evicted by a competing Colibri model. The operator can unpin it.
type ColibriPinState struct {
 DeploymentID string `json:"deployment_id"`
 Pinned bool `json:"pinned"`
 Eligible bool `json:"eligible"`
 Runtime string `json:"runtime"`
 Note string `json:"note"`
}

func (s *Service) ColibriPin(ctx context.Context,deploymentID string)(ColibriPinState,error){
 var runtimeName,backend string
 var pinned int
 err:=s.db.QueryRowContext(ctx,`SELECT COALESCE(d.runtime_name,''),
 COALESCE(json_extract(d.runtime_config_json,'$.runtime_backend'),''),
 CASE WHEN json_extract(d.runtime_config_json,'$.colibri_pinned')=1 THEN 1 ELSE 0 END
 FROM model_deployments d JOIN managed_local_models m ON m.deployment_id=d.id
 WHERE d.id=? AND m.status<>'removed'`,deploymentID).Scan(&runtimeName,&backend,&pinned)
 if err!=nil{return ColibriPinState{},err}
 eligible:=strings.EqualFold(runtimeName,"colibri")&&strings.EqualFold(backend,"colibri")
 note:="Only installed Colibri-native model folders can be pinned. GGUF/llama.cpp models are not automatically converted."
 if eligible {
  note="A pinned model stays ready after activation and blocks automatic Colibri-to-Colibri eviction. It is not guaranteed to remain in VRAM under host pressure or shutdown."
 }
 return ColibriPinState{DeploymentID:deploymentID,Pinned:pinned==1,Eligible:eligible,Runtime:runtimeName,Note:note},nil
}

func (s *Service) SetColibriPin(ctx context.Context,deploymentID string,pin bool)(ColibriPinState,error){
 if strings.TrimSpace(deploymentID)==""{return ColibriPinState{},errors.New("deployment is required")}
 before,err:=s.ColibriPin(ctx,deploymentID)
 if err!=nil{return ColibriPinState{},err}
 if !before.Eligible{return ColibriPinState{},errors.New("Colibri pinning requires a registered native Colibri model; GGUF/llama.cpp is unsupported")}
 // Single SQL statement prevents lost updates to other runtime config fields.
 // Migration 0043 enforces at most one pinned Colibri deployment per Node,
 // including during concurrent requests. A failed unique constraint does not
 // unpin the existing model or fall back to another inference runtime.
 flag:="false";if pin{flag="true"}
 res,err:=s.db.ExecContext(ctx,`UPDATE model_deployments SET
 runtime_config_json=json_set(runtime_config_json,'$.colibri_pinned',json(?)),
 updated_at=?,revision=revision+1
 WHERE id=? AND LOWER(runtime_name)='colibri'
 AND json_extract(runtime_config_json,'$.runtime_backend')='colibri'
 AND EXISTS(SELECT 1 FROM managed_local_models m WHERE m.deployment_id=model_deployments.id AND m.status<>'removed')`,
 flag,s.clock.UnixMilli(),deploymentID)
 if err!=nil{
  if pin&&strings.Contains(strings.ToLower(err.Error()),"unique"){
   return ColibriPinState{},fmt.Errorf("another Colibri model is pinned on this Node; unpin it before selecting a different model")
  }
  return ColibriPinState{},err
 }
 n,_:=res.RowsAffected()
 if n!=1{return ColibriPinState{},errors.New("Colibri deployment changed or is unavailable")}
 return s.ColibriPin(ctx,deploymentID)
}
