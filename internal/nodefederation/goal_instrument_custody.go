package nodefederation

import (
 "bytes"
 "crypto/ed25519"
 "crypto/sha256"
 "crypto/x509"
 "encoding/hex"
 "encoding/json"
 "errors"
 "strings"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/instrumentprobe"
)

// NodeInstrumentCustodyWitness is an optional, private, offline signed
// acknowledgement of operator-provided instrument evidence. It does not
// prove physical execution, trustworthy vantage, or device health.
type NodeInstrumentCustodyWitness struct {
 SigningPayload []byte `json:"signing_payload"`
 Signature []byte `json:"signature"`
}
const nodeCustodyDomain="OnePane/paired-node-instrument-custody/ed25519/v1"
const nodeCustodyClass="paired_node_key_countersigned_self_report_not_attested"

type nodeInstrumentCustodyEnvelope struct {
 Domain string `json:"domain"`
 NodeID string `json:"node_id"`
 NodeCertificateSHA256 string `json:"node_certificate_sha256"`
 InstrumentKeySHA256 string `json:"instrument_public_key_sha256"`
 ChallengeSHA256 string `json:"original_challenge_sha256"`
 LocalReceiptSHA256 string `json:"local_receipt_sha256"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 InstrumentKeyID string `json:"instrument_key_id"`
 ChallengeID string `json:"challenge_id"`
 DeclaredTargetID string `json:"declared_target_id"`
 TLSPinID string `json:"tls_pin_id"`
 SourceID string `json:"source_id"`
 Outcome string `json:"outcome"`
 ObservedAt int64 `json:"observed_at"`
 WitnessedAt int64 `json:"witnessed_at"`
 ProofClass string `json:"proof_class"`
}

func custodyHash(raw []byte)string{
 sum:=sha256.Sum256(raw)
 return "sha256:"+hex.EncodeToString(sum[:])
}

func custodyMessage(nodeID,fp string,instrumentKey ed25519.PublicKey,
 receipt instrumentprobe.LocalProbeReceipt,original instrumentprobe.Challenge,
 plan instrumentprobe.ProbePlan,witnessedAt int64)([]byte,error){
 if len(nodeID)<1||len(nodeID)>128||strings.TrimSpace(nodeID)!=nodeID||
  len(instrumentKey)!=ed25519.PublicKeySize||len(fp)!=71||
  witnessedAt<receipt.ObservedAt||witnessedAt>=original.ExpiresAt||
  witnessedAt-receipt.ObservedAt>60000{
  return nil,errors.New("Node custody requires a recent original report")
 }
 if err:=instrumentprobe.VerifyLocalReceiptAgainstApproval(
  receipt,instrumentKey,original,plan);err!=nil{return nil,err}
 report,err:=json.Marshal(receipt)
 if err!=nil{return nil,err}
 return json.Marshal(nodeInstrumentCustodyEnvelope{
  Domain:nodeCustodyDomain,NodeID:nodeID,NodeCertificateSHA256:fp,
  InstrumentKeySHA256:custodyHash(instrumentKey),
  ChallengeSHA256:custodyHash(original.SigningPayload),
  LocalReceiptSHA256:custodyHash(report),
  ProjectID:original.ProjectID,GoalID:original.GoalID,
  InstrumentKeyID:original.KeyID,ChallengeID:original.ID,
  DeclaredTargetID:plan.DeclaredTargetID,TLSPinID:plan.TLSPinID,
  SourceID:plan.SourceID,Outcome:receipt.Outcome,
  ObservedAt:receipt.ObservedAt,WitnessedAt:witnessedAt,
  ProofClass:nodeCustodyClass,
 })
}

func validateCustodyCert(cert *x509.Certificate,nodeID,pin string,
 witnessedAt int64)error{
 if cert==nil||len(cert.Raw)==0||cert.Subject.CommonName!=nodeID||
  !strings.HasPrefix(pin,"sha256:")||len(pin)!=71||
  CertificateFingerprint(cert.Raw)!=pin||
  cert.KeyUsage&x509.KeyUsageDigitalSignature==0{
  return errors.New("Node custody enrolled certificate mismatch")
 }
 at:=time.UnixMilli(witnessedAt)
 if at.Before(cert.NotBefore)||!at.Before(cert.NotAfter){
  return errors.New("Node custody certificate invalid at witness time")
 }
 allowed:=false
 for _,usage:=range cert.ExtKeyUsage{
  if usage==x509.ExtKeyUsageClientAuth{allowed=true}
 }
 if !allowed{return errors.New("Node custody requires client identity EKU")}
 if _,ok:=cert.PublicKey.(ed25519.PublicKey);!ok{
  return errors.New("Node custody requires an Ed25519 enrollment")
 }
 return nil
}

// SignLocalInstrumentCustody is explicitly invoked: zero sockets, no Tasks,
// no permission grant. The Node countersigns an independently verified
// instrument report with its in-memory enrolled federation Ed25519 key.
// Neither signature proves the claim was executed by a physical instrument.
func SignLocalInstrumentCustody(nodeID string,identity Identity,
 receipt instrumentprobe.LocalProbeReceipt,original instrumentprobe.Challenge,
 plan instrumentprobe.ProbePlan,instrumentKey ed25519.PublicKey,
 now time.Time)(NodeInstrumentCustodyWitness,error){
 if len(identity.Certificate.Certificate)!=1{
  return NodeInstrumentCustodyWitness{},errors.New("Node custody cert missing")
 }
 leaf,err:=x509.ParseCertificate(identity.Certificate.Certificate[0])
 if err!=nil{return NodeInstrumentCustodyWitness{},err}
 if err=validateCustodyCert(leaf,nodeID,identity.Fingerprint,now.UnixMilli());
  err!=nil{return NodeInstrumentCustodyWitness{},err}
 private,ok:=identity.Certificate.PrivateKey.(ed25519.PrivateKey)
 if !ok||len(private)!=ed25519.PrivateKeySize{
  return NodeInstrumentCustodyWitness{},errors.New("Node private key unavailable")
 }
 payload,err:=custodyMessage(nodeID,identity.Fingerprint,instrumentKey,
  receipt,original,plan,now.UnixMilli())
 if err!=nil{return NodeInstrumentCustodyWitness{},err}
 signature:=ed25519.Sign(private,payload)
 if !ed25519.Verify(leaf.PublicKey.(ed25519.PublicKey),payload,signature){
  return NodeInstrumentCustodyWitness{},errors.New("Node signer differs from enrolled cert")
 }
 return NodeInstrumentCustodyWitness{SigningPayload:payload,Signature:signature},nil
}

// VerifyLocalInstrumentCustody validates both independently enrolled keys,
// signed report and separately retained original OnePane approval files.
// NodeCertDER and pinned fingerprint come from an independent trusted channel.
// This is historical evidence custody, never device attest or authorisation.
func VerifyLocalInstrumentCustody(w NodeInstrumentCustodyWitness,
 nodeCertDER []byte,expectedNodeFingerprint string,
 instrumentKey ed25519.PublicKey,
 receipt instrumentprobe.LocalProbeReceipt,original instrumentprobe.Challenge,
 plan instrumentprobe.ProbePlan)error{
 if len(w.SigningPayload)<200||len(w.SigningPayload)>2048||
  len(w.Signature)!=ed25519.SignatureSize{
  return errors.New("Node custody witness missing or oversized")
 }
 leaf,err:=x509.ParseCertificate(nodeCertDER)
 if err!=nil{return err}
 var msg nodeInstrumentCustodyEnvelope
 if err=json.Unmarshal(w.SigningPayload,&msg);err!=nil{return err}
 if msg.Domain!=nodeCustodyDomain||msg.ProofClass!=nodeCustodyClass||
  msg.NodeCertificateSHA256!=expectedNodeFingerprint{
  return errors.New("Node custody enrollment or domain mismatch")
 }
 if err=validateCustodyCert(leaf,msg.NodeID,
  expectedNodeFingerprint,msg.WitnessedAt);err!=nil{return err}
 expected,err:=custodyMessage(msg.NodeID,expectedNodeFingerprint,instrumentKey,
  receipt,original,plan,msg.WitnessedAt)
 if err!=nil{return err}
 if !bytes.Equal(expected,w.SigningPayload){
  return errors.New("Node custody envelope does not bind original evidence")
 }
 if !ed25519.Verify(leaf.PublicKey.(ed25519.PublicKey),w.SigningPayload,w.Signature){
  return errors.New("Node custody Ed25519 signature invalid")
 }
 return nil
}
