# RC11 P0-M — Independent instrument-key challenge/response

**Status:** implemented as an **operator-mediated, out-of-band proof of Ed25519 key possession**, draft OnePane RC11 PR #227, 12 October 2026. It is **not hardware attestation**, automatically sampled device telemetry, a confirmed same-asset correlation, or active network protection.

## Why a second key

A human-pinned TLS leaf certificate can establish that the endpoint at an approved private IP/port possessed its certificate key at handshake time. It does not establish a physical router or IoT identity, whether its firmware is trustworthy, or whether a separate sensor truthfully measured it.

This stage allows the operator to approve an **independent instrument public key** and bind it to exactly one previously approved pinned TLS endpoint. Both bindings are human declarations; the two private keys may be owned by the same actor or compromised together, and OnePane does not claim key independence beyond out-of-band human enrollment.

The code never generates, uploads, stores, searches for or handles the instrument's private key. OnePane only generates unpredictable signing challenges and verifies externally supplied detached signatures with the human-approved public key.

## Human-only API and lifecycle

Prefix: `/v1/projects/{projectID}/continuous-goals/{goalID}`

| Method | Route | Authority |
| --- | --- | --- |
| POST | `/instrument-keys` | Authenticated human `project.write`; approve 32-byte Ed25519 public key against one exact, current TLS pin |
| GET | `/instrument-keys` | Authenticated human `project.read`; last 100 historical public-key metadata records |
| POST | `/instrument-keys/{keyID}/revoke` | Authenticated human `project.write` and optimistic Goal/key revision |
| POST | `/instrument-keys/{keyID}/challenge` | Authenticated human `project.write`; generate a one-use random challenge after current-scope review |
| POST | `/instrument-keys/{keyID}/prove` | Authenticated human `project.write`; submit a detached Ed25519 signature over that server-generated challenge |
| GET | `/instrument-proofs` | Authenticated human `project.read`; last 100 immutable proof metadata records |
| GET | `/instrument-proofs/{proofID}/verification-bundle` | Authenticated human `project.read`; export one proof's original public key, exact signed payload and detached Ed25519 signature for independent verification |

The **approval** accepts `tls_pin_id`, `public_key` (base64), `expected_goal_revision`, `expected_pin_revision`, and `expires_at`. The human must first verify the public key through an independent trusted channel, not automatically accept keys returned by the instrument on the first encounter. The server derives and seals the exact Project, Goal, Workspace, declared target, certificate-pin identity and canonical monitoring-plan hash. Expiry is more than five minutes but no more than 24 hours, never beyond the live TLS pin or target/permit expiry. Only **one active instrument key per exact TLS pin** is permitted; human revocation is necessary before deliberate rotation.

The **challenge** request contains only expected Goal/key revisions. It returns a cryptographically random 32-byte `nonce`, challenge ID, a two-minute bound, and `signing_payload` as the exact base64-encoded byte string to sign with the separately controlled Ed25519 private key. Challenge bytes are deterministic server-generated JSON with a versioned domain separator:

`OnePane/instrument-challenge/ed25519/v1`

The signed envelope binds the **Project ID, Goal ID, instrument key ID, challenge ID, declared target ID, TLS pin ID, human-approved leaf-certificate SHA-256 fingerprint, monitoring-plan hash, nonce, issue time and expiry**. Clients should sign the exact decoded `signing_payload` bytes rather than serialising a new JSON object. Signing and submitting these bytes constitutes proof of key possession **only**; it does not establish that the instrument observed a particular network asset or had safe firmware.

The **proof admission** requires the same human Project.write authority, a current configured Goal, active signer and original human reviewers, exact permitted target/plan/certificate-pin scope, an issued and unexpired single-use challenge, and a valid detached 64-byte Ed25519 signature. Replay, signature forgery, expired/revoked original authority and cross-Goal/Project signatures are rejected before immutable proof acceptance. A successful verified challenge is atomically consumed within the same SQLite writer transaction that stores the proof and tenant-audited event.

## Resource and privacy bounds

- Each enrolled key is limited to **4 challenges per rolling five minutes**, plus **128 total lifetime issued challenges**. Fixed short key/permit expiry and bounded metadata readers prevent accidental unbounded per-key growth on low-memory hosts.
- Successful signature verification is performed again when a historical bundle is exported. This independently checks the exact original challenge and its immutable context; it is **not** a post hoc claim that the instrument was installed on or measured a physical device.
- Database migration `0080_goal_instrument_challenges.sql` retains immutable human key enrollment/revocation, unique randomly generated challenge nonces, one-way issued → accepted challenge state, and append-only accepted proof history. Failed signatures do not consume the issued challenge, but rate budgets prevent unrestricted challenge minting.
- Public keys, random nonces, server-generated signing payloads, **detached 64-byte Ed25519 signatures**, their SHA-256 digests, IDs, timestamps and safe source-scope metadata are the only cryptographic/audit inputs. Accepted raw signatures are retained in the immutable SQLite proof ledger so future external reviewers can verify the cryptographic result, even after key or TLS pin revocation. Default history is still small metadata; one human-only verification-bundle endpoint reconstructs the original signed bytes and returns the enrolled public key and detached signature. It also verifies them internally and fails closed if stored proof materials no longer match. The bundle may be checked independently using `ed25519.Verify(public_key, signing_payload, signature)`. Private signing keys, target IP/port, raw network packets, instrument measurements, certificate PEM and security findings are not included in accepted proof history or new audit payloads.
- `effective_instrument_state` distinguishes historical enrollment from `active`, `expired`, `approver_inactive`, `pin_inactive` or `revoked`. Live state depends on the current TLS pin, original human reviewer, TCP permit, target and unchanged Goal plan. Revocation does not erase historical accepted cryptographic proof.
- Goal human-only `signal-triage` reports `instrument_proof_state=recent_signed_challenge`, `instrument_proof_id`, `instrument_key_id`, `instrument_proof_class=human_bound_instrument_key_possession`, and `instrument_key_possession_proven=true` only while all original grants remain effective and the accepted proof is recent (15 minutes). After key revocation, those fields disappear from **current** triage; proof history remains available separately.

**No change to safety decision authority:** `device_identity_verified=false`, `monitoring_active=false`, `remediation_authorized=false` and `cross_class_target_correlation=not_established`. An Ed25519 signature proves that someone had the private key, not that the measured remote target corresponds to the human-declared TCP endpoint, that the instrument reported truthfully, or that the home network is secure. It contributes **zero** incident votes and triggers no background Tasks, scans, alerts or host/network remediation.

## Validation and next P0

Tests cover human/Agent/foreign-tenant HTTP permissions; correctly scoped key enrollment; current Goal plan changes without changed source names; detached Ed25519 signature admission; wrong-key and cross-Project forgery; a valid signature replayed against a *different* challenge ID/nonce; duplicate challenge replay; 4-per-5min cap; immutable proof history; revocation blocking both challenge issuance and current proof attribution; and no Goal health promotion. CI is Go/SQLite/API/source contract and Windows/Ubuntu cross-compilation, **not** installed device QA.

Further work needs a trusted real instrument execution/attestation mechanism and human-approved **same-target** correlation with explicit evidence provenance and real network-vantage meaning, plus calibrated incident policies, separately approved reversible defensive operations with postcondition/rollback verification, low-resource soak and security/code review. Human-focused UI/UX/control/customisation review remains the final development workstream before producing the next QA installer; PR #227 remains draft/unmerged.
