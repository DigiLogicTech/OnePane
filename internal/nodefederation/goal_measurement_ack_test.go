package nodefederation

import (
 "errors"
 "testing"
)

func TestPairedNodeTelemetryAckMustMatchExactSubmittedEvidence(t *testing.T){
 sent:=PeerNodeMeasurement{
  Protocol:ProtocolVersion,GrantID:"approved-grant",Sequence:4,
  SignalType:"peer_connectivity",Outcome:"reachable",ObservedAt:123400,
 }
 expected:=PeerNodeMeasurementReceipt{
  ID:"receipt-unique",GrantID:sent.GrantID,ProjectID:"project",
  GoalID:"goal",ProjectWorkspaceID:"workspace",
  PeerNodeID:"origin-peer",SourceID:"peer-link",
  Sequence:4,SignalType:"peer_connectivity",Outcome:"reachable",
  ObservedAt:sent.ObservedAt,ReceivedAt:123401,
  TransportKind:"pinned_peer_mtls",
 }
 if err:=validatePeerMeasurementAcknowledgement(sent,expected,"origin-peer");err!=nil{
  t.Fatalf("matching pinned Node receipt rejected: %v",err)
 }
 for label,change:=range map[string]func(*PeerNodeMeasurementReceipt){
  "different_nonce_sequence":func(a *PeerNodeMeasurementReceipt){a.Sequence++},
  "different_grant":func(a *PeerNodeMeasurementReceipt){a.GrantID="wrong"},
  "different_sender_node":func(a *PeerNodeMeasurementReceipt){a.PeerNodeID="another"},
  "altered_outcome":func(a *PeerNodeMeasurementReceipt){a.Outcome="unreachable"},
  "altered_signal_type":func(a *PeerNodeMeasurementReceipt){a.SignalType="other"},
  "altered_observation_timestamp":func(a *PeerNodeMeasurementReceipt){a.ObservedAt++},
  "missing_receipt_identity":func(a *PeerNodeMeasurementReceipt){a.ID=""},
  "missing_goal_identity":func(a *PeerNodeMeasurementReceipt){a.GoalID=""},
  "missing_workspace":func(a *PeerNodeMeasurementReceipt){a.ProjectWorkspaceID=""},
  "missing_source":func(a *PeerNodeMeasurementReceipt){a.SourceID=""},
  "invalid_transport":func(a *PeerNodeMeasurementReceipt){a.TransportKind="unauthenticated"},
  "impossible_receipt_chronology":func(a *PeerNodeMeasurementReceipt){a.ReceivedAt=123399},
 }{
  t.Run(label,func(t *testing.T){
   changed:=expected;change(&changed)
   if err:=validatePeerMeasurementAcknowledgement(sent,changed,"origin-peer");
    !errors.Is(err,ErrNodeMeasurementNotAdmitted){
    t.Fatalf("contradictory Node acknowledgement accepted: %v",err)
   }
  })
 }
}
