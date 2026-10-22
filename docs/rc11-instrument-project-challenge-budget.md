# RC11 P0-Q — Cross-key instrument challenge budgets

**Status:** source committed in draft/unmerged PR #227. This is a bounded low-resource safety control, **not** a new monitoring or device-attestation capability.

The previously enrolled Ed25519 instrument signing key had two quotas: 4 issued two-minute challenges per rolling five minutes and 128 total challenges for that key's lifetime. A human operator could nevertheless rotate keys or use multiple approved target pins within the same Project. All those challenges remain in the immutable SQLite replay/audit ledger, so the per-key bounds alone do not protect a low-RAM, low-storage OnePane host from a rapid series of otherwise authorised issuances.

The issuer now checks two additional **Project-scoped rolling limits**, in the same database transaction as the existing signer, Goal and original human approvals:

| Scope | Limit | Exceeding the limit |
| --- | --- | --- |
| One instrument key | 4 new challenges per 5 min; 128 lifetime | Existing invalid-transition rejection |
| All instrument keys in the **same Project** | 16 new challenges per rolling 5 min | HTTP 429, `Retry-After: 300` |
| All instrument keys in the **same Project** | 256 new challenges per rolling 24 h | HTTP 429, `Retry-After: 86400` |

The new limits are **not global** to OnePane, so one Project cannot consume another Project's issuance budget. They count all statuses—including issued, expired, consumed or failed downstream submissions—and all previously approved, rotated and revoked instrument keys within that Project. Failed issue transactions add no nonce. The server never creates a background timer or cleanup job, and the caps expire automatically as the rolling windows advance. The return `Retry-After` header is a conservative upper bound; the earliest eligible attempt may be sooner.

A database index (`0083_goal_instrument_project_challenge_budget.sql`) covers `project_id, issued_at DESC`, keeping the per-Project rolling count efficient without a full-table scan. A test seeds multiple synthetic immutable challenge rows against one Project to simulate repeated key rotation (rather than creating an uncontrolled number of live keys), checks that the five-minute and daily ceilings reject, verifies a second Project remains unaffected, and confirms issuance eligibility returns when the daily window has elapsed. The original signature verification, human-only permissions and no-promoted-health rules remain unchanged.

**Limitations:** These are issuance *rate* controls, not total datastore caps or long-term audit retention. Immutable history can still grow over years; any future retention/archival mechanism must preserve forensic chain-of-custody and challenge anti-replay. No remotely trusted physical source, attested runtime, automatic monitoring, incident correlation or L5 remediation is introduced. The comprehensive human-centric UI/UX review remains last before QA packaging.
