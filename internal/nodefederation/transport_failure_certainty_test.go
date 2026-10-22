package nodefederation

import (
 "context"
 "errors"
 "fmt"
 "testing"
)

func TestFederatedTransportOnlyCallsPreSendErrorsKnown(t *testing.T){
 tests:=[]struct{name string;err error;code string;known bool}{
  {"oversized before opening socket",ErrFederationPayloadTooLarge,"remote_payload_too_large",true},
  {"bad encoding before opening socket",ErrFederationPayloadInvalid,"remote_payload_invalid",true},
  {"wrapped oversized admission",fmt.Errorf("bounded: %w",ErrFederationPayloadTooLarge),"remote_payload_too_large",true},
  {"invalid peer reply",ErrFederationResponseInvalid,"remote_transport_error",false},
  {"oversized peer reply",ErrFederationResponseTooLarge,"remote_transport_error",false},
  {"peer 429 refusal",errors.New("peer HTTP 429"),"remote_transport_error",false},
  {"cancelled after dispatch uncertain",context.Canceled,"remote_transport_error",false},
 }
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   result:=classifyFederationTransportFailure(tc.err)
   if result.Code!=tc.code||result.OutcomeKnown!=tc.known||
    !errors.Is(result,tc.err){
    t.Fatalf("incorrect effect certainty on transport failure: %+v",result)
   }
  })
 }
}
