package nodefederation

import (
 "context"
 "errors"
 "time"
)

// DefaultInboundReceiptRetention is limited to the internal federated
// transport's idempotency response cache. Saved conversations, user-owned
// Workspace data, inference provenance and model files are not touched.
const DefaultInboundReceiptRetention=7*24*time.Hour

var ErrInvalidReceiptRetention=errors.New("invalid federated receipt retention duration")

// PruneCachedInferenceResponses logically redacts bounded batches of old
// completed inference response/usage JSON while preserving immutable
// (peer, request ID, model, request digest) tombstones. Deleting receipts
// would permit the same old request to execute again, so rows are retained.
// A repeat request for an expired successful receipt gets a known error,
// never another model execution or a fabricated empty success.
//
// This is a normal SQLite logical update, NOT forensic secure erasure of
// existing WAL, backup copies or the underlying storage device.
func(s *Service)PruneCachedInferenceResponses(ctx context.Context,retention time.Duration)(int,error){
 if s==nil||s.db==nil||s.clock==nil||retention<time.Hour||
  retention>365*24*time.Hour{return 0,ErrInvalidReceiptRetention}
 threshold:=s.clock.UnixMilli()-retention.Milliseconds()
 result,err:=s.db.ExecContext(ctx,`UPDATE node_federated_inference_receipts
  SET response_json=NULL,usage_json=NULL,
      error_code='remote_cached_response_expired'
  WHERE id IN(
   SELECT id FROM node_federated_inference_receipts
   WHERE status='succeeded' AND completed_at IS NOT NULL
    AND completed_at<=?
    AND (response_json IS NOT NULL OR usage_json IS NOT NULL)
   ORDER BY completed_at,id LIMIT 128
  )`,threshold)
 if err!=nil{return 0,err}
 rows,err:=result.RowsAffected()
 return int(rows),err
}
