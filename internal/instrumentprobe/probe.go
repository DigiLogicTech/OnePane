// Package instrumentprobe implements a bounded, explicit one-shot instrument
// TCP connect on the local host. It does not attest which physical host ran
// it; detached Ed25519 signing below authenticates a separately held key.
package instrumentprobe

import (
 "bytes"
 "context"
 "crypto/ed25519"
 "crypto/sha256"
 "encoding/json"
 "errors"
 "fmt"
 "net"
 "net/netip"
 "strconv"
 "time"
)

const challengeDomainV1="OnePane/instrument-challenge/ed25519/v1"
const challengeDomainV2="OnePane/instrument-challenge/ed25519/v2"
const measurementDomain="OnePane/instrument-measurement/ed25519/v1"

// A Challenge is issued by OnePane's human-authorized challenge API.
// The caller must authenticate and independently trust the API transport.
type Challenge struct{
 ID string `json:"id"`
 KeyID string `json:"instrument_key_id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 Nonce []byte `json:"nonce"`
 IssuedAt int64 `json:"issued_at"`
 ExpiresAt int64 `json:"expires_at"`
 Status string `json:"status"`
 SigningPayload []byte `json:"signing_payload"`
}
type challengeEnvelope struct{
 Domain string `json:"domain"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 InstrumentKeyID string `json:"instrument_key_id"`
 ChallengeID string `json:"challenge_id"`
 DeclaredTargetID string `json:"declared_target_id"`
 TLSPinID string `json:"tls_pin_id"`
 PinnedCertificateSHA256 string `json:"pinned_certificate_sha256"`
 PlanHash string `json:"plan_hash"`
 TargetEndpointSHA256 string `json:"target_endpoint_sha256,omitempty"`
 Nonce []byte `json:"nonce"`
 IssuedAt int64 `json:"issued_at"`
 ExpiresAt int64 `json:"expires_at"`
}
type measurementEnvelope struct{
 Domain string `json:"domain"`
 ChallengePayload []byte `json:"challenge_payload"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 InstrumentKeyID string `json:"instrument_key_id"`
 ChallengeID string `json:"challenge_id"`
 DeclaredTargetID string `json:"declared_target_id"`
 TLSPinID string `json:"tls_pin_id"`
 SourceID string `json:"source_id"`
 MeasurementType string `json:"measurement_type"`
 Outcome string `json:"outcome"`
 ObservedAt int64 `json:"observed_at"`
}
type Dialer interface{
 DialContext(context.Context,string,string)(net.Conn,error)
}
// ProbePlan is a HUMAN-authorised, live permit snapshot obtained via
// authenticated OnePane API, then transferred over a separately trusted
// operator channel to the instrument. SHA-256 binds the original challenge
// bytes; it is NOT a server signature or physical-device attestation.
type ProbePlan struct {
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 InstrumentKeyID string `json:"instrument_key_id"`
 ChallengeID string `json:"challenge_id"`
 DeclaredTargetID string `json:"declared_target_id"`
 TLSPinID string `json:"tls_pin_id"`
 SourceID string `json:"source_id"`
 TargetIPv4 string `json:"target_ipv4"`
 TargetPort int `json:"target_port"`
 TimeoutMS int `json:"timeout_ms"`
 OriginalChallengeSHA256 string `json:"original_challenge_sha256"`
 ExpiresAt int64 `json:"expires_at"`
 ObserverVantageVerified bool `json:"observer_vantage_verified"`
 TargetDeviceIdentityVerified bool `json:"target_device_identity_verified"`
 AttestationClass string `json:"attestation_class"`
}
type Target struct {
 IPv4 string `json:"target_ipv4"`
 Port int `json:"target_port"`
 TimeoutMS int `json:"timeout_ms"`
 ConfirmedDeclaredTargetID string `json:"confirmed_declared_target_id"`
}
// LocalProbeReceipt is private to the operator's instrument. Raw IPv4 is
// NEVER included in OnePane's measurement API import request.
type LocalProbeReceipt struct {
 Challenge Challenge `json:"challenge"`
 Target Target `json:"target"`
 Outcome string `json:"outcome"`
 ObservedAt int64 `json:"observed_at"`
 LocalReportSignature []byte `json:"local_report_signature"`
}
type PreparedMeasurement struct {
 ChallengeID string `json:"challenge_id"`
 MeasurementType string `json:"measurement_type"`
 Outcome string `json:"outcome"`
 ObservedAt int64 `json:"observed_at"`
 SigningPayload []byte `json:"signing_payload"`
 ExpiresAt int64 `json:"expires_at"`
 ObserverVantageVerified bool `json:"observer_vantage_verified"`
}
type Acceptance struct{
 ChallengeID string `json:"challenge_id"`
 Outcome string `json:"outcome"`
 ObservedAt int64 `json:"observed_at"`
 Signature []byte `json:"signature"`
 ExpectedGoalRevision int64 `json:"expected_goal_revision"`
}

func canonicalSHA256(s string)bool{
 if len(s)!=71||s[:7]!="sha256:"{return false}
 for _,ch:=range s[7:]{
  if !((ch>='0'&&ch<='9')||(ch>='a'&&ch<='f')){return false}
 }
 return true
}
func ParseChallenge(ch Challenge,now time.Time)(string,error){
 if len(ch.SigningPayload)<128||len(ch.SigningPayload)>2048||
  len(ch.Nonce)!=32||ch.Status!="issued"||ch.ID==""||ch.ProjectID==""||ch.GoalID==""||
  ch.KeyID==""{return "",errors.New("missing or oversized instrument challenge")}
 var env challengeEnvelope
 if err:=json.Unmarshal(ch.SigningPayload,&env);err!=nil{return "",errors.New("invalid signed challenge envelope")}
 if (env.Domain!=challengeDomainV1&&env.Domain!=challengeDomainV2)||
  (env.Domain==challengeDomainV2&&!canonicalSHA256(env.TargetEndpointSHA256))||
  (env.Domain==challengeDomainV1&&env.TargetEndpointSHA256!=""){
  return "",errors.New("invalid instrument challenge version or endpoint digest")
 }
 if env.ChallengeID!=ch.ID||
  env.InstrumentKeyID!=ch.KeyID||env.ProjectID!=ch.ProjectID||
  env.GoalID!=ch.GoalID||env.TLSPinID==""||env.DeclaredTargetID==""||
  !bytes.Equal(env.Nonce,ch.Nonce)||env.IssuedAt!=ch.IssuedAt||
  env.ExpiresAt!=ch.ExpiresAt||ch.ExpiresAt<=ch.IssuedAt||
  ch.ExpiresAt-ch.IssuedAt>120000||
  !canonicalSHA256(env.PinnedCertificateSHA256)||!canonicalSHA256(env.PlanHash)||
  now.UnixMilli()<ch.IssuedAt||now.UnixMilli()>=ch.ExpiresAt{
  return "",errors.New("stale or inconsistent instrument challenge")
 }
 // Reject ambiguous or noncanonical JSON, including duplicate keys and
 // unknown fields, by requiring the EXACT Go v1 encoder output.
 canonical,err:=json.Marshal(env)
 if err!=nil||!bytes.Equal(canonical,ch.SigningPayload){
  return "",errors.New("noncanonical instrument challenge")
 }
 return env.DeclaredTargetID,nil
}

// ValidateProbePlan binds the plan to exactly the live human-issued nonce
// and canonical v1 challenge envelope before any network effect.
// endpointDigest matches the server's live human-approval hash. It never
// turns a manually supplied address into an attested physical device.
func endpointDigest(ip string,port int)string{
 raw:=sha256.Sum256([]byte(fmt.Sprintf(
  "OnePane/target-endpoint/v1|%s:%d",ip,port)))
 return fmt.Sprintf("sha256:%x",raw[:])
}

func ValidateProbePlan(ch Challenge,p ProbePlan,now time.Time)(Target,error){
 id,err:=ParseChallenge(ch,now)
 if err!=nil{return Target{},err}
 var challengeScope challengeEnvelope
 if err=json.Unmarshal(ch.SigningPayload,&challengeScope);err!=nil{
  return Target{},errors.New("invalid original TLS scope")
 }
 digest:=sha256.Sum256(ch.SigningPayload)
 expected:="sha256:"+fmt.Sprintf("%x",digest[:])
 if p.ProjectID!=ch.ProjectID||p.GoalID!=ch.GoalID||
  p.InstrumentKeyID!=ch.KeyID||p.ChallengeID!=ch.ID||
  p.DeclaredTargetID!=id||p.TLSPinID!=challengeScope.TLSPinID||
  p.SourceID==""||p.ExpiresAt!=ch.ExpiresAt||
  p.OriginalChallengeSHA256!=expected||
  challengeScope.Domain!=challengeDomainV2||
  challengeScope.TargetEndpointSHA256==""||
  challengeScope.TargetEndpointSHA256!=endpointDigest(p.TargetIPv4,p.TargetPort)||
  p.AttestationClass!="human_authorised_endpoint_snapshot_not_device_attestation"||
  p.ObserverVantageVerified||p.TargetDeviceIdentityVerified{
  return Target{},errors.New("one-shot target plan diverges from approved challenge")
 }
 target:=Target{IPv4:p.TargetIPv4,Port:p.TargetPort,
  TimeoutMS:p.TimeoutMS,ConfirmedDeclaredTargetID:id}
 if err=ValidateTarget(target,id);err!=nil{return Target{},err}
 return target,nil
}

// ProbeApprovedPlan reduces mistyped/supplied target ambiguity by using
// the live human-only OnePane permit snapshot rather than free-form CLI
// address flags. This only authenticates endpoint consent when the plan
// was transferred over a separately trusted authenticated human channel.
func ProbeApprovedPlan(ctx context.Context,ch Challenge,plan ProbePlan,
 private ed25519.PrivateKey,dialer Dialer,now func()time.Time)(LocalProbeReceipt,error){
 if now==nil{now=time.Now}
 target,err:=ValidateProbePlan(ch,plan,now())
 if err!=nil{return LocalProbeReceipt{},err}
 return ProbeOnce(ctx,ch,target,private,dialer,now)
}
func ValidateTarget(t Target,expectedTargetID string)error{
 ip,err:=netip.ParseAddr(t.IPv4)
 if err!=nil||!ip.Is4()||!ip.IsPrivate()||ip.String()!=t.IPv4{
  return errors.New("probe requires one canonical private IPv4 literal")
 }
 if t.Port<1||t.Port>65535||t.TimeoutMS<100||t.TimeoutMS>1500||
  t.ConfirmedDeclaredTargetID==""||t.ConfirmedDeclaredTargetID!=expectedTargetID{
  return errors.New("exact human-declared endpoint confirmation or timeout invalid")
 }
 return nil
}
func localReceiptSigningBytes(r LocalProbeReceipt)([]byte,error){
 type report struct{
  Domain string `json:"domain"`
  ChallengePayload []byte `json:"challenge_payload"`
  Target Target `json:"target"`
  Outcome string `json:"outcome"`
  ObservedAt int64 `json:"observed_at"`
 }
 return json.Marshal(report{"OnePane/local-one-shot-probe/ed25519/v1",
  r.Challenge.SigningPayload,r.Target,r.Outcome,r.ObservedAt})
}
func validOutcome(o string)bool{
 return o=="connected"||o=="not_connected"||o=="timed_out"||o=="unknown"
}

// validateManualChallengeTarget closes a legacy gap: a v2 challenge already
// seals its approved IPv4:port digest, so even an explicit manual-target
// override MUST NOT silently reach a different private endpoint. Historic
// v1 challenges lack the digest and remain explicitly less protected.
// This is an integrity constraint on approved address bytes, NOT proof
// that the physical device or local executable is trustworthy.
func validateManualChallengeTarget(ch Challenge,target Target)error{
 var envelope challengeEnvelope
 if err:=json.Unmarshal(ch.SigningPayload,&envelope);err!=nil{
  return errors.New("unreadable challenge endpoint scope")
 }
 if envelope.Domain==challengeDomainV2&&
  envelope.TargetEndpointSHA256!=endpointDigest(target.IPv4,target.Port){
  return errors.New("selected instrument endpoint differs from human-approved v2 scope")
 }
 return nil
}

// ProbeOnce opens AT MOST ONE TCP4 connection to one operator-selected private
// IPv4 socket. It does not perform DNS, a scan, TLS identity check, HTTP,
// redirects, application writes, retries, Tasks or automatic monitoring.
func ProbeOnce(ctx context.Context,ch Challenge,target Target,
 private ed25519.PrivateKey,dialer Dialer,now func()time.Time)(LocalProbeReceipt,error){
 if now==nil{now=time.Now}
 if dialer==nil||len(private)!=ed25519.PrivateKeySize{
  return LocalProbeReceipt{},errors.New("instrument or private signing key unavailable")
 }
 id,err:=ParseChallenge(ch,now())
 if err!=nil{return LocalProbeReceipt{},err}
 if err=ValidateTarget(target,id);err!=nil{return LocalProbeReceipt{},err}
 if err=validateManualChallengeTarget(ch,target);err!=nil{return LocalProbeReceipt{},err}
 if ctx.Err()!=nil{return LocalProbeReceipt{},ctx.Err()}
 start:=now()
 deadline:=time.Duration(target.TimeoutMS)*time.Millisecond
 sub,cancel:=context.WithTimeout(ctx,deadline)
 defer cancel()
 address:=net.JoinHostPort(target.IPv4,strconv.Itoa(target.Port))
 conn,dialErr:=dialer.DialContext(sub,"tcp4",address)
 if conn!=nil{_ = conn.Close()}
 observedAt:=now().UnixMilli()
 if observedAt<ch.IssuedAt||observedAt>=ch.ExpiresAt{
  return LocalProbeReceipt{},errors.New("challenge expired during probe")
 }
 outcome:="not_connected"
 if ctx.Err()!=nil{
  return LocalProbeReceipt{},ctx.Err()
 }else if errors.Is(sub.Err(),context.DeadlineExceeded)||
  errors.Is(dialErr,context.DeadlineExceeded)||
  now().Sub(start)>deadline{outcome="timed_out"
 }else if dialErr==nil&&conn!=nil{outcome="connected"}
 receipt:=LocalProbeReceipt{Challenge:ch,Target:target,
  Outcome:outcome,ObservedAt:observedAt}
 signed,err:=localReceiptSigningBytes(receipt)
 if err!=nil{return LocalProbeReceipt{},err}
 receipt.LocalReportSignature=ed25519.Sign(private,signed)
 if !ed25519.Verify(private.Public().(ed25519.PublicKey),signed,receipt.LocalReportSignature){
  return LocalProbeReceipt{},errors.New("instrument private key failed self-check")
 }
 return receipt,nil
}

// SignPrepared refuses to sign any server-prepared bytes that diverge from
// the locally signed, one-shot probe's exact outcome/observation/target
// identity. This still DOES NOT cryptographically attest physical hardware
// or ensure the operator did not substitute an executable or target address.
func SignPrepared(r LocalProbeReceipt,prepared PreparedMeasurement,
 private ed25519.PrivateKey,expectedGoalRevision int64,now time.Time)(Acceptance,error){
 if len(private)!=ed25519.PrivateKeySize||expectedGoalRevision<1||
  !validOutcome(r.Outcome)||len(r.LocalReportSignature)!=64{
  return Acceptance{},errors.New("invalid local instrument signing state")
 }
 id,err:=ParseChallenge(r.Challenge,now)
 if err!=nil{return Acceptance{},err}
 if err=ValidateTarget(r.Target,id);err!=nil{return Acceptance{},err}
 if err=validateManualChallengeTarget(r.Challenge,r.Target);err!=nil{
  return Acceptance{},err
 }
 var original challengeEnvelope
 if err=json.Unmarshal(r.Challenge.SigningPayload,&original);err!=nil{
  return Acceptance{},errors.New("invalid original signing challenge")
 }
 localBytes,err:=localReceiptSigningBytes(r)
 if err!=nil{return Acceptance{},err}
 pub:=private.Public().(ed25519.PublicKey)
 if !ed25519.Verify(pub,localBytes,r.LocalReportSignature){
  return Acceptance{},errors.New("local probe signature does not verify")
 }
 var env measurementEnvelope
 if len(prepared.SigningPayload)<256||len(prepared.SigningPayload)>4096||
  json.Unmarshal(prepared.SigningPayload,&env)!=nil{
  return Acceptance{},errors.New("invalid prepared measurement")
 }
 canonical,err:=json.Marshal(env)
 if err!=nil||!bytes.Equal(canonical,prepared.SigningPayload)||
  env.Domain!=measurementDomain||env.MeasurementType!="endpoint_tcp_connect_claim"||
  !bytes.Equal(env.ChallengePayload,r.Challenge.SigningPayload)||
  env.ProjectID!=r.Challenge.ProjectID||env.GoalID!=r.Challenge.GoalID||
  env.InstrumentKeyID!=r.Challenge.KeyID||env.ChallengeID!=r.Challenge.ID||
  env.DeclaredTargetID!=id||env.TLSPinID!=original.TLSPinID||
  env.SourceID==""||
  env.ObservedAt!=r.ObservedAt||env.Outcome!=r.Outcome||
  prepared.ChallengeID!=r.Challenge.ID||
  prepared.MeasurementType!="endpoint_tcp_connect_claim"||
  prepared.Outcome!=r.Outcome||prepared.ObservedAt!=r.ObservedAt||
  prepared.ExpiresAt!=r.Challenge.ExpiresAt||
  prepared.ObserverVantageVerified{
  return Acceptance{},errors.New("prepared bytes do not match local one-shot report")
 }
 sig:=ed25519.Sign(private,prepared.SigningPayload)
 // Check the resulting detached signature with the claimed approved
 // public key. A corrupt private-key file must not yield a false success.
 if !ed25519.Verify(pub,prepared.SigningPayload,sig){
  return Acceptance{},errors.New("instrument private key failed signing self-check")
 }
 return Acceptance{
  ChallengeID:r.Challenge.ID,Outcome:r.Outcome,ObservedAt:r.ObservedAt,
  Signature:sig,ExpectedGoalRevision:expectedGoalRevision,
 },nil
}

func SignatureDigest(sig []byte)string{
 digest:=sha256.Sum256(sig)
 return fmt.Sprintf("sha256:%x",digest[:])
}
