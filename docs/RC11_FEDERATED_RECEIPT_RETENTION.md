# RC11 — Bounded cached remote inference response retention

The Node federation receiver persists a response/usage cache to ensure
idempotent retries without another model execution. Those responses can
contain sensitive user content, so retaining their complete JSON blobs
indefinitely is unnecessary. Deleting entire receipt records would be
unsafe: after a Node restart the same request ID could be executed
again without its idempotency fence.

The local Node now performs a bounded, daily cache-retention sweep
using `DefaultInboundReceiptRetention` (seven days). Only successfully
completed `node_federated_inference_receipts.response_json` and
`usage_json` are logically cleared in 128-row SQLite batches, up to
16 batches in one daily pass; the old receipt ID, peer, model,
`request_hash` and success state remain intact as an immutable
tombstone. Duplicate retries of such an expired success receive
`remote_cached_response_expired` with a known outcome and **never
trigger the remote model again** or fabricate a success response.

Recent receipt responses remain available for normal idempotent
replay, and unknown/in-progress receipts remain unchanged. The sweep
does not affect user conversations, Workspace artifacts, historical
operation evidence, model files or backup data. This is SQL logical
redaction only, not forensic erasure from SQLite WAL, migration backups,
system snapshots or storage media. The user still controls those
backup lifecycle policies.

Real SQLite integration verifies the 128-row batch bound, complete
tombstone preservation, post-expiry replay refusal, recent-response
retention, repeated idempotent sweeps and invalid retention denial.

This is local transport cache hygiene, not a physical Node test, an
exactly-once distributed system guarantee or authorisation to release
the RC11 build. The comprehensive code-review, user vision review
and final re-review gates remain unchanged.
