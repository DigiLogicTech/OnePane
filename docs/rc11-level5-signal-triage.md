# RC11: read-only Level 5 connectivity signal triage

**Status:** initial functionality, draft integration branch, 12 October 2026. This is a human-reviewed evidence comparison, not an incident-confirmation engine, source-of-truth device identity, autonomous monitor or remediation authority.

## Reason for this design

OnePane now has two distinct, permission-controlled evidence channels:

- A **human-approved, narrowly scoped TCP connect** to one literal RFC1918 IPv4/port. Its already-recorded `connected`, `not_connected` or `timed_out` outcome indicates only whether that socket connection was established from the probing environment. It does not prove that a device is authentic, secure or serving the expected application.
- A **human-approved paired-Node mTLS receipt** with `peer_connectivity` outcome `reachable`, `unreachable` or `unknown`. Its pinned client certificate proves which registered Node connection submitted the self-reported signal. The remote Node has not demonstrated physical measurement or identity of any outside network device.

These two signals are not automatically measurements of the same target. They are never merged into a verified device-health score. Additional independently authenticated device/instrument provenance and human-approved target correlation are necessary before incident-confidence decisions are possible.

## Endpoint and permissions

`GET /v1/projects/{projectID}/continuous-goals/{goalID}/signal-triage`

Requires authenticated **human** plus `project.read` for the canonical Project Workspace. No POST, generic Project write, Agent access, bearer sensor submission or model-generated action is accepted through this read-only endpoint.

The response is a compact per-source metadata projection (`sources`, `tcp_state`, `node_state`, receipt IDs, authenticated peer Node IDs, bounded timestamps and an optional attention classification). It contains **no** raw observation payload, IP/port, target URI, password, certificate, secret, private key or remote response text. The source set is constrained by the configured Goal's existing allowlist (maximum 32), and each read uses one latest qualified receipt per class/source. The decision threshold requires **an adverse TCP sample and an adverse Node sample**, from at least two distinct registered source IDs; a healthy TCP report plus two adverse Node reports is not independent cross-class confirmation.

Evidence is current only when the latest qualified observation is no more than **15 minutes old** and the Goal, Project and canonical Workspace are currently configured and active. TCP evidence must still have its exact human-approved live permit and reviewer membership, **the current Goal monitoring-plan hash, the exact target IPv4/port/timeout and permit revision**, and a claim/completion time no earlier than permit approval. Node evidence must still have its live human-approved Goal grant, current matching paired certificate fingerprint, two-party pairing and human reviewer membership. Historical records remain immutable and independently inspectable, but revoked grants do not count toward **current** triage. The TCP projection examines the **most recently claimed** attempt (not merely the most recent successful completion). If it is still `claimed`, was `cancelled`, or has inconsistent completion chronology, it is reported as `unknown_after_interruption` and older TCP outcomes **must not be resurrected** as current reachability evidence.

### Read-only status contract

| `state` | Meaning |
| --- | --- |
| `goal_not_configured` | Goal is paused, retired or otherwise not configured for live evaluation; historical receipts remain available separately |
| `no_recent_measurements` | Configured Goal with no qualifying recent evidence from either class |
| `single_measurement_class` | Recent results from only one class, with no adverse outcome |
| `mixed_classes_no_adverse_indicator` | Both classes have recent records, neither currently adverse |
| `adverse_indicator_needs_review` | At least one adverse signal, but the strict multi-class/independent-source threshold is not reached |
| `multiple_adverse_indicators_investigate` | At least two distinct allowlisted source IDs have adverse indicators, **including at least one failed TCP connect and one peer-reported unavailable connection**; investigation is warranted, but this is NOT corroboration of the same target or a security incident |

The response also retains the canonical stored Goal `goal_status` and `goal_health` without promoting either from these diagnostic signals. A paused, retired or otherwise nonconfigured Goal short-circuits current triage with `goal_not_configured`.

All responses explicitly set `monitoring_active=false`, `device_identity_verified=false` and `remediation_authorized=false`. The limitations array explains the missing target/instrument identity and trusted device-health evidence. The method never schedules a Task, opens a socket, changes a Goal, sends an alert or authorises a recovery action.

**Fail-closed cases:** stale receipts; an inactive/retired/paused Goal; a revoked/expired human TCP permit; a withdrawn Node pairing, matching TLS cert or transport grant; suspended human approver membership; foreign Project/Workspace; invalid source allowlist. A receipt timestamp prior to the human Node grant approval is excluded. Negative outcomes from a single registered source ID cannot independently count as two corroborating sources. A positive TCP connection does not corroborate an adverse peer report.

## Engineering evidence and next gates

- Integration test seeds exact safe **test-only metadata receipts**, not network I/O, to verify zero-evidence, one-class, distinct-source adverse, credential/target non-disclosure, revoked Node and suspended human reviewer downgrade, no Goal activation and foreign-tenant rejection.
- HTTP integration test verifies human Project reader only, denies Agent/cross-tenant/anonymous calls and ensures a POST cannot activate an unattended Goal.
- Production source tests in GitHub Actions cover Go/SQLite, existing frontend contracts and cross-platform compilation, but **not installed Windows/Ubuntu or physical-device QA**.

Remaining mandatory work: canonical device/instrument identity and explicit shared-target bindings, real hardware and trust-zone probes with human consent, independent measurements at realistic cadence, low-RAM historical-data soak, calibrated incident classification, operator-controlled alerts, separately approved bounded reversible remediation with postcondition verification and full code/security review.

As agreed, complete functional/security work first. The comprehensive human-focused **UI/UX and customisation review is the last development workstream before the next QA build**, followed by final audit, CI, packaging and installed QA. No release merge or L5 home-network defence claim is authorised by this projection.
