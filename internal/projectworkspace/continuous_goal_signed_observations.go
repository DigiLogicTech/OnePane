package projectworkspace

import (
 "context"
 "crypto/ed25519"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// GoalObservationSigner is a HUMAN-approved public identity for one source
// in one Level 5 Goal. Possession of this key authenticates its payload,
// never device integrity, compliance, live health or Node trust.
type GoalObservationSigner struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 SourceID string `json:"source_id"`
 SourcePrincipalID string `json:"source_principal_id"`
 PlanHash string `json:"plan_hash"`
 PublicKey []byte `json:"public_key"`
 Status string `json:"status"`
 Revision int64 `json:"revision"`
 LastSequence int64 `json:"last_sequence"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
}
type ApproveGoalObservationSignerCommand struct {
 ProjectID,GoalID,SourceID,SourcePrincipalID,ActorPrincipalID string
 PublicKey []byte
 ExpectedGoalRevision,ExpiresAt int64
}
type RevokeGoalObservationSignerCommand struct {
 ProjectID,GoalID,SignerID,ActorPrincipalID string
 ExpectedGoalRevision,ExpectedSignerRevision int64
}

// SignedGoalObservationProof is an immutable cryptographic admission receipt,
// NOT a replacement for operator-attached receipts and not "healthy" evidence.
type SignedGoalObservationProof struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 SignerID string `json:"signer_id"`
 ObservationID string `json:"observation_id"`
 SourceID string `json:"source_id"`
 IntegrityHash string `json:"integrity_hash"`
 Sequence int64 `json:"sequence"`
 ObservedAt int64 `json:"observed_at"`
 AcceptedAt int64 `json:"accepted_at"`
}
type AcceptSignedGoalObservationCommand struct {
 ProjectID,GoalID,SignerID,ObservationID,ActorPrincipalID string
 Sequence,ExpectedGoalRevision int64
 Signature []byte
}

const goalObservationSignerColumns=`id,project_id,goal_id,project_workspace_id,
 source_id,source_principal_id,plan_hash,public_key,status,revision,
 last_sequence,approved_by,approved_at,expires_at`

func scanGoalObservationSigner(row scanner)(GoalObservationSigner,error){
 var g GoalObservationSigner
 err:=row.Scan(&g.ID,&g.ProjectID,&g.GoalID,&g.ProjectWorkspaceID,
  &g.SourceID,&g.SourcePrincipalID,&g.PlanHash,&g.PublicKey,&g.Status,
  &g.Revision,&g.LastSequence,&g.ApprovedBy,&g.ApprovedAt,&g.ExpiresAt)
 return g,err
}

func(s *Service)ApproveGoalObservationSigner(ctx context.Context,
 c ApproveGoalObservationSignerCommand)(GoalObservationSigner,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.GoalID=strings.TrimSpace(c.GoalID)
 c.SourceID=strings.TrimSpace(c.SourceID)
 c.SourcePrincipalID=strings.TrimSpace(c.SourcePrincipalID)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 if c.ProjectID==""||c.GoalID==""||c.ActorPrincipalID==""||
  c.SourcePrincipalID==""||!goalSourceID.MatchString(c.SourceID)||
  len(c.PublicKey)!=ed25519.PublicKeySize||c.ExpectedGoalRevision<1{
  return GoalObservationSigner{},ErrInvalidCommand
 }
 idv,err:=s.ids.New("gsign")
 if err!=nil{return GoalObservationSigner{},err}
 evtID,err:=s.ids.New("evt")
 if err!=nil{return GoalObservationSigner{},err}
 var saved GoalObservationSigner
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if c.ExpiresAt<=now+5*60*1000||c.ExpiresAt>now+30*24*60*60*1000{
   return ErrInvalidCommand
  }
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var wsID,status,planRaw string
  var revision int64
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,
   g.monitoring_plan_json,g.revision FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=g.project_id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).
   Scan(&wsID,&status,&planRaw,&revision)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status!="configured"{return ErrInvalidTransition}
  plan,hash,err:=goalReviewPlan(planRaw)
  if err!=nil{return err}
  allowed:=false
  for _,source:=range plan.SourceIDs{if source==c.SourceID{allowed=true;break}}
  if !allowed{return ErrInvalidCommand}
  // The source must be a live, explicitly enrolled service/Agent member,
  // never an arbitrary model-supplied principal or unauthenticated Node ID.
  var active int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals p
   JOIN workspace_memberships m ON m.principal_id=p.id
    AND m.workspace_id=? AND m.status='active'
   WHERE p.id=? AND p.status='active' AND p.principal_type IN ('service','agent')`,
   project.WorkspaceID,c.SourcePrincipalID).Scan(&active)
  if errors.Is(err,sql.ErrNoRows){return ErrPrincipalIneligible}
  if err!=nil{return err}
  saved=GoalObservationSigner{
   ID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,ProjectWorkspaceID:wsID,
   SourceID:c.SourceID,SourcePrincipalID:c.SourcePrincipalID,
   PlanHash:hash,PublicKey:append([]byte(nil),c.PublicKey...),
   Status:"approved",Revision:1,ApprovedBy:c.ActorPrincipalID,
   ApprovedAt:now,ExpiresAt:c.ExpiresAt,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_observation_signers
   (id,project_id,goal_id,project_workspace_id,source_id,
   source_principal_id,plan_hash,public_key,status,revision,last_sequence,
   approved_by,approved_at,expires_at)
   VALUES(?,?,?,?,?,?,?,?,'approved',1,0,?,?,?)`,
   saved.ID,saved.ProjectID,saved.GoalID,saved.ProjectWorkspaceID,
   saved.SourceID,saved.SourcePrincipalID,saved.PlanHash,saved.PublicKey,
   saved.ApprovedBy,saved.ApprovedAt,saved.ExpiresAt)
  if err!=nil{return err}
  result,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=? WHERE id=? AND project_id=?
    AND revision=? AND status='configured'`,now,c.GoalID,c.ProjectID,revision)
  if err!=nil{return err}
  n,err:=result.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"signer_id":idv,"source_id":c.SourceID,
   "source_principal_id":c.SourcePrincipalID,"expires_at":c.ExpiresAt,
   "source_authenticated":false,"monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&project.WorkspaceID,Type:"project.goal_source_signer_approved",
   AggregateType:"project_goal_observation_signer",AggregateID:idv,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalObservationSigner{},err}
 return saved,nil
}

