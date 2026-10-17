package agentworker

import(
 "encoding/json"

 "github.com/DigiLogicTech/OnePane/internal/observation"
)

// Persisted Worker continuation is control-plane state, not model output.
// Reject null and duplicate object keys before resolving any wait or grant.
func readWorkerContinuationFields(raw json.RawMessage)(map[string]json.RawMessage,bool){
 if observation.ValidateUnambiguousJSON(raw)!=nil{return nil,false}
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil||fields==nil{return nil,false}
 return fields,true
}
