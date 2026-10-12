package nodefederation

import (
 "context"
 "database/sql"
 "errors"

 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// ReservePairedNodeMeasurementRequest executes before telemetry JSON decoding,
// and consumes quota even if the request later fails strict JSON, freshness
// or signature/grant admission checks. One peer shares a 60/min limit across
// all Goals and grants so a compromised paired sender cannot multiply its
// successful-sample budget by creating extra grant identifiers.
func(s *Service)ReservePairedNodeMeasurementRequest(ctx context.Context,
 peerID,fingerprint string)error{
 if s==nil||s.tx==nil||s.clock==nil||peerID==""||
  len(fingerprint)!=71{return ErrNodeMeasurementNotAdmitted}
 return s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if now<=0{return ErrNodeMeasurementNotAdmitted}
  var active int
  err:=tx.QueryRowContext(ctx,`SELECT 1 FROM harness_nodes n
   JOIN node_pairings p ON p.peer_node_id=n.id
   WHERE n.id=? AND n.local=0 AND n.trust_state='paired'
    AND p.status='paired' AND p.local_confirmed=1
    AND p.peer_confirmed=1 AND p.peer_tls_fingerprint=?`,
   peerID,fingerprint).Scan(&active)
  if errors.Is(err,sql.ErrNoRows){return ErrNodeMeasurementNotAdmitted}
  if err!=nil{return err}
  var started int64
  var count int
  err=tx.QueryRowContext(ctx,`SELECT window_started_at,attempts
   FROM node_measurement_request_budgets WHERE peer_node_id=?`,
   peerID).Scan(&started,&count)
  if errors.Is(err,sql.ErrNoRows){
   _,err=tx.ExecContext(ctx,`INSERT INTO node_measurement_request_budgets
    (peer_node_id,window_started_at,attempts,updated_at) VALUES(?,?,1,?)`,
    peerID,now,now)
   return err
  }
  if err!=nil{return err}
  if started>now{return ErrNodeMeasurementBudget}
  if now-started>=60*1000{
   _,err=tx.ExecContext(ctx,`UPDATE node_measurement_request_budgets
    SET window_started_at=?,attempts=1,updated_at=?
    WHERE peer_node_id=?`,now,now,peerID)
   return err
  }
  if count>=60{return ErrNodeMeasurementBudget}
  _,err=tx.ExecContext(ctx,`UPDATE node_measurement_request_budgets
   SET attempts=attempts+1,updated_at=?
   WHERE peer_node_id=? AND attempts<60`,now,peerID)
  return err
 })
}
