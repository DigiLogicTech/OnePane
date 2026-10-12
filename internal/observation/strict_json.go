package observation

import (
 "bytes"
 "encoding/json"
 "fmt"
 "io"
)

// validateUnambiguousObservationJSON rejects duplicate object keys, including
// Unicode-escaped aliases, at every depth. The scanner consumes one JSON
// token stream without materialising a second complete value in memory.
// This prevents differently implemented consumers from interpreting a
// signature-covered Observation with conflicting field values.
func validateUnambiguousObservationJSON(raw json.RawMessage)error{
 if len(raw)==0{return fmt.Errorf("observation JSON is empty")}
 dec:=json.NewDecoder(bytes.NewReader(raw))
 dec.UseNumber()
 if err:=scanUniqueObservationValue(dec,0);err!=nil{return err}
 if _,err:=dec.Token();err!=io.EOF{
  if err==nil{return fmt.Errorf("multiple JSON roots")}
  return err
 }
 return nil
}
func scanUniqueObservationValue(dec *json.Decoder,depth int)error{
 if depth>64{return fmt.Errorf("observation JSON nesting exceeds 64")}
 token,err:=dec.Token()
 if err!=nil{return err}
 delim,ok:=token.(json.Delim)
 if !ok{
  switch token.(type){
  case nil,bool,string,json.Number,float64:
   return nil
  default:
   return fmt.Errorf("invalid JSON value type")
  }
 }
 switch delim{
 case '{':
  names:=make(map[string]struct{})
  for dec.More(){
   keyToken,err:=dec.Token()
   if err!=nil{return err}
   key,ok:=keyToken.(string)
   if !ok{return fmt.Errorf("JSON object key must be a string")}
   if _,exists:=names[key];exists{
    // Never echo private field names into HTTP errors or audit events.
    return fmt.Errorf("duplicate JSON object member")
   }
   names[key]=struct{}{}
   if err:=scanUniqueObservationValue(dec,depth+1);err!=nil{return err}
  }
  closeToken,err:=dec.Token()
  if err!=nil{return err}
  if closeToken!=json.Delim('}'){return fmt.Errorf("unclosed JSON object")}
  return nil
 case '[':
  for dec.More(){
   if err:=scanUniqueObservationValue(dec,depth+1);err!=nil{return err}
  }
  closeToken,err:=dec.Token()
  if err!=nil{return err}
  if closeToken!=json.Delim(']'){return fmt.Errorf("unclosed JSON array")}
  return nil
 default:
  return fmt.Errorf("unexpected JSON delimiter")
 }
}
