package projectworkspace

import(
 "context"
 "crypto/sha256"
 "crypto/subtle"
 "crypto/tls"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "net"
 "strconv"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// A matched *out-of-band pinned* server TLS cert proves possession of that
// cert/private key at the endpoint for THIS handshake. It does NOT establish
// a physical device's identity, software integrity or security posture.
type GoalPinnedTLSCheck struct{
 ID string `json:"id"`
 PinID string `json:"pin_id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ClaimedAt int64 `json:"claimed_at"`
 CompletedAt int64 `json:"completed_at"`
 CadenceWindow int64 `json:"cadence_window"`
 Status string `json:"status"`
 CertificatePossessionMatched bool `json:"certificate_possession_matched"`
 DeviceIdentityVerified bool `json:"device_identity_verified"`
 MonitoringActive bool `json:"monitoring_active"`
}
type RunGoalPinnedTLSCheckCommand struct{
 ProjectID,GoalID,PinID,ActorPrincipalID string
 ExpectedGoalRevision,ExpectedPinRevision int64
}
const pinnedTLSCheckColumns=`id,pin_id,project_id,goal_id,claimed_at,
 completed_at,cadence_window,status`
func scanPinnedTLSCheck(row scanner)(GoalPinnedTLSCheck,error){
 var c GoalPinnedTLSCheck
 err:=row.Scan(&c.ID,&c.PinID,&c.ProjectID,&c.GoalID,
  &c.ClaimedAt,&c.CompletedAt,&c.CadenceWindow,&c.Status)
 c.CertificatePossessionMatched=(c.Status=="matched")
 return c,err
}
var errPinMismatch=errors.New("pinned certificate does not match")

// tlsPinnedHandshake performs one outbound connection to the EXACT literal
// approved private IPv4/port. No HTTP, redirects, DNS, request text,
// application data or fallback to system roots. Setting InsecureSkipVerify is
// deliberate solely because VerifyConnection enforces an out-of-band SHA256
// pin; disabling that callback would be a security regression.
func tlsPinnedHandshake(parent context.Context,dialer tcpConnectDialer,
 ip string,port,timeoutMS int,expected string)string{
 if dialer==nil||timeoutMS<100||timeoutMS>1500||
  port<1||port>65535||!tlsCertificateFingerprint.MatchString(expected){
  return "tls_failed"
 }
 if _,err:=canonicalPrivateProbeIPv4(ip);err!=nil{return "tls_failed"}
 digest,err:=hex.DecodeString(expected[7:])
 if err!=nil||len(digest)!=sha256.Size{return "tls_failed"}
 ctx,cancel:=context.WithTimeout(parent,time.Duration(timeoutMS)*time.Millisecond)
 defer cancel()
 address:=net.JoinHostPort(ip,strconv.Itoa(port))
 raw,err:=dialer.DialContext(ctx,"tcp4",address)
 if err!=nil{
  if errors.Is(parent.Err(),context.Canceled){return "cancelled"}
  return "unavailable"
 }
 if raw==nil{return "unavailable"}
 defer raw.Close()
 connection:=tls.Client(raw,&tls.Config{
  MinVersion:tls.VersionTLS12,
  InsecureSkipVerify:true, // Mandatory exact pin verification below.
  VerifyConnection:func(state tls.ConnectionState)error{
   if len(state.PeerCertificates)==0{return errors.New("missing server certificate")}
   cert:=state.PeerCertificates[0]
   now:=time.Now()
   if now.Before(cert.NotBefore)||!now.Before(cert.NotAfter){
    return errors.New("out-of-date server certificate")
   }
   actual:=sha256.Sum256(cert.Raw)
   if subtle.ConstantTimeCompare(actual[:],digest)!=1{return errPinMismatch}
   return nil
  },
 })
 // SetDeadline covers the handshake even when a test dialer or legacy
 // implementation returns a socket with no context-aware read deadline.
 _=connection.SetDeadline(time.Now().Add(time.Duration(timeoutMS)*time.Millisecond))
 err=connection.HandshakeContext(ctx)
 if err==nil{return "matched"}
 if errors.Is(parent.Err(),context.Canceled){return "cancelled"}
 if errors.Is(err,errPinMismatch){return "mismatched"}
 return "tls_failed"
}

// The rolling interval is evaluated against the immutable claim instant,
// not bucket numbers. A new bucket is not a fresh permission to reconnect.
func tlsPinHasRecentCheckTx(ctx context.Context,tx storage.Tx,
 pinID string,now int64)(bool,error){
 var used int
 err:=tx.QueryRowContext(ctx,`SELECT COUNT(*)
  FROM project_goal_tls_endpoint_checks
  WHERE pin_id=? AND claimed_at>?`,pinID,now-5*60000).Scan(&used)
 return used>0,err
}

var manualTLSPinCheckSlots=make(chan struct{},2)

// runGoalPinnedTLSCheck has an internal-only dialer injection for hermetic TLS
// tests. No HTTP/API user can substitute the production net.Dialer.
func(s *Service)runGoalPinnedTLSCheck(ctx context.Context,
 c RunGoalPinnedTLSCheckCommand,dialer tcpConnectDialer)(GoalPinnedTLSCheck,error){
 if c.ProjectID==""||c.GoalID==""||c.PinID==""||c.ActorPrincipalID==""||
  c.ExpectedGoalRevision<1||c.ExpectedPinRevision<1{
  return GoalPinnedTLSCheck{},ErrInvalidCommand
 }
 select{
 case manualTLSPinCheckSlots<-struct{}{}:
  defer func(){<-manualTLSPinCheckSlots}()
 default:
  return GoalPinnedTLSCheck{},ErrInvalidTransition
 }
 checkID,err:=s.ids.New("gtlscheck")
 if err!=nil{return GoalPinnedTLSCheck{},err}
 evtID,err:=s.ids.New("evt")
 if err!=nil{return GoalPinnedTLSCheck{},err}
 var pin GoalTLSPin
 var scope tlsPinnedTargetScope
 var check GoalPinnedTLSCheck
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return err}
  if err=s.requireActor(ctx,tx,p.WorkspaceID,c.ActorPrincipalID);err!=nil{return err}
  if err=humanGoalReviewer(ctx,tx,c.ActorPrincipalID);err!=nil{return err}
  pin,err=scanGoalTLSPin(tx.QueryRowContext(ctx,
   `SELECT `+goalTLSPinColumns+` FROM project_goal_tls_endpoint_pins
   WHERE id=? AND project_id=? AND goal_id=?`,c.PinID,c.ProjectID,c.GoalID))
  if errors.Is(err,sql.ErrNoRows){return ErrInvalidTransition}
  if err!=nil{return err}
  if pin.Status!="approved"||pin.Revision!=c.ExpectedPinRevision||
   pin.ExpiresAt<=now||!tlsCertificateFingerprint.MatchString(pin.CertificateSHA256){
   return ErrInvalidTransition
  }
  if err=humanGoalMemberTx(ctx,tx,p.WorkspaceID,pin.ApprovedBy);err!=nil{return err}
  scope,err=s.activePinnedTargetTx(ctx,tx,c.ProjectID,c.GoalID,pin.DeclaredTargetID,now)
  if err!=nil{return err}
  if scope.GoalRevision!=c.ExpectedGoalRevision{return ErrRevisionConflict}
  if scope.Target.ID!=pin.DeclaredTargetID||
   scope.Target.ProjectWorkspaceID!=pin.ProjectWorkspaceID||
   scope.Permit.ID!=pin.PermitID||
   scope.Permit.Revision!=pin.PermitRevision||
   scope.Permit.PlanHash!=pin.PlanHash{return ErrInvalidTransition}
  window:=now/(5*60000)
  // Cadence-window uniqueness prevents concurrent claims in one bucket.
  // ALSO check a true rolling five-minute window: otherwise a claim
  // immediately before a bucket boundary and another immediately after
  // could dispatch two TCP handshakes within milliseconds.
  recent,err:=tlsPinHasRecentCheckTx(ctx,tx,pin.ID,now)
  if err!=nil{return err}
  if recent{return ErrInvalidTransition}
  check=GoalPinnedTLSCheck{ID:checkID,PinID:pin.ID,ProjectID:c.ProjectID,
   GoalID:c.GoalID,ClaimedAt:now,CadenceWindow:window,Status:"claimed"}
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_tls_endpoint_checks(
   id,pin_id,project_id,goal_id,claimed_at,cadence_window,status,completed_at)
   VALUES(?,?,?,?,?,?,'claimed',0)`,
   check.ID,check.PinID,check.ProjectID,check.GoalID,check.ClaimedAt,window)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{
   "check_id":checkID,"goal_id":c.GoalID,"pin_id":pin.ID,
   "status":"claimed","effect_class":"pinned_tls_handshake",
   "verified_device_health":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:evtID,WorkspaceID:&p.WorkspaceID,
   Type:"project.goal_tls_endpoint_check_claimed",
   AggregateType:"project_goal_tls_endpoint_check",AggregateID:checkID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return GoalPinnedTLSCheck{},err}
 // Recheck live grants BEFORE dispatch; a human revoke between claim and
 // actual socket use must not allow a new handshake.
 permitted:=false
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  var liveStatus string
  var pinRevision int64
  var expires int64
  err:=tx.QueryRowContext(ctx,`SELECT status,revision,expires_at
   FROM project_goal_tls_endpoint_pins WHERE id=?`,pin.ID).
   Scan(&liveStatus,&pinRevision,&expires)
  if err!=nil{return err}
  if liveStatus!="approved"||expires<=now||pinRevision!=pin.Revision{return nil}
  p,err:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
  if err!=nil{return nil}
  if humanGoalMemberTx(ctx,tx,p.WorkspaceID,c.ActorPrincipalID)!=nil||
   humanGoalMemberTx(ctx,tx,p.WorkspaceID,pin.ApprovedBy)!=nil{return nil}
  _,err=s.activePinnedTargetTx(ctx,tx,c.ProjectID,c.GoalID,pin.DeclaredTargetID,now)
  permitted=(err==nil)
  if errors.Is(err,ErrInvalidTransition)||errors.Is(err,ErrPrincipalIneligible){
   return nil
  }
  return err
 })
 if err!=nil{return GoalPinnedTLSCheck{},err}
 outcome:="cancelled"
 if permitted{
  outcome=tlsPinnedHandshake(ctx,dialer,
   scope.Permit.TargetIPv4,scope.Permit.TargetPort,
   scope.Permit.TimeoutMS,pin.CertificateSHA256)
 }
 finishEvt,err:=s.ids.New("evt");if err!=nil{return GoalPinnedTLSCheck{},err}
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if now<check.ClaimedAt{now=check.ClaimedAt}
  // Never persist a MATCHED result after a revocation or plan change;
  // the network operation cannot be retroactively unsent.
  if outcome=="matched"{
   var status string
   var revision,expires int64
   err:=tx.QueryRowContext(ctx,`SELECT status,revision,expires_at
     FROM project_goal_tls_endpoint_pins WHERE id=?`,pin.ID).
    Scan(&status,&revision,&expires)
   if err!=nil||status!="approved"||revision!=pin.Revision||expires<=now{
    outcome="cancelled"
   }else{
    p,projectErr:=s.repo.ProjectTx(ctx,tx,c.ProjectID)
    if projectErr!=nil{
     outcome="cancelled"
    }else if humanGoalMemberTx(ctx,tx,p.WorkspaceID,c.ActorPrincipalID)!=nil||
     humanGoalMemberTx(ctx,tx,p.WorkspaceID,pin.ApprovedBy)!=nil{
     outcome="cancelled"
    }else{
     _,scopeErr:=s.activePinnedTargetTx(ctx,tx,c.ProjectID,c.GoalID,pin.DeclaredTargetID,now)
     if scopeErr!=nil{outcome="cancelled"}
    }
   }
  }
  updated,err:=tx.ExecContext(ctx,`UPDATE project_goal_tls_endpoint_checks
   SET status=?,completed_at=? WHERE id=? AND status='claimed'`,
   outcome,now,check.ID)
  if err!=nil{return err}
  n,err:=updated.RowsAffected()
  if err!=nil{return err}
  if n!=1{return ErrInvalidTransition}
  check.Status=outcome;check.CompletedAt=now
  check.CertificatePossessionMatched=(outcome=="matched")
  var tenant string
  err=tx.QueryRowContext(ctx,`SELECT workspace_id FROM projects WHERE id=?`,
   c.ProjectID).Scan(&tenant)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{"goal_id":c.GoalID,
   "pin_id":pin.ID,"check_id":check.ID,"status":outcome,
   "certificate_possession_only":true,"verified_device_health":false})
  return s.events.Append(ctx,tx,event.Event{
   ID:finishEvt,WorkspaceID:&tenant,Type:"project.goal_tls_endpoint_check_finished",
   AggregateType:"project_goal_tls_endpoint_check",AggregateID:check.ID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  })
 })
 if err!=nil{return GoalPinnedTLSCheck{},err}
 return check,nil
}

