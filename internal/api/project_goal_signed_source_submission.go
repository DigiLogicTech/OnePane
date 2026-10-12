package api

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/observation"
 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

// Enrollment is a human-only Project WRITE operation. Proof submission is
// separately restricted to machine bearer credentials carrying the exact
// goal.source.submit capability. It DOES NOT accept project.write as an
// alternative. A current, manually enrolled Goal source and valid Ed25519
// signature are also required at the domain transaction boundary.
type goalSignedSourceCommandService interface {
 ListGoalObservationSigners(context.Context,string,string)([]projectworkspace.GoalObservationSigner,error)
 ApproveGoalObservationSigner(context.Context,projectworkspace.ApproveGoalObservationSignerCommand)(projectworkspace.GoalObservationSigner,error)
 RevokeGoalObservationSigner(context.Context,projectworkspace.RevokeGoalObservationSignerCommand)(projectworkspace.GoalObservationSigner,error)
 AcceptSignedGoalObservation(context.Context,projectworkspace.AcceptSignedGoalObservationCommand)(projectworkspace.SignedGoalObservationProof,error)
}

// Signed-source mutations are security sensitive. Unlike legacy general
// JSON endpoints they reject duplicate object fields (including escaped
// aliases), nested ambiguity, trailing roots and payloads over 8 KiB.
func decodeSignedSourceJSON(w http.ResponseWriter,r *http.Request,out any)bool{
 raw,err:=io.ReadAll(http.MaxBytesReader(w,r.Body,8*1024))
 if err!=nil||observation.ValidateUnambiguousJSON(raw)!=nil{
  writeError(w,http.StatusBadRequest,"invalid or ambiguous signed-source request")
  return false
 }
 dec:=json.NewDecoder(bytes.NewReader(raw))
 dec.DisallowUnknownFields()
 if err=dec.Decode(out);err!=nil{
  writeError(w,http.StatusBadRequest,"invalid signed-source request")
  return false
 }
 if err=dec.Decode(new(any));err!=io.EOF{
  writeError(w,http.StatusBadRequest,"invalid signed-source request")
  return false
 }
 return true
}

// Operator-only signing key history. Source-submit credentials never grant
// the ability to discover or administer enrolled signing identities.
func(s *Server)listProjectGoalSourceSigners(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if identity.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human operator required")
  return
 }
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(goalSignedSourceCommandService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"source signer listing unavailable");return}
 signers,err:=svc.ListGoalObservationSigners(r.Context(),p.ID,
  strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,signers,err,http.StatusOK)
}

func(s *Server)approveProjectGoalSourceSigner(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if identity.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human operator approval required")
  return
 }
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalSignedSourceCommandService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"source enrollment unavailable");return}
 var input struct{
  SourceID string `json:"source_id"`
  SourcePrincipalID string `json:"source_principal_id"`
  PublicKey []byte `json:"public_key"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpiresAt int64 `json:"expires_at"`
 }
 if !decodeSignedSourceJSON(w,r,&input){return}
 signer,err:=svc.ApproveGoalObservationSigner(r.Context(),
  projectworkspace.ApproveGoalObservationSignerCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),
   SourceID:input.SourceID,SourcePrincipalID:input.SourcePrincipalID,
   PublicKey:input.PublicKey,ExpectedGoalRevision:input.ExpectedGoalRevision,
   ExpiresAt:input.ExpiresAt,ActorPrincipalID:identity.PrincipalID,
  })
 respondDomain(w,signer,err,http.StatusCreated)
}

func(s *Server)revokeProjectGoalSourceSigner(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if identity.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human operator approval required")
  return
 }
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(goalSignedSourceCommandService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"source enrollment unavailable");return}
 var input struct{
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  ExpectedSignerRevision int64 `json:"expected_signer_revision"`
 }
 if !decodeSignedSourceJSON(w,r,&input){return}
 signer,err:=svc.RevokeGoalObservationSigner(r.Context(),
  projectworkspace.RevokeGoalObservationSignerCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),SignerID:r.PathValue("signerID"),
   ActorPrincipalID:identity.PrincipalID,
   ExpectedGoalRevision:input.ExpectedGoalRevision,
   ExpectedSignerRevision:input.ExpectedSignerRevision,
  })
 respondDomain(w,signer,err,http.StatusOK)
}

func(s *Server)submitProjectGoalSignedObservation(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 // A web login cookie, privileged human, or model-generated caller ID
 // cannot substitute for an authenticated machine API credential.
 if identity.AuthMethod!="bearer"||identity.CredentialID==""||
  (identity.PrincipalType!="service"&&identity.PrincipalType!="agent"){
  writeError(w,http.StatusForbidden,"machine bearer identity required")
  return
 }
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 p,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,p.WorkspaceID,"goal.source.submit"){return}
 svc,ok:=s.projects.(goalSignedSourceCommandService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"signed evidence unavailable");return}
 // The caller can supply identifiers only; the authenticated principal
 // always comes from the bearer credential, never from request JSON.
 var input struct{
  SignerID string `json:"signer_id"`
  ObservationID string `json:"observation_id"`
  Sequence int64 `json:"sequence"`
  ExpectedGoalRevision int64 `json:"expected_goal_revision"`
  Signature []byte `json:"signature"`
 }
 if !decodeSignedSourceJSON(w,r,&input){return}
 proof,err:=svc.AcceptSignedGoalObservation(r.Context(),
  projectworkspace.AcceptSignedGoalObservationCommand{
   ProjectID:p.ID,GoalID:r.PathValue("goalID"),
   SignerID:input.SignerID,ObservationID:input.ObservationID,
   Sequence:input.Sequence,ExpectedGoalRevision:input.ExpectedGoalRevision,
   Signature:input.Signature,ActorPrincipalID:identity.PrincipalID,
  })
 respondDomain(w,proof,err,http.StatusCreated)
}
