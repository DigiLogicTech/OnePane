package projectworkspace

import(
 "bytes"
 "context"
 "crypto/ed25519"
 "crypto/rand"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "errors"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// A human-enrolled INSTRUMENT KEY is a separate key-possession identity,
// not attestation of a particular physical device. An association with a
// pinned TLS endpoint is an operator's declaration, not remote proof of
// attachment, location, firmware state or measurement accuracy.
type GoalInstrumentKey struct{
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 DeclaredTargetID string `json:"declared_target_id"`
 TLSPinID string `json:"tls_pin_id"`
 PlanHash string `json:"plan_hash"`
 // Derived only from the LIVE human-approved permit when validating
 // or issuing a challenge; never a caller-provided claim or database key.
 ApprovedTargetEndpointSHA256 string `json:"-"`
 PublicKey []byte `json:"public_key"`
 Status string `json:"status"`
 EffectiveInstrumentState string `json:"effective_instrument_state,omitempty"`
 Revision int64 `json:"revision"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
 RevokedAt int64 `json:"revoked_at"`
}
type ApproveGoalInstrumentKeyCommand struct{
 ProjectID,GoalID,TLSPinID,ActorPrincipalID string
 PublicKey []byte
 ExpectedGoalRevision,ExpectedPinRevision,ExpiresAt int64
}
type RevokeGoalInstrumentKeyCommand struct{
 ProjectID,GoalID,KeyID,ActorPrincipalID string
 ExpectedGoalRevision,ExpectedKeyRevision int64
}
type IssueGoalInstrumentChallengeCommand struct{
 ProjectID,GoalID,KeyID,ActorPrincipalID string
 ExpectedGoalRevision,ExpectedKeyRevision int64
}
type AcceptGoalInstrumentChallengeCommand struct{
 ProjectID,GoalID,KeyID,ChallengeID,ActorPrincipalID string
 ExpectedGoalRevision int64
 Signature []byte
}
type GoalInstrumentChallenge struct{
 ID string `json:"id"`
 KeyID string `json:"instrument_key_id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 Nonce []byte `json:"nonce"`
 IssuedAt int64 `json:"issued_at"`
 ExpiresAt int64 `json:"expires_at"`
 Status string `json:"status"`
 // The exact base64-encoded bytes an offline enrolled instrument signs.
 // NEVER let an external caller choose or reconstruct the challenge fields.
 SigningPayload []byte `json:"signing_payload"`
}
type GoalInstrumentProof struct{
 ID string `json:"id"`
 KeyID string `json:"instrument_key_id"`
 ChallengeID string `json:"challenge_id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 DeclaredTargetID string `json:"declared_target_id"`
 TLSPinID string `json:"tls_pin_id"`
 SignatureSHA256 string `json:"signature_sha256"`
 AcceptedAt int64 `json:"accepted_at"`
 ProofClass string `json:"proof_class"`
 DeviceIdentityVerified bool `json:"device_identity_verified"`
 RemediationAuthorized bool `json:"remediation_authorized"`
}
type instrumentChallengeSigningEnvelope struct{
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
const instrumentKeyColumns=`id,project_id,goal_id,project_workspace_id,
 declared_target_id,tls_pin_id,plan_hash,public_key,status,revision,
 approved_by,approved_at,expires_at,revoked_at`
func scanGoalInstrumentKey(row scanner)(GoalInstrumentKey,error){
 var k GoalInstrumentKey
 err:=row.Scan(&k.ID,&k.ProjectID,&k.GoalID,&k.ProjectWorkspaceID,
  &k.DeclaredTargetID,&k.TLSPinID,&k.PlanHash,&k.PublicKey,&k.Status,
  &k.Revision,&k.ApprovedBy,&k.ApprovedAt,&k.ExpiresAt,&k.RevokedAt)
 return k,err
}

func(s *Service)activeInstrumentKeyTx(ctx context.Context,tx storage.Tx,
 projectID,goalID,keyID string,now int64)(GoalInstrumentKey,GoalTLSPin,error){
 var empty GoalInstrumentKey
 var noPin GoalTLSPin
 k,err:=scanGoalInstrumentKey(tx.QueryRowContext(ctx,
  `SELECT `+instrumentKeyColumns+` FROM project_goal_instrument_keys
   WHERE id=? AND project_id=? AND goal_id=?`,keyID,projectID,goalID))
 if err!=nil{return empty,noPin,ErrInvalidTransition}
 if k.Status!="approved"||k.ExpiresAt<=now||
  len(k.PublicKey)!=ed25519.PublicKeySize{return empty,noPin,ErrInvalidTransition}
 project,err:=s.repo.ProjectTx(ctx,tx,projectID)
 if err!=nil{return empty,noPin,err}
 if err=humanGoalMemberTx(ctx,tx,project.WorkspaceID,k.ApprovedBy);err!=nil{
  return empty,noPin,err
 }
 pin,err:=scanGoalTLSPin(tx.QueryRowContext(ctx,
  `SELECT `+goalTLSPinColumns+` FROM project_goal_tls_endpoint_pins
   WHERE id=? AND project_id=? AND goal_id=?`,
  k.TLSPinID,projectID,goalID))
 if err!=nil{return empty,noPin,ErrInvalidTransition}
 if pin.Status!="approved"||pin.ExpiresAt<=now||
  !tlsCertificateFingerprint.MatchString(pin.CertificateSHA256)||
  pin.DeclaredTargetID!=k.DeclaredTargetID||
  pin.ProjectWorkspaceID!=k.ProjectWorkspaceID||
  pin.PlanHash!=k.PlanHash{return empty,noPin,ErrInvalidTransition}
 if err=humanGoalMemberTx(ctx,tx,project.WorkspaceID,pin.ApprovedBy);err!=nil{
  return empty,noPin,err
 }
 scope,err:=s.activePinnedTargetTx(ctx,tx,projectID,goalID,k.DeclaredTargetID,now)
 if err!=nil{return empty,noPin,err}
 if scope.Target.ProjectWorkspaceID!=k.ProjectWorkspaceID||
  scope.Target.PlanHash!=k.PlanHash||
  scope.Permit.ID!=pin.PermitID||
  scope.Permit.Revision!=pin.PermitRevision||
  scope.Permit.PlanHash!=pin.PlanHash{return empty,noPin,ErrInvalidTransition}
 k.ApprovedTargetEndpointSHA256=goalEndpointDigest(scope.Permit.TargetIPv4,
  scope.Permit.TargetPort)
 return k,pin,nil
}

// goalEndpointDigest binds the exact HUMAN-authorised private IPv4/port
// into the server-issued immutable challenge. Only the digest is included
// in the instrument challenge payload; raw topology stays in an access-
// controlled human-only plan and the original TCP permit.
func goalEndpointDigest(ip string,port int)string{
 raw:=sha256.Sum256([]byte(fmt.Sprintf(
  "OnePane/target-endpoint/v1|%s:%d",ip,port)))
 return "sha256:"+hex.EncodeToString(raw[:])
}

func instrumentSigningBytes(k GoalInstrumentKey,pin GoalTLSPin,
 c GoalInstrumentChallenge)([]byte,error){
 return json.Marshal(instrumentChallengeSigningEnvelope{
  Domain:"OnePane/instrument-challenge/ed25519/v2",
  ProjectID:k.ProjectID,GoalID:k.GoalID,InstrumentKeyID:k.ID,
  ChallengeID:c.ID,DeclaredTargetID:k.DeclaredTargetID,
  TLSPinID:k.TLSPinID,PinnedCertificateSHA256:pin.CertificateSHA256,
  PlanHash:k.PlanHash,TargetEndpointSHA256:k.ApprovedTargetEndpointSHA256,
  Nonce:c.Nonce,IssuedAt:c.IssuedAt,ExpiresAt:c.ExpiresAt,
 })
}

func(s *Service)ApproveGoalInstrumentKey(ctx context.Context,
 c ApproveGoalInstrumentKeyCommand)(GoalInstrumentKey,error){
 if c.ProjectID==""||c.GoalID==""||c.TLSPinID==""||c.ActorPrincipalID==""||
  len(c.PublicKey)!=ed25519.PublicKeySize||c.ExpectedGoalRevision<1||
  c.ExpectedPinRevision<1{return GoalInstrumentKey{},ErrInvalidCommand}
 idv,err:=s.ids.New("ginstrument")
 if err!=nil{return GoalInstrumentKey{},err}
 evt,err:=s.ids.New("evt")
 if err!=nil{return GoalInstrumentKey{},err}
 var result GoalInstrumentKey
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if c.ExpiresAt<=now+5*60000||c.ExpiresAt>now+24*3600000{
   return ErrInvalidCommand
  }
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  pin,err:=scanGoalTLSPin(tx.QueryRowContext(ctx,
   `SELECT `+goalTLSPinColumns+` FROM project_goal_tls_endpoint_pins
   WHERE id=? AND project_id=? AND goal_id=?`,c.TLSPinID,c.ProjectID,c.GoalID))
  if err!=nil{return ErrInvalidTransition}
  if pin.Status!="approved"||pin.ExpiresAt<=now||
   pin.Revision!=c.ExpectedPinRevision||c.ExpiresAt>pin.ExpiresAt{
   return ErrInvalidTransition
  }
  if err=humanGoalMemberTx(ctx,tx,p.WorkspaceID,pin.ApprovedBy);err!=nil{return err}
  scope,err:=s.activePinnedTargetTx(ctx,tx,c.ProjectID,c.GoalID,pin.DeclaredTargetID,now)
  if err!=nil{return err}
  if c.ExpectedGoalRevision!=scope.GoalRevision{return ErrRevisionConflict}
  if c.ExpiresAt>scope.Target.ExpiresAt||pin.PlanHash!=scope.Target.PlanHash||
   pin.ProjectWorkspaceID!=scope.Target.ProjectWorkspaceID||
   pin.PermitID!=scope.Permit.ID||
   pin.PermitRevision!=scope.Permit.Revision{return ErrInvalidTransition}
  var used int
  if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_instrument_keys
   WHERE tls_pin_id=? AND status='approved'`,pin.ID).Scan(&used);err!=nil{return err}
  if used>0{return ErrInvalidTransition}
  result=GoalInstrumentKey{
   ID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,
   ProjectWorkspaceID:pin.ProjectWorkspaceID,
   DeclaredTargetID:pin.DeclaredTargetID,TLSPinID:pin.ID,
   PlanHash:pin.PlanHash,PublicKey:append([]byte(nil),c.PublicKey...),
   Status:"approved",EffectiveInstrumentState:"active",Revision:1,ApprovedBy:c.ActorPrincipalID,
   ApprovedAt:now,ExpiresAt:c.ExpiresAt,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_instrument_keys(
   id,project_id,goal_id,project_workspace_id,declared_target_id,tls_pin_id,
   plan_hash,public_key,status,revision,approved_by,approved_at,expires_at,revoked_at)
   VALUES(?,?,?,?,?,?,?,?,'approved',1,?,?,?,0)`,
   result.ID,result.ProjectID,result.GoalID,result.ProjectWorkspaceID,
   result.DeclaredTargetID,result.TLSPinID,result.PlanHash,result.PublicKey,
   result.ApprovedBy,result.ApprovedAt,result.ExpiresAt)
  if err!=nil{return err}
  updated,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=?
   WHERE id=? AND project_id=? AND revision=?`,
   now,c.GoalID,c.ProjectID,c.ExpectedGoalRevision)
  if err!=nil{return err}
  n,err:=updated.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"instrument_key_id":result.ID,
   "declared_target_id":pin.DeclaredTargetID,"tls_pin_id":pin.ID,
   "proof_class":"key_enrolled_unverified",
   "device_identity_verified":false,"monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&p.WorkspaceID,
   Type:"project.goal_instrument_key_approved",
   AggregateType:"project_goal_instrument_key",AggregateID:idv,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalInstrumentKey{},err}
 return result,nil
}

