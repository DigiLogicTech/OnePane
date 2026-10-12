# RC11 — Out-of-band pinned TLS certificate possession (P0-L)

**Status:** opt-in, human-authorised source implementation in draft PR #227, 12 October 2026. This does not provide cryptographic physical-device attestation, vulnerability assessment, incident confirmation or a running home-network security monitor.

## Model and operator consent

Existing controls first require an active Level 5 Project and configured Goal, an active human-approved single private IPv4 TCP permit, and a human-declared exact TCP endpoint with immutable permit/Goal/plan/port association. A human with live Project write permission must then explicitly register the *SHA-256 digest of the leaf TLS certificate* collected through an independent, trusted channel. The application never auto-pins whichever certificate it first encounters; blind trust-on-first-use would provide no protection against an initial wrong peer.

New immutable migration: `0079_goal_tls_endpoint_pins.sql`. It stores a short-lived (more than 5 minutes and no more than 24 hours, always no longer than its target declaration/permit) pin, scope, issuer, revision and revocation. Only one approved live pin per exact declared endpoint. The fingerprint is a lowercase literal `sha256:<64 lowercase hex digits>`, not a hostname, CA bundle or secret API key. Replacement requires separate human revocation and a new explicit approval; automatic rotation is prohibited.

Operator endpoints under `/v1/projects/{projectID}/continuous-goals/{goalID}`:

| Operation | Route suffix | Authority |
| --- | --- | --- |
| Approve exact fingerprint | `POST /tls-pins` | Human `project.write`, optimistic Goal/target revision |
| Inspect up to 100 historical pin records | `GET /tls-pins` | Human `project.read` |
| Explicitly revoke a pin | `POST /tls-pins/{pinID}/revoke` | Human `project.write`, optimistic Goal/pin revision |
| Perform **one** short verification | `POST /tls-pins/{pinID}/verify` | Human `project.write` and live pin, target, permit, Goal/reviewer/plan |
| Inspect up to 100 check receipts | `GET /tls-checks` | Human `project.read` |

The approval body provides only a declared-target ID, certificate digest, bounded expiry and expected revisions. The verify body provides only expected Goal and pin revisions. **Neither body can supply a target address, port, URL, hostname, proxy, DNS name, IP range, or TLS adapter.** The server reuses the human-approved private literal IPv4/port and 100–1500ms permit timeout. Public sensor or Agent bearer tokens cannot operate these endpoints.

Historical `status` and live `effective_pin_state` are distinct: `active`, `expired`, `approver_inactive`, `target_inactive` or `revoked`. Live status rechecks the original operator-declared target and TCP permit state; a changed Goal monitoring plan, removed source, human membership downgrade, expired permit or withdrawn pin stops future authority without rewriting history.

## Execution

The explicit checker reserves exactly one durable claim per human pin in each fixed **five-minute** window before opening a socket. Two concurrent checks globally are allowed on low-resource hosts. The checker reads canonical Project/Goal/Workspace, live operator and original reviewers, permit revision/plan, target scope, exact private IPv4/port, fingerprint and expiry before reservation and again immediately before dispatch. A cancelled/revoked authority suppresses dispatch. The check opens a TCP socket without HTTP requests, redirects, DNS resolution, custom SNI, credential payloads, application requests or scanner enumeration. It completes at or before the short permit deadline.

TLS requires at least TLS 1.2, a current leaf certificate and the **exact** approved SHA-256 DER fingerprint, using constant-time comparison. The Go TLS client's default CA/hostname chain validation is deliberately replaced **only** by the mandatory human certificate pin, permitting securely pre-enrolled self-signed or private PKI endpoints without inferring trusted manufacturer/device identity. There is no fallback to unpinned TLS. A mismatch, unavailable endpoint, TLS failure or cancellation never yields `matched`. A live-approval recheck in the final SQLite transaction cancels a match if the operator, original pin approver, declared endpoint, Goal or original permit lost authority during the short handshake.

Result statuses are `claimed`, `matched`, `mismatched`, `unavailable`, `tls_failed` or `cancelled`. Historic checks cannot be edited or removed. They contain only bounded metadata—no certificate PEM/private key, network packet, target IP/port, prompt, sensor payload, host/URL, security finding or remediation action. Operator triage adds the distinct `tls_check_state`, `tls_check_id`, `tls_pin_id`, `tls_identity_class=operator_pinned_tls_certificate` and `tls_certificate_possession_matched` fields **only for still-active human permission/plan/target mappings and recent checks**, with a new pending claim masking older completed results. A revoked/expired pin does not count as current evidence.

## Essential trust distinctions

- A match means that an endpoint answering at the **explicitly approved private IPv4/port** demonstrated possession of the private key corresponding to the human-pinned leaf certificate at handshake time.
- A match does **not** establish independent identity of the physical router, IoT device or server; its software integrity, availability of other services, lack of compromise, manufacturer authenticity, current patch state, user ownership, or any verified security posture.
- A mismatch can mean routine certificate renewal, endpoint reassignment, a proxy, misconfiguration or interception. It is **not evidence by itself of an attack**. It does not increment negative incident votes, alarm authority or Level 5 remediation permission.
- The mTLS paired-Node self-report remains a **separate identity** and `cross_class_target_correlation=not_established`; matching a human-pinned local TLS endpoint never proves that a remote Node measured that same target.
- All responses preserve `device_identity_verified=false`, `monitoring_active=false` and `remediation_authorized=false`. There are no background TLS jobs, network scans or threat-prevention actions enabled.

## Testing and remaining gates

The real local TLS handshake test checks matching/nonmatching pinned certificates, invalid fingerprint and targets, bounded deadlines, no implicit trust. SQLite/API tests cover human/Agent/foreign-tenant permission, one-time human approval, no address override, one claim per five-minute window, prior grant/Goal authority, immutable audit, revocation, exact receipt history and no security-state promotion. The two-Node/low-RAM installed QA and actual device validation remain pending.

Next P0: human-tested out-of-band fingerprint enrollment workflows and safe rotation, independent device-level signed instrument/source attestation where feasible, user-approved **same-device** source correlation with provenance and specific target semantics, rigorous incident confidence thresholds, independently human-approved reversible Level 5 actions with postcondition checks, aggregate concurrency/low-end stress, code/security audit. As agreed, the comprehensive user-focused control/customisation/UI/UX review remains **last before packaging the next QA build**; no release merge or installer at this stage.
