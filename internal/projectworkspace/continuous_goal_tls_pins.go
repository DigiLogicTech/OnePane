package projectworkspace

import(
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "regexp"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// GoalTLSPin records explicit human out-of-band certificate fingerprint
// consent, not automatic trust in a device, its software or network health.
type GoalTLSPin struct{
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 DeclaredTargetID string `json:"declared_target_id"`
 PermitID string `json:"permit_id"`
 PermitRevision int64 `json:"permit_revision"`
 PlanHash string `json:"plan_hash"`
 CertificateSHA256 string `json:"certificate_sha256"`
 Status string `json:"status"`
 EffectivePinState string `json:"effective_pin_state,omitempty"`
 Revision int64 `json:"revision"`
 ApprovedBy string `json:"approved_by"`
 ApprovedAt int64 `json:"approved_at"`
 ExpiresAt int64 `json:"expires_at"`
 RevokedAt int64 `json:"revoked_at"`
}
type ApproveGoalTLSPinCommand struct{
 ProjectID,GoalID,DeclaredTargetID,CertificateSHA256,ActorPrincipalID string
 ExpectedGoalRevision,ExpectedTargetRevision,ExpiresAt int64
}
type RevokeGoalTLSPinCommand struct{
 ProjectID,GoalID,PinID,ActorPrincipalID string
 ExpectedGoalRevision,ExpectedPinRevision int64
}
var tlsCertificateFingerprint=regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
const goalTLSPinColumns=`id,project_id,goal_id,project_workspace_id,
 declared_target_id,permit_id,permit_revision,plan_hash,certificate_sha256,
 status,revision,approved_by,approved_at,expires_at,revoked_at`
func scanGoalTLSPin(row scanner)(GoalTLSPin,error){
 var p GoalTLSPin
 err:=row.Scan(&p.ID,&p.ProjectID,&p.GoalID,&p.ProjectWorkspaceID,
  &p.DeclaredTargetID,&p.PermitID,&p.PermitRevision,&p.PlanHash,
  &p.CertificateSHA256,&p.Status,&p.Revision,&p.ApprovedBy,
  &p.ApprovedAt,&p.ExpiresAt,&p.RevokedAt)
 return p,err
}

func humanGoalMemberTx(ctx context.Context,tx storage.Tx,tenant,principal string)error{
 var yes int
 err:=tx.QueryRowContext(ctx,`SELECT 1 FROM principals p
  JOIN workspace_memberships m ON m.principal_id=p.id
   AND m.workspace_id=? AND m.status='active'
  WHERE p.id=? AND p.status='active' AND p.principal_type='human'`,
  tenant,principal).Scan(&yes)
 if err!=nil{return ErrPrincipalIneligible}
 return nil
}

// Current target authority is rechecked in the same serialized transaction
// used by pin approval and by every verification claim. No private target
// address is ever supplied in a pin/check HTTP request.
type tlsPinnedTargetScope struct{
 Target GoalTCPDeclaredTarget
 Permit GoalTCPProbePermit
 GoalRevision int64
}
func(s *Service)activePinnedTargetTx(ctx context.Context,tx storage.Tx,
 projectID,goalID,targetID string,now int64)(tlsPinnedTargetScope,error){
 var scope tlsPinnedTargetScope
 project,err:=s.repo.ProjectTx(ctx,tx,projectID)
 if err!=nil{return scope,err}
 if project.Status!="active"||project.OperatingMode!=OperatingContinuousOperations{
  return scope,ErrInvalidTransition
 }
 var goalStatus,wsID,planJSON string
 err=tx.QueryRowContext(ctx,`SELECT g.status,g.project_workspace_id,
  g.monitoring_plan_json,g.revision
  FROM project_continuous_goals g
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=g.project_id AND w.status='active'
  JOIN workspaces tenant ON tenant.id=? AND tenant.status='active'
  WHERE g.id=? AND g.project_id=?`,
  project.WorkspaceID,goalID,projectID).
  Scan(&goalStatus,&wsID,&planJSON,&scope.GoalRevision)
 if err!=nil{return scope,err}
 if goalStatus!="configured"{return scope,ErrInvalidTransition}
 plan,hash,err:=goalReviewPlan(planJSON)
 if err!=nil{return scope,err}
 target,err:=scanDeclaredTCPTarget(tx.QueryRowContext(ctx,
  `SELECT `+declaredTargetColumns+` FROM project_goal_tcp_declared_target_bindings
    WHERE id=? AND goal_id=? AND project_id=?`,targetID,goalID,projectID))
 if errors.Is(err,sql.ErrNoRows){return scope,ErrInvalidTransition}
 if err!=nil{return scope,err}
 if target.Status!="approved"||target.ExpiresAt<=now||
  target.PlanHash!=hash||target.ProjectWorkspaceID!=wsID{
  return scope,ErrInvalidTransition
 }
 if err=humanGoalMemberTx(ctx,tx,project.WorkspaceID,target.ApprovedBy);err!=nil{
  return scope,err
 }
 permit,err:=scanGoalTCPProbePermit(tx.QueryRowContext(ctx,
  `SELECT `+tcpProbePermitCols+` FROM project_goal_tcp_probe_permits
   WHERE id=? AND project_id=? AND goal_id=?`,target.PermitID,projectID,goalID))
 if err!=nil{return scope,err}
 if permit.Status!="approved"||permit.ExpiresAt<=now||
  permit.Revision!=target.PermitRevision||permit.PlanHash!=hash||
  permit.ProjectWorkspaceID!=wsID||permit.SourceID!=target.SourceID||
  permit.TargetIPv4!=target.TargetIPv4||permit.TargetPort!=target.TargetPort||
  permit.TimeoutMS<100||permit.TimeoutMS>1500||
  permit.ProbeKind!="tcp_connect"||permit.EffectClass!="network_tcp_connect"{
  return scope,ErrInvalidTransition
 }
 if _,err=canonicalPrivateProbeIPv4(permit.TargetIPv4);err!=nil{
  return scope,ErrInvalidTransition
 }
 if err=humanGoalMemberTx(ctx,tx,project.WorkspaceID,permit.ApprovedBy);err!=nil{
  return scope,err
 }
 found:=false
 for _,source:=range plan.SourceIDs{
  if source==target.SourceID{found=true;break}
 }
 if !found{return scope,ErrInvalidTransition}
 scope.Target=target;scope.Permit=permit
 return scope,nil
}

func(s *Service)ApproveGoalTLSPin(ctx context.Context,c ApproveGoalTLSPinCommand)(GoalTLSPin,error){
 if c.ProjectID==""||c.GoalID==""||c.DeclaredTargetID==""||
  c.ActorPrincipalID==""||c.ExpectedGoalRevision<1||
  c.ExpectedTargetRevision<1||!tlsCertificateFingerprint.MatchString(c.CertificateSHA256){
  return GoalTLSPin{},ErrInvalidCommand
 }
 idv,err:=s.ids.New("gtlspin")
 if err!=nil{return GoalTLSPin{},err}
 evt,err:=s.ids.New("evt");if err!=nil{return GoalTLSPin{},err}
 var out GoalTLSPin
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if c.ExpiresAt<=now+5*60000||c.ExpiresAt>now+24*3600000{
   return ErrInvalidCommand
  }
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  scope,err:=s.activePinnedTargetTx(ctx,tx,c.ProjectID,c.GoalID,c.DeclaredTargetID,now)
  if err!=nil{return err}
  if scope.GoalRevision!=c.ExpectedGoalRevision||
   scope.Target.Revision!=c.ExpectedTargetRevision{return ErrRevisionConflict}
  if c.ExpiresAt>scope.Target.ExpiresAt||c.ExpiresAt>scope.Permit.ExpiresAt{
   return ErrInvalidCommand
  }
  var active int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_goal_tls_endpoint_pins
   WHERE declared_target_id=? AND status='approved'`,c.DeclaredTargetID).Scan(&active)
  if err!=nil{return err}
  if active>0{return ErrInvalidTransition}
  out=GoalTLSPin{
   ID:idv,ProjectID:c.ProjectID,GoalID:c.GoalID,
   ProjectWorkspaceID:scope.Target.ProjectWorkspaceID,
   DeclaredTargetID:scope.Target.ID,PermitID:scope.Permit.ID,
   PermitRevision:scope.Permit.Revision,PlanHash:scope.Permit.PlanHash,
   CertificateSHA256:c.CertificateSHA256,Status:"approved",EffectivePinState:"active",Revision:1,
   ApprovedBy:c.ActorPrincipalID,ApprovedAt:now,ExpiresAt:c.ExpiresAt,
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_tls_endpoint_pins(
   id,project_id,goal_id,project_workspace_id,declared_target_id,permit_id,
   permit_revision,plan_hash,certificate_sha256,status,revision,
   approved_by,approved_at,expires_at,revoked_at)
   VALUES(?,?,?,?,?,?,?,?,?,'approved',1,?,?,?,0)`,
   out.ID,out.ProjectID,out.GoalID,out.ProjectWorkspaceID,
   out.DeclaredTargetID,out.PermitID,out.PermitRevision,out.PlanHash,
   out.CertificateSHA256,out.ApprovedBy,out.ApprovedAt,out.ExpiresAt)
  if err!=nil{return err}
  result,err:=tx.ExecContext(ctx,`UPDATE project_continuous_goals
   SET revision=revision+1,updated_at=?
   WHERE id=? AND project_id=? AND revision=?`,
   now,c.GoalID,c.ProjectID,c.ExpectedGoalRevision)
  if err!=nil{return err}
  n,err:=result.RowsAffected();if err!=nil{return err}
  if n!=1{return ErrRevisionConflict}
  body,_:=json.Marshal(map[string]any{
   "goal_id":out.GoalID,"pin_id":out.ID,"target_id":out.DeclaredTargetID,
   "effect_class":"pinned_tls_handshake","identity_claim":"out_of_band_certificate_pin",
   "device_identity_verified":false,"monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&p.WorkspaceID,Type:"project.goal_tls_endpoint_pin_approved",
   AggregateType:"project_goal_tls_endpoint_pin",AggregateID:idv,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalTLSPin{},err}
 return out,nil
}

func(s *Service)RevokeGoalTLSPin(ctx context.Context,c RevokeGoalTLSPinCommand)(GoalTLSPin,error){
 if c.ProjectID==""||c.GoalID==""||c.PinID==""||
  c.ActorPrincipalID==""||c.ExpectedGoalRevision<1||
  c.ExpectedPinRevision<1{return GoalTLSPin{},ErrInvalidCommand}
 evt,err:=s.ids.New("evt")
 if err!=nil{return GoalTLSPin{},err}
 var out GoalTLSPin
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if p.Status!="active"{return ErrInvalidTransition}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  out,err=scanGoalTLSPin(tx.QueryRowContext(ctx,
   `SELECT `+goalTLSPinColumns+` FROM project_goal_tls_endpoint_pins
    WHERE id=? AND project_id=? AND goal_id=?`,c.PinID,c.ProjectID,c.GoalID))
  if err!=nil{return err}
  if out.Status!="approved"||out.Revision!=c.ExpectedPinRevision{
   return ErrRevisionConflict
  }
  var revision int64
  err=tx.QueryRowContext(ctx,`SELECT revision FROM project_continuous_goals
   WHERE id=? AND project_id=?`,c.GoalID,c.ProjectID).Scan(&revision)
  if err!=nil{return err}
  if revision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  result,err:=tx.ExecContext(ctx,`UPDATE project_goal_tls_endpoint_pins
   SET status='revoked',revision=revision+1,revoked_by=?,revoked_at=?
   WHERE id=? AND status='approved' AND revision=?`,
   c.ActorPrincipalID,now,c.PinID,c.ExpectedPinRevision)
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
  out.Status="revoked";out.EffectivePinState="revoked";out.Revision++;out.RevokedAt=now
  body,_:=json.Marshal(map[string]any{"goal_id":out.GoalID,
   "pin_id":out.ID,"revoked":true,"device_identity_verified":false})
  return s.events.Append(ctx,tx,event.Event{
   ID:evt,WorkspaceID:&p.WorkspaceID,Type:"project.goal_tls_endpoint_pin_revoked",
   AggregateType:"project_goal_tls_endpoint_pin",AggregateID:out.ID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:body,OccurredAt:now,
  })
 })
 if err!=nil{return GoalTLSPin{},err}
 return out,nil
}

