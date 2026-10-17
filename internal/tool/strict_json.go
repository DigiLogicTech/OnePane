package tool

import (
 "encoding/json"
 "fmt"
)

// decodeUniqueJSONValue rejects duplicate keys at every nesting level,
// including equivalent Unicode-escaped spellings. It parses the value only
// once, avoiding the extra full-buffer decode on RAM-constrained hosts.
//
// A model-authored Tool request must not contain two competing instructions
// for the same field: downstream adapters and permit hashes must agree on
// one canonical interpretation before any authority is consumed.
func decodeUniqueJSONValue(dec *json.Decoder,depth int)(any,error){
 if depth>64{return nil,fmt.Errorf("JSON nesting exceeds 64 levels")}
 token,err:=dec.Token()
 if err!=nil{return nil,err}
 delim,ok:=token.(json.Delim)
 if !ok{
  switch token.(type){
  case nil,bool,string,json.Number,float64:
   return token,nil
  default:
   return nil,fmt.Errorf("unsupported JSON token %T",token)
  }
 }
 switch delim{
 case '{':
  obj:=make(map[string]any)
  for dec.More(){
   keyToken,err:=dec.Token()
   if err!=nil{return nil,err}
   key,ok:=keyToken.(string)
   if !ok{return nil,fmt.Errorf("JSON object key must be a string")}
   if _,exists:=obj[key];exists{
    return nil,fmt.Errorf("duplicate JSON key %q",key)
   }
   value,err:=decodeUniqueJSONValue(dec,depth+1)
   if err!=nil{return nil,err}
   obj[key]=value
  }
  ending,err:=dec.Token()
  if err!=nil{return nil,err}
  if ending!=json.Delim('}'){
   return nil,fmt.Errorf("invalid JSON object termination")
  }
  return obj,nil
 case '[':
  list:=make([]any,0)
  for dec.More(){
   value,err:=decodeUniqueJSONValue(dec,depth+1)
   if err!=nil{return nil,err}
   list=append(list,value)
  }
  ending,err:=dec.Token()
  if err!=nil{return nil,err}
  if ending!=json.Delim(']'){
   return nil,fmt.Errorf("invalid JSON array termination")
  }
  return list,nil
 default:
  return nil,fmt.Errorf("unexpected JSON delimiter %q",delim)
 }
}
