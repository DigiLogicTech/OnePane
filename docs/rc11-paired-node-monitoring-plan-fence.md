# RC11 — Paired Node monitoring-plan version fence

**Scope:** P0, source-level fail-closed consent gate. Hosted CI does not attest any physical Node or external target.

A human-approved paired-Node connectivity grant was previously bound to the Goal, Workspace, source ID and mTLS certificate, but not to the exact immutable monitoring-plan version. A subsequent edit could retain the same source name and inadvertently preserve an earlier transport approval.

Migration 0085 adds monotonic Goal plan_generation plus an immutable plan_generation and plan_sha256 on each approved Node grant. The current admitted sample must match both the generation and SHA-256 of the original canonical human-approved source plan inside the same SQLite writer transaction as sequence consumption and immutable receipt insertion. The current operator inventory explicitly reports scope_inactive for stale approvals, while historical approved status and signed receipts remain intact.

A plan revision invalidates the old grant even if the source names are unchanged or the old JSON is later restored. Existing approved grants created before migration default to generation zero and blank fingerprint: these fail closed, and require explicit operator revoke plus a fresh approved grant after upgrade. No Project data or Node history is deleted.

Acceptance tests require previously admitted self-report, same-source plan expansion denial, return-to-original-plan denial, unchanged durable sequence and receipt count on failed submissions, denial of rewriting grant hash, and successful human reapproval of the current version. Paused/revoked/expired peers and withdrawn human grants remain independently checked.

This binds permission to approved monitoring intent. It does NOT establish trusted physical Node execution, physical network vantage, instrument ownership, target-device identity, independent evidence corroboration, background monitoring or Level 5 defensive authority. The comprehensive human-focused UI/UX alignment review stays the last development step before a separately authorised installer and installed QA.
