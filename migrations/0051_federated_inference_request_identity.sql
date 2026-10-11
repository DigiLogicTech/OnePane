-- Bind an inbound federated inference request ID to one immutable model
-- and prompt/context payload. Existing pre-upgrade receipts have no
-- provable request identity and must not replay cached responses.
-- New requests store only a SHA-256 digest, not the original prompt.
ALTER TABLE node_federated_inference_receipts
 ADD COLUMN request_hash TEXT NOT NULL DEFAULT ''
 CHECK (request_hash = '' OR
  (length(request_hash) = 64 AND request_hash NOT GLOB '*[^0-9a-f]*'));
