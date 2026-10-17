package nodefederation

import (
 "encoding/json"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/inference"
)

func TestRemoteInferenceReplyRefusesAmbiguousSuccess(t *testing.T){
 check:=func(name string,r RemoteInferenceResponse,wantSuccess,wantKnown bool,wantCode string){
  t.Helper()
  result,err:=validateRemoteInferenceReply(r)
  var te *inference.TransportError
  if wantSuccess{
   if err!=nil||string(result.ResponseJSON)!=`{"answer":"ok"}`{
    t.Fatalf("%s: well-formed completion rejected: %+v err=%v",name,result,err)
   }
   return
  }
  if !errors.As(err,&te)||te.Code!=wantCode||te.OutcomeKnown!=wantKnown||
   len(result.ResponseJSON)!=0{
   t.Fatalf("%s: wrong boundary result=%+v error=%v",name,result,err)
  }
 }
 good:=RemoteInferenceResponse{ResponseJSON:json.RawMessage(`{"answer":"ok"}`),
  UsageJSON:json.RawMessage(`{"tokens":4}`),OutcomeKnown:true}
 check("normal model reply",good,true,true,"")
 check("unknown empty peer reply",RemoteInferenceResponse{},false,false,"invalid_federation_reply")
 check("known without evidence",RemoteInferenceResponse{OutcomeKnown:true},false,false,"invalid_federation_reply")
 check("false known flag with model text",
  RemoteInferenceResponse{ResponseJSON:good.ResponseJSON},false,false,"invalid_federation_reply")
 check("bad usage JSON",
  RemoteInferenceResponse{ResponseJSON:good.ResponseJSON,
   UsageJSON:json.RawMessage(`{"tokens":`),OutcomeKnown:true},false,false,"invalid_federation_reply")
 check("reported known model failure",
  RemoteInferenceResponse{ErrorCode:"remote_inference_failed",OutcomeKnown:true},
  false,true,"remote_inference_failed")
 check("reported unknown model failure",
  RemoteInferenceResponse{ErrorCode:"duplicate_or_unknown_remote_request",OutcomeKnown:false},
  false,false,"duplicate_or_unknown_remote_request")
 check("error and success contradicted",
  RemoteInferenceResponse{ErrorCode:"failed",ResponseJSON:good.ResponseJSON,OutcomeKnown:true},
  false,false,"invalid_federation_reply")
 check("unbounded error code",
  RemoteInferenceResponse{ErrorCode:string(make([]byte,129)),OutcomeKnown:true},
  false,false,"invalid_federation_reply")
 check("unsafe error code",
  RemoteInferenceResponse{ErrorCode:"secret\ninjected",OutcomeKnown:true},
  false,false,"invalid_federation_reply")
}
