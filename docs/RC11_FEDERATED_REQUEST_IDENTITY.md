# RC11 — Federated inference request identity and replay safety

Federated Node inference receipts previously keyed stored responses by
(peer, request ID) only. A peer could reuse that same request ID for a
different prompt, attached context or model and receive an older cached
response without the request content matching. Concurrent duplicates
could also race the receipt's SELECT/INSERT path.

Migration 0051 adds `request_hash` to existing receipts. For each new
request the receiver persists only a SHA-256 digest of the immutable
remote model and prompt/context/classification request fields, not
the raw prompt. The unique (peer, request ID) database constraint
atomically admits **one** execution. Identical retries may consume
a successful receipt; an in-flight/unknown duplicate gets an explicit
unknown outcome without new execution. A different model or prompt
using the same ID yields `remote_request_identity_conflict` with a
known non-dispatch outcome and no cached response contents.

Historical pre-upgrade receipts lack the request digest. They fail
closed on subsequent retries rather than releasing unverifiable
historic responses. No schema rebuild, filesystem move, Node hardware
access or model substitution is necessary.

Integration tests cover exact idempotent replay, changed prompt, changed
model, changed evidence context, legacy receipt safety, concurrent
duplicate requests and successful completion without double execution.
These are local SQLite/transport tests, not physical Node acceptance or
a distributed exactly-once proof. The actual model could still perform
an external side effect that is unknown after process failure;
existing unknown-outcome recovery applies.

The RC11 user code-review, vision realignment and second review gates
remain in force; this change does not authorise packaging or release.
