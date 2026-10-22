package projectworkspace

import(
 "bytes"
 "context"
 "crypto/ed25519"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// SignedMeasurementClaim is an instrument's key-authenticated CLAIM of a
// TCP-connect outcome. The observer's hardware, its actual vantage and
// physical-device identity are NOT independently verified.
type SignedMeasurementClaim struct{
 ID string `json:"id"`
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
 AcceptedAt int64 `json:"accepted_at"`
 SignatureSHA256 string `json:"signature_sha256"`
 ProofClass string `json:"proof_class"`
 ObserverVantageVerified bool `json:"observer_vantage_verified"`
 DeviceIdentityVerified bool `json:"device_identity_verified"`
 TargetCorrelationEstablished bool `json:"target_correlation_established"`
 RemediationAuthorized bool `json:"remediation_authorized"`
}

type SignedMeasurementCommand struct{
 ProjectID,GoalID,KeyID,ChallengeID,ActorPrincipalID string
 ExpectedGoalRevision,ObservedAt int64
 Outcome string
 Signature []byte
}

type InstrumentMeasurementSigningPayload struct{
 ChallengeID string `json:"challenge_id"`
 MeasurementType string `json:"measurement_type"`
 Outcome string `json:"outcome"`
 ObservedAt int64 `json:"observed_at"`
 SigningPayload []byte `json:"signing_payload"`
 ExpiresAt int64 `json:"expires_at"`
 // ObservedAt is a source-reported timestamp: its accuracy is unproven.
 ObserverVantageVerified bool `json:"observer_vantage_verified"`
}

type instrumentMeasurementEnvelope struct{
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

type InstrumentMeasurementVerificationBundle struct{
 Claim SignedMeasurementClaim `json:"claim"`
 PublicKey []byte `json:"public_key"`
 SigningPayload []byte `json:"signing_payload"`
 Signature []byte `json:"signature"`
 DetachedSignatureVerified bool `json:"detached_signature_verified"`
}

const instrumentMeasurementColumns=`id,project_id,goal_id,instrument_key_id,
 challenge_id,declared_target_id,tls_pin_id,source_id,measurement_type,
 outcome,observed_at,accepted_at,signature_sha256,proof_class`

const instrumentMeasurementColumnsQualified=`m.id,m.project_id,m.goal_id,
 m.instrument_key_id,m.challenge_id,m.declared_target_id,m.tls_pin_id,
 m.source_id,m.measurement_type,m.outcome,m.observed_at,m.accepted_at,
 m.signature_sha256,m.proof_class`

func scanSignedMeasurement(row scanner)(SignedMeasurementClaim,error){
 var p SignedMeasurementClaim
 err:=row.Scan(&p.ID,&p.ProjectID,&p.GoalID,&p.InstrumentKeyID,
  &p.ChallengeID,&p.DeclaredTargetID,&p.TLSPinID,&p.SourceID,
  &p.MeasurementType,&p.Outcome,&p.ObservedAt,&p.AcceptedAt,
  &p.SignatureSHA256,&p.ProofClass)
 return p,err
}

func validInstrumentMeasurementOutcome(outcome string)bool{
 switch outcome{
 case "connected","not_connected","timed_out","unknown":return true
 default:return false
 }
}

func measurementSigningBytes(k GoalInstrumentKey,pin GoalTLSPin,
 ch GoalInstrumentChallenge,sourceID,outcome string,observedAt int64)([]byte,error){
 return json.Marshal(instrumentMeasurementEnvelope{
  Domain:"OnePane/instrument-measurement/ed25519/v1",
  ChallengePayload:ch.SigningPayload,
  ProjectID:k.ProjectID,GoalID:k.GoalID,InstrumentKeyID:k.ID,
  ChallengeID:ch.ID,DeclaredTargetID:k.DeclaredTargetID,
  TLSPinID:pin.ID,SourceID:sourceID,
  MeasurementType:"endpoint_tcp_connect_claim",
  Outcome:outcome,ObservedAt:observedAt,
 })
}

// loadInstrumentMeasurementScopeTx is shared by preparation and admission;
// both require the current exact human-approved Goal, private TCP endpoint,
// TLS-pin scope, human roles and original crypto enrollment.
func(s *Service)loadInstrumentMeasurementScopeTx(ctx context.Context,tx storage.Tx,
 c SignedMeasurementCommand,now int64)(
 GoalInstrumentKey,GoalTLSPin,GoalInstrumentChallenge,string,error){
 var emptyKey GoalInstrumentKey
 var emptyPin GoalTLSPin
 var emptyChallenge GoalInstrumentChallenge
 p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
 if err!=nil{return emptyKey,emptyPin,emptyChallenge,"",err}
 if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{
  return emptyKey,emptyPin,emptyChallenge,"",err
 }
 if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{
  return emptyKey,emptyPin,emptyChallenge,"",err
 }
 key,pin,err:=s.activeInstrumentKeyTx(ctx,tx,c.ProjectID,c.GoalID,c.KeyID,now)
 if err!=nil{return emptyKey,emptyPin,emptyChallenge,"",err}
 var revision int64
 err=tx.QueryRowContext(ctx,`SELECT revision FROM project_continuous_goals
  WHERE id=? AND project_id=?`,c.GoalID,c.ProjectID).Scan(&revision)
 if err!=nil{return emptyKey,emptyPin,emptyChallenge,"",err}
 if revision!=c.ExpectedGoalRevision{
  return emptyKey,emptyPin,emptyChallenge,"",ErrRevisionConflict
 }
 scope,err:=s.activePinnedTargetTx(ctx,tx,c.ProjectID,c.GoalID,key.DeclaredTargetID,now)
 if err!=nil{return emptyKey,emptyPin,emptyChallenge,"",err}
 if scope.Permit.ID!=pin.PermitID||scope.Permit.Revision!=pin.PermitRevision||
  scope.Target.ID!=key.DeclaredTargetID||scope.Target.PlanHash!=key.PlanHash||
  scope.Target.ProjectWorkspaceID!=key.ProjectWorkspaceID{
  return emptyKey,emptyPin,emptyChallenge,"",ErrInvalidTransition
 }
 var ch GoalInstrumentChallenge
 ch.ID=c.ChallengeID;ch.KeyID=c.KeyID;ch.ProjectID=c.ProjectID;ch.GoalID=c.GoalID
 err=tx.QueryRowContext(ctx,`SELECT nonce,signing_payload,issued_at,expires_at,status
  FROM project_goal_instrument_challenges
  WHERE id=? AND instrument_key_id=? AND project_id=? AND goal_id=?`,
  c.ChallengeID,c.KeyID,c.ProjectID,c.GoalID).
  Scan(&ch.Nonce,&ch.SigningPayload,&ch.IssuedAt,&ch.ExpiresAt,&ch.Status)
 if errors.Is(err,sql.ErrNoRows){return emptyKey,emptyPin,emptyChallenge,"",ErrInvalidTransition}
 if err!=nil{return emptyKey,emptyPin,emptyChallenge,"",err}
 if ch.Status!="issued"||ch.ExpiresAt<=now||ch.IssuedAt>now||
  ch.IssuedAt<key.ApprovedAt||c.ObservedAt<ch.IssuedAt||
  c.ObservedAt>now||c.ObservedAt>=ch.ExpiresAt{
  return emptyKey,emptyPin,emptyChallenge,"",ErrInvalidTransition
 }
 expected,err:=instrumentSigningBytes(key,pin,ch)
 if err!=nil{return emptyKey,emptyPin,emptyChallenge,"",err}
 if !bytes.Equal(ch.SigningPayload,expected){
  return emptyKey,emptyPin,emptyChallenge,"",ErrInvalidTransition
 }
 return key,pin,ch,scope.Target.SourceID,nil
}

// PrepareGoalInstrumentMeasurement returns exact bytes to sign WITHOUT
// consuming the challenge or opening a socket. A successful prepare does
// not assert a measurement happened; it remains a user-mediated statement.
func(s *Service)PrepareGoalInstrumentMeasurement(ctx context.Context,
 c SignedMeasurementCommand)(InstrumentMeasurementSigningPayload,error){
 if !validMeasurementCommand(c,false){
  return InstrumentMeasurementSigningPayload{},ErrInvalidCommand
 }
 now:=s.clock.UnixMilli()
 // Read within a consistent transaction; admission ALWAYS rechecks.
 var result InstrumentMeasurementSigningPayload
 err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  key,pin,ch,source,err:=s.loadInstrumentMeasurementScopeTx(ctx,tx,c,now)
  if err!=nil{return err}
  payload,err:=measurementSigningBytes(key,pin,ch,source,c.Outcome,c.ObservedAt)
  if err!=nil{return err}
  result=InstrumentMeasurementSigningPayload{
   ChallengeID:ch.ID,MeasurementType:"endpoint_tcp_connect_claim",
   Outcome:c.Outcome,ObservedAt:c.ObservedAt,SigningPayload:payload,
   ExpiresAt:ch.ExpiresAt,
  }
  return nil
 })
 if err!=nil{return InstrumentMeasurementSigningPayload{},err}
 return result,nil
}