func(s *Service)RevokeGoalInstrumentKey(ctx context.Context,
 c RevokeGoalInstrumentKeyCommand)(GoalInstrumentKey,error){
 if c.ProjectID==""||c.GoalID==""||c.KeyID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedKeyRevision<1{
  return GoalInstrumentKey{},ErrInvalidCommand
 }
 evt,err:=s.ids.New("evt");if err!=nil{return GoalInstrumentKey{},err}
 var k GoalInstrumentKey
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  k,err=scanGoalInstrumentKey(tx.QueryRowContext(ctx,
   `SELECT `+instrumentKeyColumns+` FROM project_goal_instrument_keys
   WHERE id=? AND project_id=? AND goal_id=?`,c.KeyID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if k.Status!="approved"||k.Revision!=c.ExpectedKeyRevision{
   return ErrRevisionConflict
  }
  var revision int64
  if err=tx.QueryRowContext(ctx,`SELECT revision FROM project_continuous_goals
   WHERE id=? AND project_id=?`,c.GoalID,c.ProjectID).Scan(&revision);err!=nil{
   return err
  }
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  result,err:=tx.ExecContext(ctx,`UPDATE project_goal_instrument_keys
   SET status='revoked',revision=revision+1,revoked_by=?,revoked_at=?
   WHERE id=? AND revision=? AND status='approved'`,
   c.ActorPrincipalID,now,c.KeyID,c.ExpectedKeyRevision)
  if err!=nil{return err}
  n,err:=result.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  result,err=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=?
   WHERE id=? AND project_id=? AND revision=?`,
   now,c.GoalID,c.ProjectID,c.ExpectedGoalRevision)
  if err!=nil{return err}
  n,err=result.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  k.Status="revoked";k.EffectiveInstrumentState="revoked";k.Revision++;k.RevokedAt=now
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"instrument_key_id":c.KeyID,"revoked":true,
   "device_identity_verified":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&p.WorkspaceID,Type:"project.goal_instrument_key_revoked",
   AggregateType:"project_goal_instrument_key",AggregateID:c.KeyID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalInstrumentKey{},err}
 return k,nil
}

func(s *Service)IssueGoalInstrumentChallenge(ctx context.Context,
 c IssueGoalInstrumentChallengeCommand)(GoalInstrumentChallenge,error){
 if c.ProjectID==""||c.GoalID==""||c.KeyID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedKeyRevision<1{
  return GoalInstrumentChallenge{},ErrInvalidCommand
 }
 idv,err:=s.ids.New("gchallenge");if err!=nil{return GoalInstrumentChallenge{},err}
 evt,err:=s.ids.New("evt");if err!=nil{return GoalInstrumentChallenge{},err}
 nonce:=make([]byte,32)
 if _,err=rand.Read(nonce);err!=nil{return GoalInstrumentChallenge{},err}
 var challenge GoalInstrumentChallenge
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  key,pin,err:=s.activeInstrumentKeyTx(ctx,tx,c.ProjectID,c.GoalID,c.KeyID,now)
  if err!=nil{return err}
  if key.Revision!=c.ExpectedKeyRevision{return ErrRevisionConflict}
  var revision int64
  if err=tx.QueryRowContext(ctx,`SELECT revision FROM project_continuous_goals
   WHERE id=? AND project_id=?`,c.GoalID,c.ProjectID).Scan(&revision);err!=nil{
   return err
  }
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  var count int
  if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_instrument_challenges
   WHERE instrument_key_id=?`,key.ID).Scan(&count);err!=nil{return err}
  // An immutable replay ledger on old or small disks must have a fixed
  // lifetime ceiling; 4/5-minute throttling by itself is not enough.
  if count>=128{return ErrInvalidTransition}
  if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_instrument_challenges
   WHERE instrument_key_id=? AND issued_at>?`,
   key.ID,now-5*60000).Scan(&count);err!=nil{return err}
  if count>=4{return ErrInvalidTransition}
  expires:=now+2*60000
  if key.ExpiresAt<expires{expires=key.ExpiresAt}
  if pin.ExpiresAt<expires{expires=pin.ExpiresAt}
  if expires<=now{return ErrInvalidTransition}
  challenge=GoalInstrumentChallenge{
   ID:idv,KeyID:key.ID,ProjectID:c.ProjectID,GoalID:c.GoalID,
   Nonce:nonce,IssuedAt:now,ExpiresAt:expires,Status:"issued",
  }
  challenge.SigningPayload,err=instrumentSigningBytes(key,pin,challenge)
  if err!=nil{return err}
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_instrument_challenges
   (id,instrument_key_id,project_id,goal_id,nonce,signing_payload,
    issued_at,expires_at,status,accepted_at)
   VALUES(?,?,?,?,?,?,?,?,'issued',0)`,
   challenge.ID,key.ID,c.ProjectID,c.GoalID,challenge.Nonce,
   challenge.SigningPayload,now,expires)
  if err!=nil{return err}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"instrument_key_id":key.ID,"challenge_id":idv,
   "proof_class":"challenge_issued_not_authenticated",
   "device_identity_verified":false,"monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_instrument_challenge_issued",
   AggregateType:"project_goal_instrument_challenge",AggregateID:idv,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalInstrumentChallenge{},err}
 return challenge,nil
}

