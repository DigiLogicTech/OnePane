# RC11: Level 5 source-signature submission API (implementation checkpoint)

**Status (2026-10-12):** Source-key administration and proof submission HTTP enforcement. Draft integration branch. This is a cryptographically authenticated **existing-Observation proof** path, not live verified Node sensor transport, verified device health or automatic protection.

## Permission boundaries

The API requires authentication before any write. Project-based key management is separated from machine-only observation proof submission:

| Endpoint | Principal/authentication | Required authorisation | Side effects |
| --- | --- | --- | --- |
| `GET /v1/projects/{projectID}/continuous-goals/{goalID}/source-signers` | human | `project.read` | Up to 100 key metadata records, no private key |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/source-signers` | human | `project.write` | Stores human-approved public signer key; updates configured Goal revision and audit event |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/source-signers/{signerID}/revoke` | human | `project.write` | Revokes current key, increments revisions, retains immutable signed proofs |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/signed-observations/record` | service/agent holding real bearer API credential; no session/cookie substitutes | **`goal.source.submit`** for canonical tenant Workspace, plus live approved signer and source key | Conservatively reserves one of 30 source-reading slots in a fixed 60-second per-key window and records strictly unverified local Observation; returns only ID/hash for separate signing |
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

### Step 1: Stage one unverified reading

```json
{
  "signer_id": "approved-signer-id",
  "observation_type": "gateway.sample",
  "value": {
    "reachable": true
  }
}
```

Requires an exact approved signer and a service/Agent bearer credential with `goal.source.submit`. Only this narrowly authorised source can stage new readings. The server chooses canonical tenant Workspace, the authenticated source principal, `signed-source://<signer-id>` opaque subject, local timestamp, fixed probe version, `INTERNAL` confidentiality, `TRUSTED_NODES` residency and `UNVERIFIED_DERIVED` trust. Raw sensor payloads are local/trusted-node-only and cannot be automatically routed to cloud destinations. Caller-supplied Node origin, provenance, trust, subject, actor or Workspace fields are unknown and refused. Request bodies are limited to 8 KiB, raw `value` to 4 KiB and must be valid, unambiguous JSON.

The response returns `observation_id`, `integrity_hash`, `source_id`, `observed_at` and `status: unverified_awaiting_source_signature` only. Sensitive reading `value` is stored in the isolated immutable Observation record and **never** echoed in the response or Goal event metadata. Its `SubjectRef` is an opaque source signer ID rather than an arbitrary device hostname.

A second, serialized SQLite reservation independently enforces at most **30 staged readings per source key per 60-second time window**. This quota is conservatively spent before the Observation append; if a subsequent append fails or human revocation races with it, an unverified row may remain, but **no signed proof, verified health, monitoring or remediation is inferred**. Source credential revocation and key revocation prevent future authenticated proof admission.

### Step 2: Sign and submit the returned Observation ID/hash

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
- A source stages a **new, unverified local Observation** and subsequently submits its separate Ed25519 proof using that immutable Observation ID/hash. This does **not** certify origin Node/device identity. The API does not provide Node pairing, authenticated measurement transport, programmatic source principal creation, key provisioning, credential issuance or remote probe execution. Key distribution must be separately secured.
- A machine API credential must already have been provisioned with the narrowly scoped `goal.source.submit` capability and exact Workspace. No broad `project.write` fallback or automatic issuance was added.
- Submission does not activate a L5 Goal, schedule monitoring, apply remediation or turn health into a green/verified state. Signature coverage is a separate informational projection.
- A signer can commit no more than **30 successful immutable proofs in any rolling 60-second window**, enforced under the same SQLite writer transaction with an indexed bounded query; denied admission does not advance the signing sequence. The API reports HTTP **429** with a conservative `Retry-After: 60` header. Source admission remains capped at **8 KiB** of JSON request body.
- Operational hardening before production: source credential provisioning/revocation UX, denial/invalid-signature request throttling and aggregate per-Project budget, device/source-sensor measurement identity, paired-Node mutually authenticated transport, backlog strategy, high-volume historical retention pagination, and incident signal confidence.

