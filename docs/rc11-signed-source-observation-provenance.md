# RC11 — Level 5 signed source observation provenance

**Status (2026-10-12):** Source-level authenticated evidence foundation. Not end-to-end monitoring, authenticated remote ingestion, or a packaged user feature. PR #227 remains draft/unmerged.

## Why this is necessary

An ordinary Observation carries a recomputable SHA-256 content hash. The hash detects a change to immutable storage but does **not** establish who produced the underlying data. A data label such as `authoritative_data` likewise does not establish sensor authenticity. A manually attached Observation receipt remains a human attribution, not a source cryptographic proof.

## Implemented trust boundary

- Migration `0070_goal_observation_source_signatures.sql` introduces independently enrolled Ed25519 public keys and immutable source-signed proof records. No existing Observation, manual receipt or prior health state is upgraded retroactively.
- A human Project member approves **one active key per exact Goal/source**. The key is also bound to one canonical Project Workspace, source service/Agent principal, approved source plan hash, and bounded expiry (more than five minutes and at most thirty days). Public keys only are stored; OnePane does not generate/handle the signing private key.
- `ApproveGoalObservationSigner` and `RevokeGoalObservationSigner` require live human Project authority inside the SQLite writer transaction; neither schedules work nor authorises a network connection.
- `AcceptSignedGoalObservation` verifies the recorded Observation's content hash, the exact tenant and source principal, active Project/Workspace, goal configuration and source plan, source and human-approver membership/type, key status and expiry, observation chronology/freshness, the Ed25519 signature and a strictly advancing signer sequence. Sequence consumption and immutable proof insertion occur in the **same** transaction.
- No foreign or unverified remote Node origin is accepted by this initial local-source admission path. A matching Node ID/data label does not substitute for authenticated Node transport.
- Version 1 signature domain: `OnePane/goal-observation/ed25519/v1`. The signed JSON produced by `SignedGoalObservationMessage` contains only `domain, project_id, goal_id, signer_id, observation_id, integrity_hash, sequence`, in that order. Never sign an untrusted caller's raw alternative encoding and assume equivalence.
- A revoked or expired key cannot authenticate **new** observations. Historical proofs remain immutable for audits; signing privileges are never reconstructed from old signature records or from human receipt approvals.
- Goal evidence coverage now reports separate `signature_evidence_state` and `recent_source_signed_sources` metadata. The states distinguish missing, historical-key-inactive, recent-source-signature and stale-source-signature. Manually attached receipt freshness continues to be tracked independently. `monitoring_active=false` and `device_health=unverified` remain truthful: origin integrity is not host security, operation health or successful continuous protection.
- A read-only API, `GET /v1/projects/{projectID}/continuous-goals/{goalID}/signed-observations`, requires authenticated `project.read`, checks canonical active Project/Workspace ownership, and returns bounded proof identifiers/timestamps/source/hashes only, never Observation raw values, signature bytes, credentials or private keys.

## Deliberately not exposed or activated yet

**No operator key-enrolment or source-submission HTTP endpoints, no automatic proof ingestion, no remote Node evidence transfer, no remediation and no verified device health.** The internal Go approval and verification methods are integration-testable foundations. A future source-submit API must have its own independently authorised identity/capability, request authentication, size and rate limits, device/Node provenance, source principal binding, and audit review. Do **not** use a broad `project.write` grant as a substitute for a narrowly scoped sensor-submit capability.

Remaining tasks: signed transport and bootstrapped source-key onboarding; signed measurement provenance bound to a verified source/paired Node; anti-replay under concurrent/restarted submission; multi-signal baseline and incident criteria; human-authorised reversible playbooks with verified postconditions; long-history read/query resource-pressure tests; manual Windows/Ubuntu and physical Node QA.

## Verification

- Key enrolment, anti-replay, cryptographic mismatch, unapproved source, cross-goal signature, human/source membership and role revocation, key replacement/immutability, tenant read isolation, coverage truthfulness, source-secret redaction and absence of unintended Task/monitor execution have Go/SQLite integration regressions.
- The initial signed-source schema/service/proof head passed full hosted CI [#38203538876](https://github.com/DigiLogicTech/OnePane/actions/runs/38203538876). Live-signature coverage head passed [#38203684151](https://github.com/DigiLogicTech/OnePane/actions/runs/38203684151); identity type and membership regression passed [#38203802519](https://github.com/DigiLogicTech/OnePane/actions/runs/38203802519).
- The first API smoke CI [#38203944469](https://github.com/DigiLogicTech/OnePane/actions/runs/38203944469) exposed a route precedence test failure: the legacy generic Goal transition POST handler intercepted the signed-proof read-only path and returned HTTP 400. The correction explicitly returns HTTP 405/Allow GET for this exact path, with no write handler. **Full source/regression CI passed** at [#38204111470](https://github.com/DigiLogicTech/OnePane/actions/runs/38204111470), including the endpoint contract, Go/SQLite integration, full Go regression and cross-platform compilation. The following manifest edits are documentation-only.

**User-mandated sequencing:** Complete feature/security implementation and code review first. The comprehensive human-focused UI/UX/customisation alignment is the **last development workstream before the next QA build**. Follow with final code/CI verification and operator-approved Windows/Ubuntu packaging/installed QA. No release merge or installer is authorised by this checkpoint.


## RC11 signed-source key rotation and strict JSON ambiguity checks — 2026-10-12

- [x] **Revoked-key rotation tested:** revoke a source's Ed25519 identity, preserve prior immutable proofs and sequence watermark, explicitly approve a distinct new public key for that Goal/source, then accept a fresh proof from sequence 1 of the new key only. The old signer remains revoked. Full application/SQLite/cross-platform CI [#38204321403](https://github.com/DigiLogicTech/OnePane/actions/runs/38204321403) passed.
- [x] **Ambiguous Observation JSON is fail-closed:** a streaming, depth-bounded token validator now rejects duplicate JSON object fields (including Unicode-escaped aliases), multiple roots and excessive nesting before Observation recording. The same check runs when an existing Observation is integrity-verified, so a legacy hash with ambiguous JSON cannot be treated as authenticated sensor data. Errors do not reveal private field names.
- [x] **Negative tests:** valid nested arrays and ordinary values still pass; duplicate root/nested/Unicode aliases, secret-bearing error messages, invalid roots and deep arrays are rejected. A SQLite regression constructs a legacy immutable Observation with duplicate fields and a correctly matching SHA-256 and confirms that integrity verification still fails.
- [x] **Validated:** strict-JSON source/test head `6a1feb751e8f` passed full hosted CI [#38204547439](https://github.com/DigiLogicTech/OnePane/actions/runs/38204547439), including evidence admission/legacy regression, Go/SQLite integration, full Go tests and cross-platform compilation. This does not certify installed systems or physical Nodes.
- [ ] **Next:** independently authenticated, capability-scoped service submission and key enrolment UI/API; trusted paired-Node measurement/identity; multi-signal incident assessment and safe postcondition-checked remediation; low-RAM long-history validation. No autonomous protection or installed QA claim. Preserve the full user-centred UI/UX review as the final pre-build development gate.