func validMeasurementCommand(c SignedMeasurementCommand,requireSig bool)bool{
 if c.ProjectID==""||c.GoalID==""||c.KeyID==""||c.ChallengeID==""||
  c.ActorPrincipalID==""||c.ExpectedGoalRevision<1||c.ObservedAt<=0||
  !validInstrumentMeasurementOutcome(c.Outcome){return false}
 if requireSig&&len(c.Signature)!=ed25519.SignatureSize{return false}
 return true
}

func(s *Service)AcceptGoalInstrumentMeasurement(ctx context.Context,
 c SignedMeasurementCommand)(SignedMeasurementClaim,error){
 if !validMeasurementCommand(c,true){
  return SignedMeasurementClaim{},ErrInvalidCommand
 }
 idv,err:=s.ids.New("gmeasure")
 if err!=nil{return SignedMeasurementClaim{},err}
 evt,err:=s.ids.New("evt")
 if err!=nil{return SignedMeasurementClaim{},err}
 var result SignedMeasurementClaim
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  key,pin,ch,source,err:=s.loadInstrumentMeasurementScopeTx(ctx,tx,c,now)
  if err!=nil{return err}
  payload,err:=measurementSigningBytes(key,pin,ch,source,c.Outcome,c.ObservedAt)
  if err!=nil{return err}
  if !ed25519.Verify(ed25519.PublicKey(key.PublicKey),payload,c.Signature){
   return ErrInvalidCommand
  }
  updated,err:=tx.ExecContext(ctx,`UPDATE project_goal_instrument_challenges
   SET status='accepted',accepted_at=? WHERE id=? AND status='issued'
    AND expires_at>?`,now,ch.ID,now)
  if err!=nil{return err}
  rows,err:=updated.RowsAffected();if err!=nil{return err}
  if rows!=1{return ErrInvalidTransition}
  sum:=sha256.Sum256(c.Signature)
  result=SignedMeasurementClaim{
   ID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,
   InstrumentKeyID:key.ID,ChallengeID:ch.ID,
   DeclaredTargetID:key.DeclaredTargetID,TLSPinID:pin.ID,
   SourceID:source,MeasurementType:"endpoint_tcp_connect_claim",
   Outcome:c.Outcome,ObservedAt:c.ObservedAt,AcceptedAt:now,
   SignatureSHA256:"sha256:"+hex.EncodeToString(sum[:]),
   ProofClass:"instrument_self_report_signed_not_attested",
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_instrument_measurement_claims(
   id,project_id,goal_id,instrument_key_id,challenge_id,
   declared_target_id,tls_pin_id,source_id,measurement_type,outcome,
   observed_at,accepted_at,signed_payload,signature,signature_sha256,proof_class)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
   result.ID,result.ProjectID,result.GoalID,result.InstrumentKeyID,
   result.ChallengeID,result.DeclaredTargetID,result.TLSPinID,
   result.SourceID,result.MeasurementType,result.Outcome,
   result.ObservedAt,result.AcceptedAt,payload,c.Signature,
   result.SignatureSHA256,result.ProofClass)
  if err!=nil{return err}
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"claim_id":idv,"key_id":key.ID,
   "challenge_id":ch.ID,"proof_class":result.ProofClass,
   "measurement_truth_verified":false,"vantage_verified":false,
   "target_correlation_established":false,
   "device_identity_verified":false,"remediation_authorized":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_instrument_measurement_claim_accepted",
   AggregateType:"project_goal_instrument_measurement",AggregateID:idv,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return SignedMeasurementClaim{},err}
 return result,nil
}

