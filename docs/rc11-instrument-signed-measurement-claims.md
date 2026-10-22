# RC11 — Human-imported instrument-signed endpoint self-reports

**Status:** P0-N initial implementation; draft OnePane PR #227, 13 October 2026. A signed *report* is not an independently verified physical-device measurement.

## Defined narrow evidence scope

A registered Ed25519 instrument signing key is approved out of band by a human and bound to one human-declared TCP target, one out-of-band leaf-certificate TLS pin, the exact active private IPv4/port TCP permit, the Goal source ID, current monitoring plan and canonical Project Workspace. All existing approvals and original human membership remain revocable and expire independently.

This work allows an external signer to assert **one** tightly typed outcome about the **declared endpoint**:

- `measurement_type=endpoint_tcp_connect_claim`
- `outcome=connected | not_connected | timed_out | unknown`
- `observed_at`: signed source-supplied Unix-millisecond time, constrained to the two-minute challenge's time interval and no later than server admission

The result can only be imported by an authenticated human Project writer. There is no remotely exposed or unattended sensor producer, process invocation, arbitrary user-provided network target, hostname, command, script, shell, scan or periodic probe. The external instrument MUST perform its own appropriately consented reading; **OnePane cannot currently verify that it actually did**. The human-approved binding is an intended observation target, not physical-source/target-location attestation.

## Complete human-mediated protocol

The existing human-only `POST .../instrument-keys/{keyID}/challenge` reserves a random 32-byte nonce, issues a versioned Ed25519 challenge envelope, sets a maximum two-minute lifetime and consumes one of the existing per-key budgets: four challenges per five minutes and 128 lifetime challenges.

Under `/v1/projects/{projectID}/continuous-goals/{goalID}`:

| Route | Method and permission | Description |
| --- | --- | --- |
| `/instrument-keys/{keyID}/measurement/prepare` | POST, human `project.write` | Accept server-issued `challenge_id`, one allowed outcome, source `observed_at` and `expected_goal_revision`; return exact deterministic signing bytes, no network effect or nonce consumption |
| `/instrument-keys/{keyID}/measurement/accept` | POST, human `project.write` | Same fields plus base64 64-byte detached Ed25519 `signature`; atomically verify and consume the original challenge, store immutable signed claim and append safe tenant event |
| `/instrument-measurements` | GET, human `project.read` | Bounded 100-entry metadata-only history, including after signer revocation |
| `/instrument-measurements/{claimID}/verification-bundle` | GET, human `project.read` | Original signed payload and detached signature with enrolled public key for independent verification |

The v1 signing payload is server-generated JSON with the domain separator `OnePane/instrument-measurement/ed25519/v1`. It includes the complete byte-for-byte original v1 challenge payload (base64), Project, Goal, enrolled instrument key, one-time challenge, target and pinned-TLS IDs, Goal allowlisted source ID, narrow measurement type, outcome and observed time. It never includes raw target IP/port or a network packet. The external key signs **the exact bytes returned from prepare**. Altering the outcome or source target ID breaks signature verification.

Server admission transaction must recheck the original TLS pin and original reviewers, human Project membership, active configured Goal and Project Workspace, canonical monitoring-plan hash, exact human-approved permit and target, original challenge status `issued`, nonce expiration, post-issuance source timestamp and strict Ed25519 signature. It atomically moves challenge `issued→accepted` with the immutable claim/event. A challenge used for the earlier **key-possession** proof is unavailable for a measurement, and a challenge used for a measurement cannot be replayed for a key-possession proof or another outcome. No fallback model, alternate signer, implicit trust-on-first-use, unaudited reissue or automatic recovery is allowed.

## Evidence status versus health

Stored `proof_class=instrument_self_report_signed_not_attested`. Every signed claim and bundle explicitly records `observer_vantage_verified=false`, `device_identity_verified=false`, `target_correlation_established=false`, `remediation_authorized=false`. **A genuine Ed25519 signature only authenticates possession of the approved key and exactly what that key asserted.** It does not prove the instrument physically observed the intended device, used the correct network vantage, ran safe firmware, reported truthfully, or that the target was compromised/unavailable.

Human-only Goal `signal-triage` may show the most recent live-permission, past-15-minute signed self-report as `signed_measurement_claim_state=recent_instrument_self_report_unverified`, including its outcome, ID and proof class. These are **zero incident votes**; a signed negative source report cannot independently increase TCP/Node adverse classification, become `monitoring_active`, mark device health verified, establish cross-class correlation, authorise a Task or trigger defensive remediation. When the key/pin/target/Goal membership or plan is revoked or changed, the signed report disappears from **current** triage but remains in historical, independently checkable proof records.

## Test evidence and remaining engineering

SQLite migration `0081_goal_instrument_signed_measurements.sql` enforces a unique proof per challenge, exactly four allowed outcomes, an explicit proof class, immutable proof rows, 64-byte signatures, bounded signing bytes and a canonical signature digest. Existing key/challenge quotas bound the per-key volume. New tests check forged detached signatures, tampered outcomes, prechallenge timestamps, cross-tenant scope, single-use across two proof types, bounded read/history, no raw network address, offline independent re-verification after revocation, human/Agent permissions and absence of health and incident promotion.

**Not yet delivered:** secure real instrument workload and physical-vantage attestation, authenticated identity of the measured device (as distinct from one IP/port), fresh multi-vantage evidence independence, calibrated incident confidence, trustworthy reversible operator-approved Level 5 actions with rollback/postconditions, low-end resource soak and full code/security review. The human-focused UI/UX/control/customisation review remains the final development workstream before preparing the next QA installer. This work **does not** allow claiming an operational autonomous home-network security system.
