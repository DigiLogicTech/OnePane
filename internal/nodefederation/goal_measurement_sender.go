package nodefederation

import (
 "context"
 "database/sql"
 "errors"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// The caller must provide the exact human-approved remote Goal grant ID.
// There is NO background polling, network scanning, Task execution, or
// automatic discovery of grants. This is a manually invoked self-report.
// TLS authenticates the local Node to the remote peer, not measurement
// accuracy or trust in any external device.
type SendPeerNodeMeasurementCommand struct{
 RecipientNodeID string
 GrantID string
 Outcome string
}

func(s *Service)reserveOutboundMeasurementSequence(ctx context.Context,
 recipientNodeID,grantID string)(int64,error){
 if s==nil||s.db==nil||s.tx==nil||s.clock==nil||s.local.ID==""||
  recipientNodeID==""||recipientNodeID==s.local.ID||
  len(grantID)<1||len(grantID)>128||strings.TrimSpace(grantID)!=grantID{
  return 0,ErrNodeMeasurementNotAdmitted
 }
 var sequence int64
 err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var trust,pair string
  var localConfirmed,remoteConfirmed int
  err:=tx.QueryRowContext(ctx,`SELECT h.trust_state,
   p.status,p.local_confirmed,p.peer_confirmed
   FROM harness_nodes h JOIN node_pairings p ON p.peer_node_id=h.id
   JOIN harness_nodes local ON local.id=? AND local.local=1
   WHERE h.id=? AND h.local=0`,s.local.ID,recipientNodeID).
   Scan(&trust,&pair,&localConfirmed,&remoteConfirmed)
  if err!=nil{return ErrNodeMeasurementNotAdmitted}
  if trust!="paired"||pair!="paired"||
   localConfirmed!=1||remoteConfirmed!=1{
   return ErrNodeMeasurementNotAdmitted
  }
  // A low-resource Node cannot accumulate unbounded per-grant counters.
  var entries int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM
   node_outbound_measurement_sequences
   WHERE local_node_id=? AND peer_node_id=?`,
   s.local.ID,recipientNodeID).Scan(&entries)
  if err!=nil{return err}
  var already int
  err=tx.QueryRowContext(ctx,`SELECT 1 FROM node_outbound_measurement_sequences
   WHERE local_node_id=? AND peer_node_id=? AND grant_id=?`,
   s.local.ID,recipientNodeID,grantID).Scan(&already)
  if !errors.Is(err,sql.ErrNoRows)&&err!=nil{return err}
  if errors.Is(err,sql.ErrNoRows)&&entries>=32{
   return ErrNodeMeasurementBudget
  }
  now:=s.clock.UnixMilli()
  if now<=0{return ErrNodeMeasurementNotAdmitted}
  err=tx.QueryRowContext(ctx,`INSERT INTO node_outbound_measurement_sequences
   (local_node_id,peer_node_id,grant_id,sequence,updated_at)
   VALUES(?,?,?,1,?)
   ON CONFLICT(local_node_id,peer_node_id,grant_id)
   DO UPDATE SET sequence=node_outbound_measurement_sequences.sequence+1,
    updated_at=excluded.updated_at
   RETURNING sequence`,
   s.local.ID,recipientNodeID,grantID,now).Scan(&sequence)
  return err
 })
 if err!=nil{return 0,err}
 return sequence,nil
}

func(s *Service)SendPeerNodeMeasurement(ctx context.Context,
 cmd SendPeerNodeMeasurementCommand)(PeerNodeMeasurementReceipt,error){
 if (cmd.Outcome!="reachable"&&cmd.Outcome!="unreachable"&&
  cmd.Outcome!="unknown")||s==nil{
  return PeerNodeMeasurementReceipt{},ErrNodeMeasurementNotAdmitted
 }
 recipientID:=strings.TrimSpace(cmd.RecipientNodeID)
 if recipientID==""||recipientID!=cmd.RecipientNodeID||
  cmd.GrantID==""||len(cmd.GrantID)>128{
  return PeerNodeMeasurementReceipt{},ErrNodeMeasurementNotAdmitted
 }
 // Existing federation API enforces pinned destination TLS fingerprint.
 endpoint,fp,err:=s.pairedEndpoint(ctx,recipientID)
 if err!=nil{return PeerNodeMeasurementReceipt{},ErrNodeMeasurementNotAdmitted}
 seq,err:=s.reserveOutboundMeasurementSequence(ctx,recipientID,cmd.GrantID)
 if err!=nil{return PeerNodeMeasurementReceipt{},err}
 // Reservation is never rolled back after transport failures. Sender
 // retries with a NEW sequence; the receiver rejects duplicates.
 request:=PeerNodeMeasurement{
  Protocol:ProtocolVersion,GrantID:cmd.GrantID,Sequence:seq,
  SignalType:"peer_connectivity",Outcome:cmd.Outcome,
  ObservedAt:s.clock.UnixMilli(),
 }
 var response PeerNodeMeasurementReceipt
 err=postJSON(ctx,s.pinnedClient(fp,true),
  endpoint+"/federation/v1/measurements",request,&response)
 if err!=nil{return PeerNodeMeasurementReceipt{},err}
 // Never trust a response for a different grant or sequence.
 if err=validatePeerMeasurementAcknowledgement(request,response,s.local.ID);err!=nil{
  return PeerNodeMeasurementReceipt{},err
 }
 return response,nil
}

// validatePeerMeasurementAcknowledgement refuses a syntactically valid
// mTLS response whose claimed persisted evidence does not EXACTLY match the
// originally transmitted telemetry. A pinned peer identity alone cannot
// make a contradictory receipt a valid acknowledgement. The received
// metadata remains a Node self-report, NOT a verified physical observation.
func validatePeerMeasurementAcknowledgement(
 request PeerNodeMeasurement, ack PeerNodeMeasurementReceipt,
 senderNodeID string)error{
 if senderNodeID==""||ack.ID==""||
  ack.ProjectID==""||ack.GoalID==""||
  ack.ProjectWorkspaceID==""||ack.SourceID==""||
  ack.GrantID!=request.GrantID||ack.Sequence!=request.Sequence||
  ack.PeerNodeID!=senderNodeID||ack.SignalType!=request.SignalType||
  ack.Outcome!=request.Outcome||ack.ObservedAt!=request.ObservedAt||
  ack.ReceivedAt<ack.ObservedAt||
  ack.TransportKind!="pinned_peer_mtls"{
  return ErrNodeMeasurementNotAdmitted
 }
 return nil
}