func(s *Service)ListGoalInstrumentMeasurements(ctx context.Context,
 projectID,goalID string)([]SignedMeasurementClaim,error){
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var exists int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces t ON t.id=p.workspace_id AND t.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.project_id=? AND g.id=?`,projectID,goalID).Scan(&exists)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+instrumentMeasurementColumns+`
  FROM project_goal_instrument_measurement_claims
  WHERE project_id=? AND goal_id=?
  ORDER BY accepted_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]SignedMeasurementClaim{}
 for rows.Next(){
  claim,err:=scanSignedMeasurement(rows)
  if err!=nil{return nil,err}
  out=append(out,claim)
 }
 return out,rows.Err()
}

func(s *Service)GetGoalInstrumentMeasurementBundle(ctx context.Context,
 projectID,goalID,claimID string)(InstrumentMeasurementVerificationBundle,error){
 if projectID==""||goalID==""||claimID==""{
  return InstrumentMeasurementVerificationBundle{},ErrInvalidCommand
 }
 var result InstrumentMeasurementVerificationBundle
 var chPayload []byte
 var nonce []byte
 var pinHash,keyPlan,targetIP string
 var targetPort int
 var issued,expires int64
 err:=s.db.QueryRowContext(ctx,`SELECT `+instrumentMeasurementColumnsQualified+`,
  m.signed_payload,m.signature,k.public_key,
  ch.signing_payload,ch.nonce,ch.issued_at,ch.expires_at,
  pin.certificate_sha256,k.plan_hash,target.target_ipv4,target.target_port
  FROM project_goal_instrument_measurement_claims m
  JOIN project_goal_instrument_keys k ON k.id=m.instrument_key_id
   AND k.project_id=m.project_id AND k.goal_id=m.goal_id
   AND k.declared_target_id=m.declared_target_id
   AND k.tls_pin_id=m.tls_pin_id
  JOIN project_goal_instrument_challenges ch ON ch.id=m.challenge_id
   AND ch.instrument_key_id=m.instrument_key_id
   AND ch.project_id=m.project_id AND ch.goal_id=m.goal_id
   AND ch.status='accepted'
  JOIN project_goal_tls_endpoint_pins pin ON pin.id=m.tls_pin_id
   AND pin.project_id=m.project_id AND pin.goal_id=m.goal_id
   AND pin.declared_target_id=m.declared_target_id
  JOIN project_goal_tcp_declared_target_bindings target
   ON target.id=m.declared_target_id
   AND target.project_id=m.project_id AND target.goal_id=m.goal_id
   AND target.source_id=m.source_id AND target.permit_id=pin.permit_id
   AND target.plan_hash=k.plan_hash
  JOIN project_continuous_goals g ON g.id=m.goal_id AND g.project_id=m.project_id
  JOIN projects p ON p.id=m.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE m.id=? AND m.project_id=? AND m.goal_id=?`,
  claimID,projectID,goalID).
  Scan(&result.Claim.ID,&result.Claim.ProjectID,&result.Claim.GoalID,
   &result.Claim.InstrumentKeyID,&result.Claim.ChallengeID,
   &result.Claim.DeclaredTargetID,&result.Claim.TLSPinID,
   &result.Claim.SourceID,&result.Claim.MeasurementType,
   &result.Claim.Outcome,&result.Claim.ObservedAt,&result.Claim.AcceptedAt,
   &result.Claim.SignatureSHA256,&result.Claim.ProofClass,
   &result.SigningPayload,&result.Signature,&result.PublicKey,
   &chPayload,&nonce,&issued,&expires,&pinHash,&keyPlan,&targetIP,&targetPort)
 if errors.Is(err,sql.ErrNoRows){return InstrumentMeasurementVerificationBundle{},ErrInvalidTransition}
 if err!=nil{return InstrumentMeasurementVerificationBundle{},err}
 var msg instrumentMeasurementEnvelope
 if err=json.Unmarshal(result.SigningPayload,&msg);err!=nil{
  return InstrumentMeasurementVerificationBundle{},ErrInvalidTransition
 }
 var base instrumentChallengeSigningEnvelope
 if err=json.Unmarshal(chPayload,&base);err!=nil{
  return InstrumentMeasurementVerificationBundle{},ErrInvalidTransition
 }
 if msg.Domain!="OnePane/instrument-measurement/ed25519/v1"||
  !bytes.Equal(msg.ChallengePayload,chPayload)||
  msg.ProjectID!=projectID||msg.GoalID!=goalID||
  msg.InstrumentKeyID!=result.Claim.InstrumentKeyID||
  msg.ChallengeID!=result.Claim.ChallengeID||
  msg.DeclaredTargetID!=result.Claim.DeclaredTargetID||
  msg.TLSPinID!=result.Claim.TLSPinID||
  msg.SourceID!=result.Claim.SourceID||
  msg.MeasurementType!=result.Claim.MeasurementType||
  msg.Outcome!=result.Claim.Outcome||
  msg.ObservedAt!=result.Claim.ObservedAt||
  (base.Domain!="OnePane/instrument-challenge/ed25519/v1"&&
   base.Domain!="OnePane/instrument-challenge/ed25519/v2")||
  base.ProjectID!=projectID||base.GoalID!=goalID||
  base.InstrumentKeyID!=result.Claim.InstrumentKeyID||
  base.ChallengeID!=result.Claim.ChallengeID||
  base.DeclaredTargetID!=result.Claim.DeclaredTargetID||
  base.TLSPinID!=result.Claim.TLSPinID||
  base.PinnedCertificateSHA256!=pinHash||base.PlanHash!=keyPlan||
  !bytes.Equal(base.Nonce,nonce)||base.IssuedAt!=issued||
  base.ExpiresAt!=expires||
  msg.ObservedAt<issued||msg.ObservedAt>=expires||
  result.Claim.AcceptedAt>=expires{
  return InstrumentMeasurementVerificationBundle{},ErrInvalidTransition
 }
 if base.Domain=="OnePane/instrument-challenge/ed25519/v2"&&
  base.TargetEndpointSHA256!=goalEndpointDigest(targetIP,targetPort){
  return InstrumentMeasurementVerificationBundle{},ErrInvalidTransition
 }
 sum:=sha256.Sum256(result.Signature)
 if len(result.Signature)!=ed25519.SignatureSize||
  len(result.PublicKey)!=ed25519.PublicKeySize||
  "sha256:"+hex.EncodeToString(sum[:])!=result.Claim.SignatureSHA256||
  !ed25519.Verify(ed25519.PublicKey(result.PublicKey),result.SigningPayload,result.Signature){
  return InstrumentMeasurementVerificationBundle{},ErrInvalidTransition
 }
 result.DetachedSignatureVerified=true
 return result,nil
}