func(s *Service)RevokeGoalObservationSigner(ctx context.Context,
 c RevokeGoalObservationSignerCommand)(GoalObservationSigner,error){
 if strings.TrimSpace(c.ProjectID)==""||strings.TrimSpace(c.GoalID)==""||
  strings.TrimSpace(c.SignerID)==""||strings.TrimSpace(c.ActorPrincipalID)==""||
  c.ExpectedGoalRevision<1||c.ExpectedSignerRevision<1{
  return GoalObservationSigner{},ErrInvalidCommand
 }
 evtID,err:=s.ids.New("evt")
 if err!=nil{return GoalObservationSigner{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  var goalStatus string
  var goalRevision int64
  err=tx.QueryRowContext(ctx,`SELECT g.status,g.revision FROM project_continuous_goals g
   JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=g.project_id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).
   Scan(&goalStatus,&goalRevision)
  if err!=nil{return err}
  if goalRevision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  signer,err:=scanGoalObservationSigner(tx.QueryRowContext(ctx,
   `SELECT `+goalObservationSignerColumns+` FROM project_goal_observation_signers
    WHERE id=? AND goal_id=? AND project_id=?`,c.SignerID,c.GoalID,c.ProjectID))
  if err!=nil{return err}
  if signer.Revision!=c.ExpectedSignerRevision{return ErrRevisionConflict}
  if signer.Status!="approved"{return ErrInvalidTransition}
  now:=s.clock.UnixMilli()
  result,err:=tx.ExecContext(ctx,`UPDATE project_goal_observation_signers
   SET status='revoked',revision=revision+1,revoked_by=?,revoked_at=?
   WHERE id=? AND revision=? AND status='approved'`,
   c.ActorPrincipalID,now,c.SignerID,c.ExpectedSignerRevision)
  if err!=nil{return err}
  n,err:=result.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  result,err=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=?
   WHERE id=? AND project_id=? AND revision=?`,
   now,c.GoalID,c.ProjectID,goalRevision)
  if err!=nil{return err}
  n,err=result.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"signer_id":c.SignerID,"source_id":signer.SourceID,
   "revoked":true,"monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&project.WorkspaceID,Type:"project.goal_source_signer_revoked",
   AggregateType:"project_goal_observation_signer",AggregateID:c.SignerID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalObservationSigner{},err}
 return scanGoalObservationSigner(s.db.QueryRowContext(ctx,
  `SELECT `+goalObservationSignerColumns+` FROM project_goal_observation_signers
   WHERE id=? AND goal_id=? AND project_id=?`,c.SignerID,c.GoalID,c.ProjectID))
}

// SignedGoalObservationMessage is the versioned, domain-separated payload
// which an enrolled source signs locally. Neither private keys nor source
// raw Observation values are sent to the Project Goal audit trail.
func SignedGoalObservationMessage(projectID,goalID,signerID,
 observationID,integrityHash string,sequence int64)([]byte,error){
 if projectID==""||goalID==""||signerID==""||observationID==""||
  !strings.HasPrefix(integrityHash,"sha256:")||len(integrityHash)!=71||
  sequence<1{return nil,ErrInvalidCommand}
 return json.Marshal(struct{
  Domain string `json:"domain"`
  ProjectID string `json:"project_id"`
  GoalID string `json:"goal_id"`
  SignerID string `json:"signer_id"`
  ObservationID string `json:"observation_id"`
  IntegrityHash string `json:"integrity_hash"`
  Sequence int64 `json:"sequence"`
 }{"OnePane/goal-observation/ed25519/v1",projectID,goalID,
  signerID,observationID,integrityHash,sequence})
}

// AcceptSignedGoalObservation requires a real key signature and an active
// human-approved goal source at the *serialized final admission boundary*.
// It never initiates sockets, monitoring, remediation or verified health.
func(s *Service)AcceptSignedGoalObservation(ctx context.Context,
 c AcceptSignedGoalObservationCommand)(SignedGoalObservationProof,error){
 if strings.TrimSpace(c.ProjectID)==""||strings.TrimSpace(c.GoalID)==""||
  strings.TrimSpace(c.SignerID)==""||strings.TrimSpace(c.ObservationID)==""||
  strings.TrimSpace(c.ActorPrincipalID)==""||c.Sequence<1||
  c.ExpectedGoalRevision<1||len(c.Signature)!=ed25519.SignatureSize{
  return SignedGoalObservationProof{},ErrInvalidCommand
 }
 // Look up only a scoped, authenticated source's Observation; never
 // return sensor values in errors or event payloads.
 var signerWorkspaceID string
 err:=s.db.QueryRowContext(ctx,`SELECT p.workspace_id
  FROM project_goal_observation_signers k
  JOIN projects p ON p.id=k.project_id AND p.status='active'
  JOIN project_continuous_goals g ON g.id=k.goal_id AND g.project_id=p.id
  WHERE k.id=? AND k.project_id=? AND k.goal_id=? AND
   k.source_principal_id=?`,c.SignerID,c.ProjectID,c.GoalID,
  c.ActorPrincipalID).Scan(&signerWorkspaceID)
 if err!=nil{return SignedGoalObservationProof{},ErrInvalidTransition}
 var owns int
 err=s.db.QueryRowContext(ctx,`SELECT 1 FROM observations
  WHERE id=? AND workspace_id=?`,c.ObservationID,signerWorkspaceID).Scan(&owns)
 if err!=nil{return SignedGoalObservationProof{},ErrInvalidTransition}
 obsSvc:=observation.NewService(s.db,s.tx,s.clock)
 if err=obsSvc.VerifyIntegrity(ctx,c.ObservationID);err!=nil{
  return SignedGoalObservationProof{},ErrInvalidCommand
 }
 obs,err:=obsSvc.Get(ctx,c.ObservationID)
 if err!=nil{return SignedGoalObservationProof{},err}
 idv,err:=s.ids.New("gproof")
 if err!=nil{return SignedGoalObservationProof{},err}
 evtID,err:=s.ids.New("evt")
 if err!=nil{return SignedGoalObservationProof{},err}
 var receipt SignedGoalObservationProof
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  project,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
   return ErrInvalidTransition
  }
  if err=s.requireActor(ctx,tx,project.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  var wsID,status,planRaw string
  var rev int64
  var cadence int64
  err=tx.QueryRowContext(ctx,`SELECT g.project_workspace_id,g.status,
   g.monitoring_plan_json,g.revision,g.cadence_minutes
   FROM project_continuous_goals g JOIN project_workspaces w
   ON w.id=g.project_workspace_id AND w.project_id=g.project_id AND w.status='active'
   WHERE g.id=? AND g.project_id=?`,c.GoalID,c.ProjectID).
   Scan(&wsID,&status,&planRaw,&rev,&cadence)
  if err!=nil{return err}
  if rev!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if status!="configured"{return ErrInvalidTransition}
  plan,hash,err:=goalReviewPlan(planRaw)
  if err!=nil{return err}
  signer,err:=scanGoalObservationSigner(tx.QueryRowContext(ctx,
   `SELECT `+goalObservationSignerColumns+` FROM project_goal_observation_signers
    WHERE id=? AND project_id=? AND goal_id=?`,c.SignerID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if signer.Status!="approved"||signer.ExpiresAt<=now||
   signer.ProjectWorkspaceID!=wsID||signer.PlanHash!=hash||
   signer.SourcePrincipalID!=c.ActorPrincipalID||
   c.Sequence<=signer.LastSequence{return ErrInvalidTransition}
  allowed:=false
  for _,source:=range plan.SourceIDs{if source==signer.SourceID{allowed=true;break}}
  if !allowed{return ErrInvalidTransition}
  // The source principal and its operator's approval must still be
  // active; an expired, demoted or removed principal cannot submit.
  for _,entry:=range []struct{id,kind string}{
   {signer.SourcePrincipalID,"source"},{signer.ApprovedBy,"human"},
  }{
   var active int
   err=tx.QueryRowContext(ctx,`SELECT 1 FROM principals p
    JOIN workspace_memberships m ON m.principal_id=p.id
      AND m.workspace_id=? AND m.status='active'
    WHERE p.id=? AND p.status='active'
      AND ((?='human' AND p.principal_type='human') OR
       (?='source' AND p.principal_type IN ('service','agent')))`,
    project.WorkspaceID,entry.id,entry.kind,entry.kind).Scan(&active)
   if errors.Is(err,sql.ErrNoRows){return ErrPrincipalIneligible}
   if err!=nil{return err}
  }
  if obs.WorkspaceID!=project.WorkspaceID||obs.ProbeToolID!=signer.SourceID||
   obs.SourcePrincipalID==nil||*obs.SourcePrincipalID!=signer.SourcePrincipalID||
   obs.Label.OriginNodeID!=""||obs.ObservedAt<signer.ApprovedAt||
   obs.CreatedAt<signer.ApprovedAt||obs.ObservedAt>now||
   now-obs.ObservedAt>reviewFreshnessMillis(cadence){
   return ErrInvalidTransition
  }
  // Untrusted remote Node origin remains excluded until authenticated
  // paired-Node transport and source attestation are implemented.
  var dbTenant,dbSource,dbHash,dbPrincipal string
  var dbObservedAt int64
  err=tx.QueryRowContext(ctx,`SELECT o.workspace_id,o.probe_tool_id,
   o.integrity_hash,o.source_principal_id,o.observed_at FROM observations o
   WHERE o.id=? AND o.origin_node_id IS NULL`,c.ObservationID).
   Scan(&dbTenant,&dbSource,&dbHash,&dbPrincipal,&dbObservedAt)
  if err!=nil{return ErrInvalidTransition}
  if dbTenant!=obs.WorkspaceID||dbSource!=obs.ProbeToolID||
   dbHash!=obs.IntegrityHash||dbPrincipal!=signer.SourcePrincipalID||
   dbObservedAt!=obs.ObservedAt{return ErrInvalidTransition}
  message,err:=SignedGoalObservationMessage(c.ProjectID,c.GoalID,
   signer.ID,obs.ID,obs.IntegrityHash,c.Sequence)
  if err!=nil{return err}
  if !ed25519.Verify(ed25519.PublicKey(signer.PublicKey),message,c.Signature){
   return fmt.Errorf("%w: invalid source signature",ErrInvalidCommand)
  }
  // Atomic monotonic sequence advancement is the durable anti-replay fence.
  result,err:=tx.ExecContext(ctx,`UPDATE project_goal_observation_signers
   SET last_sequence=?,revision=revision+1 WHERE id=? AND revision=?
    AND status='approved' AND last_sequence<?`,
   c.Sequence,signer.ID,signer.Revision,c.Sequence)
  if err!=nil{return err}
  n,err:=result.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  receipt=SignedGoalObservationProof{
   ID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,
   ProjectWorkspaceID:wsID,SignerID:signer.ID,
   ObservationID:obs.ID,SourceID:signer.SourceID,
   IntegrityHash:obs.IntegrityHash,Sequence:c.Sequence,
   ObservedAt:obs.ObservedAt,AcceptedAt:now,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_signed_observation_proofs(
   id,project_id,goal_id,project_workspace_id,signer_id,observation_id,
   source_id,integrity_hash,sequence,signature,observed_at,accepted_at)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
   receipt.ID,receipt.ProjectID,receipt.GoalID,receipt.ProjectWorkspaceID,
   receipt.SignerID,receipt.ObservationID,receipt.SourceID,
   receipt.IntegrityHash,receipt.Sequence,c.Signature,
   receipt.ObservedAt,receipt.AcceptedAt)
  if err!=nil{return err}
  body,_:=json.Marshal(map[string]any{
   "goal_id":c.GoalID,"signer_id":signer.ID,"observation_id":obs.ID,
   "source_id":signer.SourceID,"sequence":c.Sequence,
   "source_signature_valid":true,"monitoring_active":false,
   "device_health":"unverified","node_attested":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&project.WorkspaceID,
   Type:"project.goal_observation_source_signature_accepted",
   AggregateType:"project_goal_signed_observation_proof",AggregateID:receipt.ID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return SignedGoalObservationProof{},err}
 return receipt,nil
}


// ListGoalSignedObservationProofs returns only immutable source-signature
// receipt metadata for an active Project and canonical Workspace. It never
// returns Observation values, credentials, private keys or signatures.
func(s *Service)ListGoalSignedObservationProofs(ctx context.Context,
 projectID,goalID string)([]SignedGoalObservationProof,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""{
  return nil,ErrInvalidCommand
 }
 var exists int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&exists)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT id,project_id,goal_id,
  project_workspace_id,signer_id,observation_id,source_id,
  integrity_hash,sequence,observed_at,accepted_at
  FROM project_goal_signed_observation_proofs
  WHERE goal_id=? AND project_id=?
  ORDER BY accepted_at DESC,id DESC LIMIT 100`,goalID,projectID)
 if err!=nil{return nil,err}
 defer rows.Close()
 results:=[]SignedGoalObservationProof{}
 for rows.Next(){
  var item SignedGoalObservationProof
  if err=rows.Scan(&item.ID,&item.ProjectID,&item.GoalID,
   &item.ProjectWorkspaceID,&item.SignerID,&item.ObservationID,
   &item.SourceID,&item.IntegrityHash,&item.Sequence,
   &item.ObservedAt,&item.AcceptedAt);err!=nil{return nil,err}
  results=append(results,item)
 }
 return results,rows.Err()
}

// ListGoalObservationSigners returns only bounded PUBLIC signing-key metadata
// so human operators can inspect, rotate and revoke source identities.
// Key material here is a public key, never a private signing secret.
func(s *Service)ListGoalObservationSigners(ctx context.Context,
 projectID,goalID string)([]GoalObservationSigner,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(goalID)==""{
  return nil,ErrInvalidCommand
 }
 var exists int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&exists)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+goalObservationSignerColumns+`
  FROM project_goal_observation_signers
  WHERE goal_id=? AND project_id=?
  ORDER BY approved_at DESC,id DESC LIMIT 100`,goalID,projectID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalObservationSigner{}
 for rows.Next(){
  signer,err:=scanGoalObservationSigner(rows)
  if err!=nil{return nil,err}
  out=append(out,signer)
 }
 return out,rows.Err()
}
