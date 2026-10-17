//go:build integration

package nodefederation

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/clock"
 "github.com/DigiLogicTech/OnePane/internal/inference"
)

type supersedeInboundReceiptExecutor struct{
 db *sql.DB
 peer,requestID string
 mode string
 calls int
}
func(e *supersedeInboundReceiptExecutor)DispatchFederatedLocal(
 ctx context.Context,dep,requestID string,prompt json.RawMessage)(inference.DispatchResult,error){
 e.calls++
 // Model work completes, but the original executing receipt has been
 // independently reconciled before it attempts a terminal CAS update.
 result,err:=e.db.ExecContext(context.Background(),`UPDATE node_federated_inference_receipts
   SET status='unknown',error_code='operator_reconciliation_pending'
   WHERE peer_node_id=? AND remote_request_id=? AND status='executing'`,
   e.peer,e.requestID)
 if err!=nil{return inference.DispatchResult{},err}
 rows,err:=result.RowsAffected()
 if err!=nil||rows!=1{return inference.DispatchResult{},ErrFederatedReceiptConflict}
 if e.mode=="failure"{return inference.DispatchResult{},errors.New("inference failed after receipt reconciliation")}
 if e.mode=="invalid"{return inference.DispatchResult{ResponseJSON:json.RawMessage(`not-json`)},nil}
 return inference.DispatchResult{ResponseJSON:json.RawMessage(`{"answer":"model finished"}`),
   UsageJSON:json.RawMessage(`{"tokens":1}`)},nil
}

func TestInboundInferenceTerminalCASCannotOverwriteReconciledReceipt(t *testing.T){
 ctx:=context.Background()
 db,now:=seedNodeDB(t,"local","local-fp")
 defer db.Close()
 for _,q:=range []string{
  `INSERT INTO harness_nodes(id,name,local,identity_fingerprint,trust_state,
    trust_zone,protocol_json,capabilities_json,last_seen_at,revision,created_at,updated_at)
    VALUES('peer','Peer',0,'peer','paired','PAIRED_MTLS','{}','{}',1,1,1,1)`,
  `INSERT INTO models(id,model_ref,modalities_json,static_metadata_json,trust_state,
    created_at,updated_at)
    VALUES('m','test/local','["text"]','{}','user_trusted',1,1)`,
  `INSERT INTO model_deployments(id,model_id,node_id,runtime_name,runtime_config_json,
    status,deployment_fingerprint,revision,discovered_at,updated_at)
    VALUES('d','m','local','local','{}','ready','fp',1,1,1)`,
 }{if _,err:=db.SQL().ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 _=now
 svc:=NewService(db.SQL(),db,clock.Real{},NodeView{ID:"local",Local:true},Identity{},"",30*time.Second)
 for _,mode:=range []string{"success","failure","invalid"}{
  t.Run(mode,func(t *testing.T){
   key:="cas-superseded-"+mode
   exec:=&supersedeInboundReceiptExecutor{db:db.SQL(),peer:"peer",requestID:key,mode:mode}
   req:=RemoteInferenceRequest{Protocol:ProtocolVersion,RequestID:key,DeploymentID:"d",
    RequestJSON:json.RawMessage(`{"prompt":"one run only"}`)}
   out,err:=svc.executeInbound(ctx,"peer",req,exec)
   if !errors.Is(err,ErrFederatedReceiptConflict)||exec.calls!=1||
    out.ErrorCode!=""||len(out.ResponseJSON)!=0{
    t.Fatalf("superseded receipt falsely acknowledged: mode=%s err=%v out=%+v calls=%d",
     mode,err,out,exec.calls)
   }
   var status,code string
   if err:=db.SQL().QueryRowContext(ctx,`SELECT status,error_code
     FROM node_federated_inference_receipts WHERE peer_node_id='peer'
      AND remote_request_id=?`,key).Scan(&status,&code);err!=nil||
      status!="unknown"||code!="operator_reconciliation_pending"{
    t.Fatalf("reconciliation overwritten by original caller: status=%s code=%s err=%v",status,code,err)
   }
   replay,err:=svc.executeInbound(ctx,"peer",req,exec)
   if err!=nil||exec.calls!=1||replay.OutcomeKnown||
     replay.ErrorCode!="duplicate_or_unknown_remote_request"{
    t.Fatalf("superseded receipt executed twice: out=%+v err=%v calls=%d",replay,err,exec.calls)
   }
  })
 }
}