func(s *Service)RunGoalPinnedTLSCheck(ctx context.Context,
 c RunGoalPinnedTLSCheckCommand)(GoalPinnedTLSCheck,error){
 return s.runGoalPinnedTLSCheck(ctx,c,&net.Dialer{})
}

func(s *Service)ListGoalPinnedTLSChecks(ctx context.Context,
 projectID,goalID string)([]GoalPinnedTLSCheck,error){
 if projectID==""||goalID==""{return nil,ErrInvalidCommand}
 var belongs int
 err:=s.db.QueryRowContext(ctx,`SELECT 1 FROM project_continuous_goals g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
  JOIN project_workspaces w ON w.id=g.project_workspace_id
   AND w.project_id=p.id AND w.status='active'
  WHERE g.id=? AND g.project_id=?`,goalID,projectID).Scan(&belongs)
 if err!=nil{return nil,err}
 rows,err:=s.db.QueryContext(ctx,`SELECT `+pinnedTLSCheckColumns+`
  FROM project_goal_tls_endpoint_checks WHERE project_id=? AND goal_id=?
  ORDER BY claimed_at DESC,id DESC LIMIT 100`,projectID,goalID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]GoalPinnedTLSCheck{}
 for rows.Next(){
  var x GoalPinnedTLSCheck
  if err=rows.Scan(&x.ID,&x.PinID,&x.ProjectID,&x.GoalID,
   &x.ClaimedAt,&x.CompletedAt,&x.CadenceWindow,&x.Status);err!=nil{
   return nil,err
  }
  x.CertificatePossessionMatched=(x.Status=="matched")
  out=append(out,x)
 }
 return out,rows.Err()
}