## Validation and release sequencing

Source and integration tests use real persisted bearer credentials and workspace/capability scopes, verify a human-only registration, machine-only signed admission, project.write-only denial, source impersonation denial, replay/tampered sequence rejection, duplicate JSON denial, key audit visibility/revocation, no raw-secret readback and no auto-monitoring. CI details are maintained in `docs/rc11-second-review-gap-matrix.md`.

**User sequencing remains binding:** finish the functional/security integration and first code review before the full human-focused UI/UX/customisation pass, which is the LAST development workstream before the next QA build. Then final CI/code audit and authorised Windows/Ubuntu packaging. No merge/release at this checkpoint.


## Operator-managed machine bearer credentials (P0-D)

Source evidence HTTP bearer tokens are no longer intended to be provisioned as unconstrained generic project credentials. A human operator must first enrol an exact Ed25519 signer ID, then mint a **separate single-purpose bearer token** for that signer:

| Method and endpoint | Authority | Result |
| --- | --- | --- |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/source-signers/{signerID}/credentials` | Human `project.write` plus live Goal/signer/source checks | Creates random bearer token; only this response contains `bearer_token`, alongside safe credential metadata |
| `GET /v1/projects/{projectID}/continuous-goals/{goalID}/source-signers/{signerID}/credentials` | Human `project.write` | Bounded metadata including expiry/revocation; no secrets |
| `POST /v1/projects/{projectID}/continuous-goals/{goalID}/source-signers/{signerID}/credentials/{credentialID}/revoke` | Human `project.write` | Atomically revokes the binding and underlying API credential; no token in response |

Issue body: `{"expires_at":<epoch_milliseconds>}`. Revocation body: `{}`. Signer credentials expire after 5 minutes to 7 days and no later than the approved signing key. No more than two active credentials per signer. A new token is generated using 32 bytes from the CSPRNG and is stored only as a SHA-256 token digest; no plaintext token is stored in either database or audit events. Browser UI must show/copy a newly minted token once, then never rehydrate it; protecting the clipboard/browser local state remains a later UI/UX design task. The credential inventory returns a status of `active`, `revoked`, `expired`, `signer_inactive` or `scope_inactive`, evaluated against the current machine-token and signer state; this is not a device-health indicator.

The `record` and `submit` domain writer transactions now verify that the active API credential and immutable source-credential binding match the **exact** Goal, signer, original service principal, tenant Workspace, current grant and finite expiry; only `["goal.source.submit"]` and the exact tenant Workspace array are accepted. A generic bearer token with project.write, wildcard permissions, unbounded expiry or no explicit binder record cannot enter the source-only proof lane. Historic internal Go-only service methods are not public bearer endpoints.

**Status:** This is source credential provisioning, not first-time physical sensor principal enrollment or paired-Node authenticated transport. It cannot by itself prove device origin, enable network remediation, or mark a Goal healthy. Integration/CI status is recorded in the RC11 task manifest.


### Failed-request abuse budget

In addition to the separate 30-per-minute **successful** source reading and signed proof budgets, migration `0074_goal_source_request_attempt_budget.sql` introduces one **durable shared 60-attempt fixed 60-second window per machine credential**, used by both `record` and `submit`. Every authenticated, signer-bound request consumes a reservation **before JSON decoding, signature verification or Observation access**. Invalid JSON and forged/expired signatures consume attempts rather than bypassing success-only limits. The 61st attempt returns HTTP 429 with `Retry-After: 60`; clock rollback cannot reset the window. The per-credential budget is independent of each signer’s accepted-proof/reading quotas.

This is a low-resource fail-closed guard against a malfunctioning authorised sensor, not a general Internet-facing denial-of-service solution. Authentication/proxy limits, per-Project aggregate budgets, backpressure and trusted-Node transport throughput still require operational hardening. A token without a live exact-signer binding is denied before request-body processing and does not qualify for this credential-specific budget.