func(s *Service)AcceptGoalInstrumentChallenge(ctx context.Context,
 c AcceptGoalInstrumentChallengeCommand)(GoalInstrumentProof,error){
 if c.ProjectID==""||c.GoalID==""||c.KeyID==""||
  c.ChallengeID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||len(c.Signature)!=ed25519.SignatureSize{
  return GoalInstrumentProof{},ErrInvalidCommand
 }
 proofID,err:=s.ids.New("giproof");if err!=nil{return GoalInstrumentProof{},err}
 evt,err:=s.ids.New("evt");if err!=nil{return GoalInstrumentProof{},err}
 var proof GoalInstrumentProof
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  key,pin,err:=s.activeInstrumentKeyTx(ctx,tx,c.ProjectID,c.GoalID,c.KeyID,now)
  if err!=nil{return err}
  var revision int64
  if err=tx.QueryRowContext(ctx,`SELECT revision FROM project_continuous_goals
   WHERE id=? AND project_id=?`,c.GoalID,c.ProjectID).Scan(&revision);err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  var challenge GoalInstrumentChallenge
  challenge.ID=c.ChallengeID
  challenge.KeyID=c.KeyID
  challenge.ProjectID=c.ProjectID
  challenge.GoalID=c.GoalID
  err=tx.QueryRowContext(ctx,`SELECT nonce,signing_payload,issued_at,expires_at,status
   FROM project_goal_instrument_challenges
   WHERE id=? AND instrument_key_id=? AND project_id=? AND goal_id=?`,
   c.ChallengeID,c.KeyID,c.ProjectID,c.GoalID).
   Scan(&challenge.Nonce,&challenge.SigningPayload,
    &challenge.IssuedAt,&challenge.ExpiresAt,&challenge.Status)
  if errors.Is(err,sql.ErrNoRows){return ErrInvalidTransition}
  if err!=nil{return err}
  if challenge.Status!="issued"||challenge.ExpiresAt<=now||
   challenge.IssuedAt>now||challenge.IssuedAt<key.ApprovedAt{
   return ErrInvalidTransition
  }
  // Check that persisted bytes are still the canonical v1 envelope
  // required by this live two-minute challenge; a later code version may
  // change new messages but cannot change historical issuance bytes.
  expected,err:=instrumentSigningBytes(key,pin,challenge)
  if err!=nil{return err}
  if !bytes.Equal(challenge.SigningPayload,expected){
   return ErrInvalidTransition
  }
  if !ed25519.Verify(ed25519.PublicKey(key.PublicKey),
   challenge.SigningPayload,c.Signature){
   return ErrInvalidCommand
  }
  updated,err:=tx.ExecContext(ctx,`UPDATE project_goal_instrument_challenges
   SET status='accepted',accepted_at=?
   WHERE id=? AND status='issued'`,now,c.ChallengeID)
  if err!=nil{return err}
  n,err:=updated.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrInvalidTransition}
  digest:=sha256.Sum256(c.Signature)
  proof=GoalInstrumentProof{
   ID:proofID,KeyID:key.ID,ChallengeID:c.ChallengeID,
   ProjectID:c.ProjectID,GoalID:c.GoalID,DeclaredTargetID:key.DeclaredTargetID,
   TLSPinID:key.TLSPinID,SignatureSHA256:"sha256:"+hex.EncodeToString(digest[:]),
   AcceptedAt:now,ProofClass:"human_bound_instrument_key_possession",
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_instrument_proofs
   (id,instrument_key_id,challenge_id,project_id,goal_id,declared_target_id,
   tls_pin_id,signature_sha256,signature,accepted_at,proof_class)
   VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
   proof.ID,proof.KeyID,proof.ChallengeID,proof.ProjectID,proof.GoalID,
   proof.DeclaredTargetID,proof.TLSPinID,proof.SignatureSHA256,
   append([]byte(nil),c.Signature...),proof.AcceptedAt,proof.ProofClass)
  if err!=nil{return err}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"instrument_key_id":key.ID,
   "challenge_id":c.ChallengeID,"proof_id":proof.ID,
   "proof_class":proof.ProofClass,"device_identity_verified":false,
   "monitoring_active":false,"remediation_authorized":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_instrument_proof_accepted",
   AggregateType:"project_goal_instrument_proof",AggregateID:proof.ID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalInstrumentProof{},err}
 return proof,nil
}

