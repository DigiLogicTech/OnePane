package nodefederation

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
)

// ErrNodeMeasurementNotAdmitted is deliberately non-echoing: neither remote
// peers nor their caller can infer Goal, Workspace or source grant contents.
var ErrNodeMeasurementNotAdmitted=errors.New("node measurement transport not admitted")
var ErrNodeMeasurementReplay=errors.New("node measurement sequence replayed")
var ErrNodeMeasurementBudget=errors.New("node measurement rate limit")

// PeerNodeMeasurement is an allowlisted self-reported connectivity state.
// It is authenticated to the paired TLS *sender Node*, NOT to a particular
// physical device/sensor, host, signal accuracy, or device health.
type PeerNodeMeasurement struct{
 Protocol string `json:"protocol"`
 GrantID string `json:"grant_id"`
 Sequence int64 `json:"sequence"`
 SignalType string `json:"signal_type"`
 Outcome string `json:"outcome"`
 ObservedAt int64 `json:"observed_at"`
}
type PeerNodeMeasurementReceipt struct{
 ID string `json:"id"`
 GrantID string `json:"grant_id"`
 ProjectID string `json:"project_id"`
 GoalID string `json:"goal_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 PeerNodeID string `json:"peer_node_id"`
 SourceID string `json:"source_id"`
 Sequence int64 `json:"sequence"`
 SignalType string `json:"signal_type"`
 Outcome string `json:"outcome"`
 ObservedAt int64 `json:"observed_at"`
 ReceivedAt int64 `json:"received_at"`
 TransportKind string `json:"transport_kind"`
}

func(s *Service)AcceptPairedNodeMeasurement(ctx context.Context,
 peerID,peerFingerprint string,c PeerNodeMeasurement)(PeerNodeMeasurementReceipt,error){
 if s==nil||s.db==nil||s.tx==nil||s.clock==nil||
  peerID==""||!strings.HasPrefix(peerFingerprint,"sha256:")||
  len(peerFingerprint)!=71||c.Protocol!=ProtocolVersion||
  strings.TrimSpace(c.GrantID)==""||c.Sequence<1||
  c.SignalType!="peer_connectivity"||
  (c.Outcome!="reachable"&&c.Outcome!="unreachable"&&c.Outcome!="unknown")||
  c.ObservedAt<1{return PeerNodeMeasurementReceipt{},ErrNodeMeasurementNotAdmitted}
 receiptID,err:=s.ids.New("nmeasure")
 if err!=nil{return PeerNodeMeasurementReceipt{},err}
 eventID,err:=s.ids.New("evt")
 if err!=nil{return PeerNodeMeasurementReceipt{},err}
 var receipt PeerNodeMeasurementReceipt
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  now:=s.clock.UnixMilli()
  if c.ObservedAt>now||now-c.ObservedAt>5*60*1000{
   return ErrNodeMeasurementNotAdmitted
  }
  var tenantID,grantStatus,planRaw,nodeTrust,pairStatus,storedFingerprint,approverStatus string
  var grantRevision,lastSeq,approvedAt,expiresAt int64
  var confirmedLocal,confirmedPeer int
  err:=tx.QueryRowContext(ctx,`SELECT
   p.workspace_id,g.project_id,g.goal_id,g.project_workspace_id,g.source_id,g.status,
   g.revision,g.last_sequence,g.approved_at,g.expires_at,
   goal.monitoring_plan_json,n.trust_state,pair.status,pair.peer_tls_fingerprint,
   pair.local_confirmed,pair.peer_confirmed,approver.status
   FROM project_goal_node_measurement_grants g
   JOIN projects p ON p.id=g.project_id AND p.status='active'
   JOIN workspaces tenant ON tenant.id=p.workspace_id AND tenant.status='active'
   JOIN project_continuous_goals goal ON goal.id=g.goal_id
    AND goal.project_id=p.id AND goal.status='configured'
   JOIN project_workspaces pw ON pw.id=g.project_workspace_id
    AND pw.project_id=p.id AND pw.status='active'
   JOIN harness_nodes n ON n.id=g.peer_node_id AND n.local=0
   JOIN node_pairings pair ON pair.peer_node_id=n.id
   JOIN principals approver ON approver.id=g.approved_by
     AND approver.principal_type='human'
   JOIN workspace_memberships mem ON mem.principal_id=approver.id
     AND mem.workspace_id=p.workspace_id AND mem.status='active'
   WHERE g.id=? AND g.peer_node_id=? AND g.peer_tls_fingerprint=?`,
   c.GrantID,peerID,peerFingerprint).
   Scan(&tenantID,&receipt.ProjectID,&receipt.GoalID,&receipt.ProjectWorkspaceID,
    &receipt.SourceID,&grantStatus,&grantRevision,&lastSeq,&approvedAt,&expiresAt,
    &planRaw,&nodeTrust,&pairStatus,&storedFingerprint,
    &confirmedLocal,&confirmedPeer,&approverStatus)
  if err!=nil{return ErrNodeMeasurementNotAdmitted}
  if grantStatus!="approved"||expiresAt<=now||approvedAt>c.ObservedAt||
   approverStatus!="active"||nodeTrust!="paired"||pairStatus!="paired"||
   confirmedLocal!=1||confirmedPeer!=1||storedFingerprint!=peerFingerprint{
   return ErrNodeMeasurementNotAdmitted
  }
  var plan struct{SourceIDs []string `json:"source_ids"`}
  if json.Unmarshal([]byte(planRaw),&plan)!=nil{return ErrNodeMeasurementNotAdmitted}
  allowed:=false
  for _,source:=range plan.SourceIDs{
   if source==receipt.SourceID{allowed=true;break}
  }
  if !allowed{return ErrNodeMeasurementNotAdmitted}
  if c.Sequence<=lastSeq{return ErrNodeMeasurementReplay}
  // Bounded indexed scan: one compromised Node cannot indefinitely fill
  // the local receipt ledger, even with valid mTLS credentials.
  var count int
  err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM (
   SELECT 1 FROM project_goal_node_measurement_receipts
   WHERE grant_id=? AND received_at>?
   ORDER BY received_at DESC LIMIT 30
  )`,c.GrantID,now-60000).Scan(&count)
  if err!=nil{return err}
  if count>=30{return ErrNodeMeasurementBudget}
  written,err:=tx.ExecContext(ctx,`UPDATE project_goal_node_measurement_grants
   SET last_sequence=?,revision=revision+1
   WHERE id=? AND revision=? AND status='approved' AND last_sequence<?
    AND expires_at>?`,c.Sequence,c.GrantID,grantRevision,c.Sequence,now)
  if err!=nil{return err}
  affected,err:=written.RowsAffected()
  if err!=nil{return err}
  if affected!=1{return ErrNodeMeasurementReplay}
  receipt=PeerNodeMeasurementReceipt{
   ID:receiptID,GrantID:c.GrantID,ProjectID:receipt.ProjectID,
   GoalID:receipt.GoalID,ProjectWorkspaceID:receipt.ProjectWorkspaceID,
   PeerNodeID:peerID,SourceID:receipt.SourceID,Sequence:c.Sequence,
   SignalType:c.SignalType,Outcome:c.Outcome,ObservedAt:c.ObservedAt,
   ReceivedAt:now,TransportKind:"pinned_peer_mtls",
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_goal_node_measurement_receipts(
   id,grant_id,project_id,goal_id,project_workspace_id,peer_node_id,
   source_id,sequence,signal_type,outcome,observed_at,received_at,transport_kind)
   VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
   receipt.ID,receipt.GrantID,receipt.ProjectID,receipt.GoalID,
   receipt.ProjectWorkspaceID,receipt.PeerNodeID,receipt.SourceID,
   receipt.Sequence,receipt.SignalType,receipt.Outcome,
   receipt.ObservedAt,receipt.ReceivedAt,receipt.TransportKind)
  if err!=nil{return err}
  payload,_:=json.Marshal(map[string]any{
   "goal_id":receipt.GoalID,"grant_id":receipt.GrantID,
   "peer_node_id":peerID,"source_id":receipt.SourceID,
   "sequence":receipt.Sequence,"transport":"pinned_peer_mtls",
   "signal_type":receipt.SignalType,"outcome":receipt.Outcome,
   "node_origin_authenticated":true,"device_health":"unverified",
   "monitoring_active":false,
  })
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&tenantID,Type:"project.goal_node_measurement_received",
   AggregateType:"project_goal_node_measurement_receipt",
   AggregateID:receipt.ID,Payload:payload,OccurredAt:now,
  })
 })
 if errors.Is(err,sql.ErrNoRows){err=ErrNodeMeasurementNotAdmitted}
 if err!=nil{return PeerNodeMeasurementReceipt{},err}
 return receipt,nil
}
