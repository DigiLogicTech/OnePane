package projectworkspace

import (
 "context"
 "fmt"

 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// Granting new off-host data transfer authority requires an authenticated
// human in the actual tenant Workspace. A model/Agent is free to propose
// a grant, but cannot approve its own expansion through Project/Workspace
// policy mutations or an existing service identity.
func(s *Service)requireHumanNodeApproval(ctx context.Context,tx storage.Tx,tenantID,actorID string)error{
 if err:=s.requireActor(ctx,tx,tenantID,actorID);err!=nil{return err}
 state,err:=s.repo.ActorStateTx(ctx,tx,tenantID,actorID)
 if err!=nil{return err}
 if state.PrincipalType!="human"{
  return fmt.Errorf("%w: trusted Node transfer grant requires human approval",ErrPrincipalIneligible)
 }
 return nil
}