func(s *Service)ListGoalInstrumentKeys(ctx context.Context,
 projectID,goalID string)([]GoalInstrumentKey,error){
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var exists int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces ws ON ws.id=g.project_workspace_id
   AND ws.project_id=p.id AND ws.status='active'
  WHERE g.project_id=? AND g.id=?`,projectID,goalID).Scan(&exists)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+instrumentKeyColumns+`,
  CASE WHEN status='revoked' THEN 'revoked'
   WHEN expires_at<=? THEN 'expired'
   WHEN NOT EXISTS(SELECT 1 FROM principals human
    JOIN projects project ON project.id=project_goal_instrument_keys.project_id
    JOIN workspace_memberships member ON member.workspace_id=project.workspace_id
     AND member.principal_id=human.id AND member.status='active'
    WHERE human.id=project_goal_instrument_keys.approved_by
     AND human.principal_type='human' AND human.status='active')
    THEN 'approver_inactive'
   ELSE 'active'
  END
  FROM project_goal_instrument_keys WHERE project_id=? AND goal_id=?
  ORDER BY approved_at DESC,id DESC LIMIT 100`,
  s.clock.UnixMilli(),projectID,goalID)
 if err!=nil{return nil,err}
 out:=[]GoalInstrumentKey{}
 for rows.Next(){
  var item GoalInstrumentKey
  err=rows.Scan(&item.ID,&item.ProjectID,&item.GoalID,
   &item.ProjectWorkspaceID,&item.DeclaredTargetID,&item.TLSPinID,
   &item.PlanHash,&item.PublicKey,&item.Status,&item.Revision,
   &item.ApprovedBy,&item.ApprovedAt,&item.ExpiresAt,
   &item.RevokedAt,&item.EffectiveInstrumentState)
  if err!=nil{rows.Close();return nil,err}
  out=append(out,item)
 }
 if err=rows.Err();err!=nil{rows.Close();return nil,err}
 // Close SQLite result before nested reads; single-connection low-RAM
 // deployments cannot safely hold two active result sets concurrently.
 if err=rows.Close();err!=nil{return nil,err}
 pins,err:=s.ListGoalTLSPins(ctx,projectID,goalID)
 if err!=nil{return nil,err}
 byID:=make(map[string]GoalTLSPin,len(pins))
 for _,p:=range pins{byID[p.ID]=p}
 for i:=range out{
  if out[i].EffectiveInstrumentState!="active"{continue}
  pin,ok:=byID[out[i].TLSPinID]
  if !ok||pin.EffectivePinState!="active"||
   pin.DeclaredTargetID!=out[i].DeclaredTargetID||
   pin.PlanHash!=out[i].PlanHash||
   pin.ProjectWorkspaceID!=out[i].ProjectWorkspaceID{
   out[i].EffectiveInstrumentState="pin_inactive"
  }
 }
 return out,nil
}

func(s *Service)ListGoalInstrumentProofs(ctx context.Context,
 projectID,goalID string)([]GoalInstrumentProof,error){
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var exists int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces ws ON ws.id=g.project_workspace_id
   AND ws.project_id=p.id AND ws.status='active'
  WHERE g.project_id=? AND g.id=?`,projectID,goalID).Scan(&exists)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT id,instrument_key_id,
  challenge_id,project_id,goal_id,declared_target_id,tls_pin_id,
  signature_sha256,accepted_at,proof_class
  FROM project_goal_instrument_proofs WHERE project_id=? AND goal_id=?
  ORDER BY accepted_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalInstrumentProof{}
 for rows.Next(){
  var p GoalInstrumentProof
  err=rows.Scan(&p.ID,&p.KeyID,&p.ChallengeID,&p.ProjectID,&p.GoalID,
   &p.DeclaredTargetID,&p.TLSPinID,&p.SignatureSHA256,
   &p.AcceptedAt,&p.ProofClass)
  if err!=nil{return nil,err}
  out=append(out,p)
 }
 return out,rows.Err()
}

 
// GoalInstrumentVerificationBundle contains the original public detached
// signature and the EXACT bytes the instrument signed. A human can verify
// it with any independent Ed25519 implementation, even after revocation.
// It is NOT an instrument device-attestation, signed observation of a
// physical asset, endpoint security verdict or automatic monitoring result.
type GoalInstrumentVerificationBundle struct{
 Proof GoalInstrumentProof `json:"proof"`
 PublicKey []byte `json:"public_key"`
 SigningPayload []byte `json:"signing_payload"`
 Signature []byte `json:"signature"`
 DetachedSignatureVerified bool `json:"detached_signature_verified"`
 DeviceIdentityVerified bool `json:"device_identity_verified"`
 RemediationAuthorized bool `json:"remediation_authorized"`
}

