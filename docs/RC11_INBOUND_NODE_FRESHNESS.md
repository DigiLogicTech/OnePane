# RC11 — Inbound federation freshness and pairing authority

The origin Node now refuses inbound model inference when the sending
Node is no longer eligible, even if the TLS client certificate still
matches a previously approved paired identity.

The shared `peerFromTLS` boundary requires both sides of the pairing
to be confirmed in the authoritative pairing row. It still allows a
temporarily unavailable but genuinely paired Node to authenticate its
**heartbeat**, enabling a strictly newer signed capability manifest
to recover its advertised model capacities.

The inference handler applies a stricter, read-only local admission
check before consuming the model request: peer must be remote,
paired/available, have a currently paired and both-side-confirmed
record, and have an unexpired capability manifest. A missing manifest,
expired heartbeat, unavailable Node or partially confirmed pairing
therefore denies inference before receipt insertion and execution.

The opt-in physical Node smoke is not represented as passed.
The paired TLS SQLite/HTTP regression seeds a current manifest,
verifies ordinary inbound inference and idempotent replay, then
expires the manifest and removes one confirm bit independently,
verifying rejection. It restores the fixture before testing a
missing local model-admission authority. Heartbeat recovery remains
possible after expiry.

These are local admission boundaries; a Node may still disconnect
after a successful check. Transport unknown-outcome semantics
continue to apply. RC11 packaging remains contingent on the
comprehensive code review, user vision realignment and second review.