func(s *Service)ListGoalTLSPins(ctx context.Context,projectID,goalID string)([]GoalTLSPin,error){
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var exists int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
    AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&exists)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+goalTLSPinColumns+`,
  CASE WHEN status='revoked' THEN 'revoked'
   WHEN expires_at<=? THEN 'expired'
   WHEN NOT EXISTS(SELECT 1 FROM principals reviewer
    JOIN projects p ON p.id=project_goal_tls_endpoint_pins.project_id
    JOIN workspace_memberships m ON m.workspace_id=p.workspace_id
     AND m.principal_id=reviewer.id AND m.status='active'
    WHERE reviewer.id=project_goal_tls_endpoint_pins.approved_by
     AND reviewer.principal_type='human' AND reviewer.status='active')
    THEN 'approver_inactive'
   ELSE 'active'
  END
  FROM project_goal_tls_endpoint_pins
  WHERE project_id=? AND goal_id=?
  ORDER BY approved_at DESC,id DESC LIMIT 100`,
  s.clock.UnixMilli(),projectID,goalID)
 if err!=nil{return nil,err}
 out:=[]GoalTLSPin{}
 for rows.Next(){
  var pin GoalTLSPin
  err=rows.Scan(&pin.ID,&pin.ProjectID,&pin.GoalID,&pin.ProjectWorkspaceID,
   &pin.DeclaredTargetID,&pin.PermitID,&pin.PermitRevision,&pin.PlanHash,
   &pin.CertificateSHA256,&pin.Status,&pin.Revision,&pin.ApprovedBy,
   &pin.ApprovedAt,&pin.ExpiresAt,&pin.RevokedAt,&pin.EffectivePinState)
  if err!=nil{rows.Close();return nil,err}
  out=append(out,pin)
 }
 if err=rows.Err();err!=nil{rows.Close();return nil,err}
 // Close before making the second bounded query: low-memory SQLite may
 // allow only one read connection, and leaving rows open risks deadlock.
 if err=rows.Close();err!=nil{return nil,err}
 targets,err:=s.ListGoalTCPTargets(ctx,projectID,goalID)
 if err!=nil{return nil,err}
 byID:=make(map[string]GoalTCPDeclaredTarget,len(targets))
 for _,target:=range targets{byID[target.ID]=target}
 for i:=range out{
  if out[i].EffectivePinState!="active"{continue}
  target,ok:=byID[out[i].DeclaredTargetID]
  if !ok||target.EffectiveIdentityState!="active"||
   target.PermitID!=out[i].PermitID||
   target.PermitRevision!=out[i].PermitRevision||
   target.ProjectWorkspaceID!=out[i].ProjectWorkspaceID||
   target.PlanHash!=out[i].PlanHash{
   out[i].EffectivePinState="target_inactive"
  }
 }
 return out,nil
}