func(s *Service)GetGoalInstrumentVerificationBundle(ctx context.Context,
 projectID,goalID,proofID string)(GoalInstrumentVerificationBundle,error){
 if projectID==""||goalID==""||proofID==""{
  return GoalInstrumentVerificationBundle{},ErrInvalidCommand
 }
 var out GoalInstrumentVerificationBundle
 var certHash,planHash string
 var nonce []byte
 var issuedAt,expiresAt int64
 err:=s.db.QueryRowContext(ctx,`SELECT
  p.id,p.instrument_key_id,p.challenge_id,p.project_id,p.goal_id,
  p.declared_target_id,p.tls_pin_id,p.signature_sha256,
  p.accepted_at,p.proof_class,p.signature,
  k.public_key,k.plan_hash,pin.certificate_sha256,
  challenge.nonce,challenge.signing_payload,
  challenge.issued_at,challenge.expires_at
  FROM project_goal_instrument_proofs p
  JOIN project_goal_instrument_keys k ON k.id=p.instrument_key_id
   AND k.project_id=p.project_id AND k.goal_id=p.goal_id
   AND k.declared_target_id=p.declared_target_id AND k.tls_pin_id=p.tls_pin_id
  JOIN project_goal_tls_endpoint_pins pin ON pin.id=k.tls_pin_id
   AND pin.project_id=p.project_id AND pin.goal_id=p.goal_id
  JOIN project_goal_instrument_challenges challenge
   ON challenge.id=p.challenge_id
   AND challenge.instrument_key_id=p.instrument_key_id
   AND challenge.project_id=p.project_id AND challenge.goal_id=p.goal_id
   AND challenge.status='accepted'
  JOIN project_continuous_goals goal ON goal.id=p.goal_id
   AND goal.project_id=p.project_id
  JOIN projects project ON project.id=goal.project_id AND project.status='active'
  JOIN workspaces tenant ON tenant.id=project.workspace_id AND tenant.status='active'
  JOIN project_workspaces workspace ON workspace.id=goal.project_workspace_id
   AND workspace.project_id=project.id AND workspace.status='active'
  WHERE p.id=? AND p.project_id=? AND p.goal_id=?`,
  proofID,projectID,goalID).
  Scan(&out.Proof.ID,&out.Proof.KeyID,&out.Proof.ChallengeID,
   &out.Proof.ProjectID,&out.Proof.GoalID,&out.Proof.DeclaredTargetID,
   &out.Proof.TLSPinID,&out.Proof.SignatureSHA256,&out.Proof.AcceptedAt,
   &out.Proof.ProofClass,&out.Signature,&out.PublicKey,&planHash,
   &certHash,&nonce,&out.SigningPayload,&issuedAt,&expiresAt)
 if errors.Is(err,sql.ErrNoRows){return GoalInstrumentVerificationBundle{},ErrInvalidTransition}
 if err!=nil{return GoalInstrumentVerificationBundle{},err}
 // Validate all historically signed context fields against their
 // immutable database entries, WITHOUT reserialising and losing the
 // original bytes that the instrument actually signed in v1.
 var envelope instrumentChallengeSigningEnvelope
 if err=json.Unmarshal(out.SigningPayload,&envelope);err!=nil{
  return GoalInstrumentVerificationBundle{},ErrInvalidTransition
 }
 if (envelope.Domain!="OnePane/instrument-challenge/ed25519/v1"&&
  envelope.Domain!="OnePane/instrument-challenge/ed25519/v2")||
  envelope.ProjectID!=projectID||envelope.GoalID!=goalID||
  envelope.InstrumentKeyID!=out.Proof.KeyID||
  envelope.ChallengeID!=out.Proof.ChallengeID||
  envelope.DeclaredTargetID!=out.Proof.DeclaredTargetID||
  envelope.TLSPinID!=out.Proof.TLSPinID||
  envelope.PinnedCertificateSHA256!=certHash||
  envelope.PlanHash!=planHash||
  !bytes.Equal(envelope.Nonce,nonce)||
  envelope.IssuedAt!=issuedAt||envelope.ExpiresAt!=expiresAt{
  return GoalInstrumentVerificationBundle{},ErrInvalidTransition
 }
 if envelope.Domain=="OnePane/instrument-challenge/ed25519/v2"{
  // A v2 proof also seals the original human-approved private endpoint
  // digest; compare against the immutable target scope rather than a
  // currently mutable Project setting or client-controlled claim.
  var ip string
  var port int
  err=s.db.QueryRowContext(ctx,`SELECT target_ipv4,target_port
   FROM project_goal_tcp_declared_target_bindings
   WHERE id=? AND project_id=? AND goal_id=?`,
   out.Proof.DeclaredTargetID,projectID,goalID).Scan(&ip,&port)
  if err!=nil||envelope.TargetEndpointSHA256!=goalEndpointDigest(ip,port){
   return GoalInstrumentVerificationBundle{},ErrInvalidTransition
  }
 }
 actual:=sha256.Sum256(out.Signature)
 if len(out.Signature)!=ed25519.SignatureSize||
  len(out.PublicKey)!=ed25519.PublicKeySize||
  "sha256:"+hex.EncodeToString(actual[:])!=out.Proof.SignatureSHA256||
  !ed25519.Verify(ed25519.PublicKey(out.PublicKey),out.SigningPayload,out.Signature){
  // Corrupted or tampered audit rows never produce a verified output.
  return GoalInstrumentVerificationBundle{},ErrInvalidTransition
 }
 out.DetachedSignatureVerified=true
 return out,nil
}
