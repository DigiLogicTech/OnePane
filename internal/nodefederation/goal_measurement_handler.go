package nodefederation

import (
 "bytes"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/observation"
)

// Strict 2 KiB envelope limit and duplicate-key rejection apply only to
// this opt-in transport. Large raw telemetry or ambiguous JSON is excluded.
func decodeNodeMeasurement(w http.ResponseWriter,r *http.Request,
 out *PeerNodeMeasurement)bool{
 raw,err:=io.ReadAll(http.MaxBytesReader(w,r.Body,2048))
 if err!=nil||observation.ValidateUnambiguousJSON(raw)!=nil{
  writeJSON(w,400,map[string]string{"error":"invalid measurement envelope"})
  return false
 }
 dec:=json.NewDecoder(bytes.NewReader(raw))
 dec.DisallowUnknownFields()
 if err=dec.Decode(out);err!=nil{
  writeJSON(w,400,map[string]string{"error":"invalid measurement envelope"})
  return false
 }
 if err=dec.Decode(new(any));err!=io.EOF{
  writeJSON(w,400,map[string]string{"error":"invalid measurement envelope"})
  return false
 }
 return true
}

// mTLS certificate possession and human-reviewed pairing identify the peer
// Node transport. The payload can only report its own connectivity status;
// no physical host, scan targets, credentials, or trusted-health state.
func(s *Server)receiveNodeMeasurement(w http.ResponseWriter,r *http.Request){
 if s.svc==nil{
  writeJSON(w,503,map[string]string{"error":"Node telemetry unavailable"})
  return
 }
 if r.TLS==nil||len(r.TLS.PeerCertificates)!=1{
  writeJSON(w,401,map[string]string{"error":"paired Node certificate required"})
  return
 }
 cert:=r.TLS.PeerCertificates[0]
 now:=time.Now()
 if cert.NotBefore.After(now)||!cert.NotAfter.After(now){
  writeJSON(w,401,map[string]string{"error":"current Node certificate required"})
  return
 }
 peer,err:=s.svc.peerFromTLS(r)
 if err!=nil{
  writeJSON(w,401,map[string]string{"error":"paired Node certificate required"})
  return
 }
 // Rate-limit every authenticated peer attempt BEFORE parsing even
 // malformed JSON. An active peer cannot bypass this by varying grant IDs.
 fingerprint:=CertificateFingerprint(cert.Raw)
 if err=s.svc.ReservePairedNodeMeasurementRequest(r.Context(),
  peer,fingerprint);err!=nil{
  if errors.Is(err,ErrNodeMeasurementBudget){
   w.Header().Set("Retry-After","60")
   writeJSON(w,429,map[string]string{"error":"Node telemetry request rate limit"})
  }else if errors.Is(err,ErrNodeMeasurementNotAdmitted){
   writeJSON(w,403,map[string]string{"error":"Node telemetry not authorised"})
  }else{
   writeJSON(w,500,map[string]string{"error":"Node telemetry admission unavailable"})
  }
  return
 }
 var input PeerNodeMeasurement
 if !decodeNodeMeasurement(w,r,&input){return}
 receipt,err:=s.svc.AcceptPairedNodeMeasurement(r.Context(),peer,
  fingerprint,input)
 if err!=nil{
  switch{
  case errors.Is(err,ErrNodeMeasurementBudget):
   w.Header().Set("Retry-After","60")
   writeJSON(w,429,map[string]string{"error":"Node telemetry rate limit"})
  case errors.Is(err,ErrNodeMeasurementReplay):
   writeJSON(w,409,map[string]string{"error":"Node telemetry sequence conflict"})
  case errors.Is(err,ErrNodeMeasurementNotAdmitted):
   writeJSON(w,403,map[string]string{"error":"Node telemetry not authorised"})
  default:
   writeJSON(w,500,map[string]string{"error":"Node telemetry not recorded"})
  }
  return
 }
 writeJSON(w,201,receipt)
}
