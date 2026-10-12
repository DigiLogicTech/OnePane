# RC11: Level 5 source-signature submission API (implementation checkpoint)

**Status (2026-10-12):** Source-key administration and proof submission HTTP enforcement. Draft integration branch. This is a cryptographically authenticated **existing-Observation proof** path, not live verified Node sensor transport, verified device health or automatic protection.

## Permission boundaries

The API requires authentication before any write. Project-based key management is separated from machine-only observation proof submission:

| Endpoint | Principal/authentication | Required authorisation | Side effects |
| --- | --- | --- | --- |
| `GET /v1/projects/{projectID}/continuous-goals/{goalID}/source-signers` | human | `project.read` | Up to 100 key metadata records, no private key |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/source-signers` | human | `project.write` | Stores human-approved public signer key; updates configured Goal revision and audit event |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/source-signers/{signerID}/revoke` | human | `project.write` | Revokes current key, increments revisions, retains immutable signed proofs |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/signed-observations/submit` | service/agent holding real bearer API credential; no session/cookie substitutes | **`goal.source.submit`** for canonical tenant Workspace, plus active registered signer for the exact Goal/source and Ed25519 proof | Atomically consumes monotonically increasing source signature sequence and persists metadata-only proof |
| `GET /v1/projects/{projectID}/continuous-goals/{goalID}/signed-observations` | authorised Project reader | `project.read` | Up to 100 immutable signed proof metadata records |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/signed-observations` | any | **never permitted** | Explicit HTTP 405, read-only endpoint |

`project.write`, `project.read`, a valid web session, and mere possession of an Observation ID do **not** grant source submission. Conversely `goal.source.submit` does not grant key approval, revocation or key inventory access.

The canonical authenticated identity is used as the submitting source principal; the request may not supply or override `actor_principal_id`. The domain service rechecks the approval's exact Project, Goal, source ID, canonical Workspace, source principal and human approver membership/types, plan hash, active Goal status, key validity and expiry, freshness, Observation content SHA-256 and strict unique-key JSON validity, and Ed25519 signature. Sequence advancement and immutable proof insertion happen in one SQLite transaction, with no queued task or model execution.

All mutation JSON bodies are strictly decoded (unknown/duplicate fields, Unicode-escaped duplicate aliases, extra roots, deep structures and payloads over 8192 bytes fail); binary public keys and signatures are `base64`-encoded via Go `[]byte` JSON.

### Human key enrolment body
```json
{
  "source_id": "gateway",
  "source_principal_id": "your-service-principal-id",
  "public_key": "base64-encoded-32-byte-Ed25519-public-key",
  "expected_goal_revision": 1,
  "expires_at": 1790000000000
}
```

### Source proof submission body
```json
{
  "signer_id": "approved-signer-id",
  "observation_id": "previously-recorded-observation-id",
  "sequence": 1,
  "expected_goal_revision": 2,
  "signature": "base64-encoded-64-byte-Ed25519-signature"
}
```

`expires_at` above is illustrative only; the service enforces more than five minutes and no more than 30 days from actual submission. `SignedGoalObservationMessage` in `internal/projectworkspace/continuous_goal_signed_observations.go` defines the **exact versioned signed bytes**: `OnePane/goal-observation/ed25519/v1` over canonical JSON of Project ID, Goal ID, signer ID, Observation ID, Observation integrity hash and monotonic sequence. Sign the exact output bytes, not a separate re-implementation that happens to look similar.

## Security and functionality limitations

- A source-signed Observation establishes approval of a signing key and possession of its private key; it does not prove that the source's measurements are accurate, an originating remote Node is paired, that a particular physical device is healthy, or that network protection is running.
- A source submits proof for an **already-recorded**, immutable local Observation. The API does not provide a route for sensor raw readings, Node pairing, authenticated measurement transport, programmatic source principal creation, key provisioning, credential issuance or remote probe execution. Key distribution must be separately secured.
- A machine API credential must already have been provisioned with the narrowly scoped `goal.source.submit` capability and exact Workspace. No broad `project.write` fallback or automatic issuance was added.
- Submission does not activate a L5 Goal, schedule monitoring, apply remediation or turn health into a green/verified state. Signature coverage is a separate informational projection.
- Operational hardening before production: source credential provisioning/revocation UX, per-principal/key rate and payload budgets, device/source-sensor measurement identity, paired-Node mutually authenticated transport, backlog strategy, high-volume historical retention pagination, and incident signal confidence.

## Validation and release sequencing

Source and integration tests use real persisted bearer credentials and workspace/capability scopes, verify a human-only registration, machine-only signed admission, project.write-only denial, source impersonation denial, replay/tampered sequence rejection, duplicate JSON denial, key audit visibility/revocation, no raw-secret readback and no auto-monitoring. CI details are maintained in `docs/rc11-second-review-gap-matrix.md`.

**User sequencing remains binding:** finish the functional/security integration and first code review before the full human-focused UI/UX/customisation pass, which is the LAST development workstream before the next QA build. Then final CI/code audit and authorised Windows/Ubuntu packaging. No merge/release at this checkpoint.
